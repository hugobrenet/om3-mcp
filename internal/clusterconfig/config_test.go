package clusterconfig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opensvc/om3-mcp/internal/testutil"
	"go.yaml.in/yaml/v2"
)

func writeConfig(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clusters.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(testutil.WriteClusters(t, map[string]string{"cluster-a": "Example cluster"}))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadSnapshotAndIndependentCopies(t *testing.T) {
	path := testutil.WriteClusters(t, map[string]string{"cluster-b": "Second cluster", "cluster-a": "Example cluster"})
	catalog, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	list := catalog.List()
	if catalog.Len() != 2 || list[0].Ref != "cluster-a" || list[1].Ref != "cluster-b" {
		t.Fatalf("unexpected catalogue order: %+v", list)
	}
	want, ok := catalog.Lookup("cluster-a")
	if !ok || want.Name != "Example cluster" || want.RequestTimeout != 20*time.Second || want.ExpectedClusterID != "00000000-0000-4000-8000-000000000001" || !reflect.DeepEqual(want.Nodes, map[string]string{"node-a": "https://192.0.2.20:1215", "node-b": "https://192.0.2.21:1215"}) || len(want.CAPEM) == 0 {
		t.Fatal("loaded target lost its configuration")
	}
	// Neither consumers nor file replacement can alter the loaded snapshot.
	list[0].Name = "changed"
	list[0].Nodes["node-a"] = "https://other.example"
	list[0].CAPEM[0] = '!'
	copy, _ := catalog.Lookup("cluster-a")
	copy.Nodes["node-a"] = "https://other.example"
	copy.CAPEM[0] = '!'
	if err := os.WriteFile(want.CAFile, []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := catalog.Lookup("cluster-a")
	if !reflect.DeepEqual(got, want) {
		t.Fatal("catalogue changed after loading")
	}
	if _, ok := catalog.Lookup("unknown"); ok {
		t.Fatal("unknown target resolved")
	}
	var absent *Catalog
	if absent.Len() != 0 || absent.List() != nil {
		t.Fatal("nil catalogue is not empty")
	}
}

func TestLoadRejectsAmbiguousYAML(t *testing.T) {
	base := string(fixture(t))
	for name, data := range map[string]string{
		"empty": "", "null": "null", "unsupported version": strings.Replace(base, "version: 2", "version: 1", 1),
		"float version":        strings.Replace(base, "version: 2", "version: 2.0", 1),
		"quoted version":       strings.Replace(base, "version: 2", `version: "1"`, 1),
		"empty catalogue":      "version: 2\nclusters: {}\n",
		"unknown field":        base + "secret: never-echo-this-value\n",
		"unknown nested field": strings.Replace(base, "name:", "password: never-echo-this-value\n    name:", 1),
		"duplicate field":      base + "version: 2\n",
		"duplicate cluster":    strings.Replace(base, "  cluster-a:\n", "  cluster-a: {}\n  cluster-a:\n", 1),
		"duplicate name":       strings.Replace(base, "    name:", "    name: duplicate\n    name:", 1),
		"another document":     base + "---\nversion: 2\n",
		"empty extra document": base + "---\n",
		"non-string ref":       strings.Replace(base, "cluster-a:", "123:", 1),
		"boolean name":         strings.Replace(base, "name: Example cluster", "name: true", 1),
		"numeric ID":           strings.Replace(base, "expected_cluster_id: 00000000-0000-4000-8000-000000000001", "expected_cluster_id: 123", 1),
		"unknown TLS field":    strings.Replace(base, "    tls:\n", "    tls:\n      unknown: true\n", 1),
		"numeric node":         strings.Replace(base, "node-a:", "42:", 1),
		"duplicate node":       strings.Replace(base, "node-a:", "node-a: https://example.com\n      node-a:", 1),
		"boolean endpoint":     strings.Replace(base, "https://192.0.2.20:1215", "true", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, []byte(data)))
			if err == nil || strings.Contains(err.Error(), "never-echo-this-value") {
				t.Fatalf("invalid YAML accepted or payload leaked: %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidTargets(t *testing.T) {
	base := fixture(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"name", ""}, {"name", " padded "}, {"name", "line\nbreak"}, {"name", strings.Repeat("a", 129)},
		{"expected_cluster_id", ""}, {"expected_cluster_id", " padded "},
		{"default_node", "unknown-node"}, {"default_node", " node-a "}, {"default_node", "line\nbreak"},
		{"default_node", strings.Repeat("a", 257)}, {"default_node", true}, {"default_node", 42},
		{"nodes", map[string]string{}}, {"nodes", map[string]string{"node-a": "http://192.0.2.20:1215"}},
		{"nodes", map[string]string{"node-a": "https://user:never-echo-this-value@192.0.2.20:1215"}},
		{"nodes", map[string]string{"node-a": "https://192.0.2.20:1215", "node-b": "https://192.0.2.20:01215/"}},
		{"nodes", map[string]string{"node-a": "https://EXAMPLE.COM:443/", "node-b": "https://example.com"}},
		{"nodes", map[string]string{"": "https://example.com"}},
		{"nodes", map[string]string{" padded ": "https://example.com"}},
		{"nodes", map[string]string{"line\nbreak": "https://example.com"}},
		{"request_timeout", ""}, {"request_timeout", "0s"}, {"request_timeout", "500ms"}, {"request_timeout", "2m1s"},
		{"tls", map[string]string{"ca_file": "relative.pem"}},
		{"tls", map[string]string{"ca_file": filepath.Join(t.TempDir(), "absent.pem")}},
		{"tls", map[string]string{"ca_file": t.TempDir()}},
	} {
		t.Run(fmt.Sprintf("%s=%v", tc.field, tc.value), func(t *testing.T) {
			var doc map[string]any
			if err := yaml.Unmarshal(base, &doc); err != nil {
				t.Fatal(err)
			}
			cluster := doc["clusters"].(map[any]any)["cluster-a"].(map[any]any)
			cluster[tc.field] = tc.value
			data, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(writeConfig(t, data)); err == nil || strings.Contains(err.Error(), "never-echo-this-value") {
				t.Fatalf("invalid target accepted or credentials leaked: %v", err)
			}
		})
	}
	for _, ref := range []string{"", "../target", "has space", "é", strings.Repeat("a", 65)} {
		data := strings.Replace(string(base), "cluster-a:", fmt.Sprintf("%q:", ref), 1)
		if _, err := Load(writeConfig(t, []byte(data))); err == nil {
			t.Fatalf("invalid reference %q accepted", ref)
		}
	}
	names := map[string]string{}
	for i := 0; i <= maxClusters; i++ {
		names[fmt.Sprintf("cluster-%d", i)] = "Example cluster"
	}
	if _, err := Load(testutil.WriteClusters(t, names)); err == nil {
		t.Fatal("oversized catalogue accepted")
	}
}

func TestDefaultNodeReferencesCatalogueNode(t *testing.T) {
	base := strings.Replace(string(fixture(t)), "    name:", "    default_node: node-b\n    name:", 1)
	catalog, err := Load(writeConfig(t, []byte(base)))
	if err != nil {
		t.Fatal(err)
	}
	cluster, ok := catalog.Lookup("cluster-a")
	if !ok || cluster.DefaultNode != "node-b" {
		t.Fatal("default node was lost from the validated catalogue")
	}
	// The omitted field is valid for native-only catalogues, and is not
	// populated from map iteration order, even with only one node.
	catalog, err = Load(testutil.WriteTarget(t, "Native", "id", "", map[string]string{"node-a": "https://192.0.2.20:1215"}))
	if err != nil || catalog.List()[0].DefaultNode != "" {
		t.Fatalf("implicit default node: %v", err)
	}
}

func TestEndpointOrigins(t *testing.T) {
	for raw, want := range map[string]string{
		"https://192.0.2.20:1215/": "https://192.0.2.20:1215", "https://EXAMPLE.COM:00443/": "https://example.com",
		"https://node-a.example:01215": "https://node-a.example:1215", "https://[2001:db8::1]:1215": "https://[2001:db8::1]:1215",
		"https://[2001:db8:0:0::1]/": "https://[2001:db8::1]",
	} {
		if got, err := endpointOrigin(raw); err != nil || got != want {
			t.Errorf("origin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "https://", "https://example.com/api", "https://example.com/%2f", "https://example.com?", "https://example.com#", "https://example.com?key=value", "https://example.com:0", "https://example.com:65536", "https://example.com:", "https://[bad]", "https://2001:db8::1", "https://999.0.2.20", "https://bad..example", "https://-bad.example", " https://example.com", "https://user:secret@example.com", "https://[fe80::1%25eth0]"} {
		if _, err := endpointOrigin(raw); err == nil {
			t.Errorf("invalid origin %q accepted", raw)
		}
	}
}

func TestLoadFilesAndCA(t *testing.T) {
	for _, path := range []string{"relative.yaml", t.TempDir(), filepath.Join(t.TempDir(), "missing.yaml"), writeConfig(t, bytes.Repeat([]byte("x"), maxConfigBytes+1))} {
		if _, err := Load(path); err == nil {
			t.Fatal("invalid configuration file accepted")
		}
	}
	// Certificates are generated in memory; no deployment material is used.
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := func(isCA bool, usage x509.KeyUsage) []byte {
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: isCA, BasicConstraintsValid: true, KeyUsage: usage}
		der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	valid := cert(true, x509.KeyUsageCertSign)
	for _, bundle := range [][]byte{valid, append(bytes.Clone(valid), valid...)} {
		if err := validateCA(bundle); err != nil {
			t.Fatalf("valid public CA bundle rejected: %v", err)
		}
	}
	for name, material := range map[string][]byte{
		"empty": nil, "invalid DER": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid")}),
		"leaf": cert(false, x509.KeyUsageDigitalSignature), "CA without signing usage": cert(true, x509.KeyUsageDigitalSignature),
		"private key":            pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("never-echo-this-value")}),
		"extra text":             append(bytes.Clone(valid), []byte("never-echo-this-value")...),
		"oversized":              bytes.Repeat([]byte("x"), maxCABytes+1),
		"malformed before valid": append([]byte("-----BEGIN CERTIFICATE-----\ninvalid!\n-----END CERTIFICATE-----\n"), valid...),
	} {
		t.Run(name, func(t *testing.T) {
			path := testutil.WriteClusters(t, map[string]string{"cluster-a": "Example cluster"})
			catalog, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(catalog.List()[0].CAFile, material, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || strings.Contains(err.Error(), "never-echo-this-value") {
				t.Fatalf("invalid trust material accepted or contents leaked: %v", err)
			}
		})
	}
}
