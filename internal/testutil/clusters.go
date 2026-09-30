// Package testutil provides generated public certificates and fictitious
// cluster configurations for tests. No fixture is taken from a real deployment.
package testutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.yaml.in/yaml/v2"
)

func WriteCA(t testing.TB) string {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "example-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func WriteClusters(t testing.TB, names map[string]string) string {
	t.Helper()
	ca := WriteCA(t)
	clusters := map[string]any{}
	for ref, name := range names {
		clusters[ref] = map[string]any{
			"name": name, "expected_cluster_id": "00000000-0000-4000-8000-000000000001",
			"endpoints": []string{"https://192.0.2.20:1215", "https://192.0.2.21:1215"},
			"tls":       map[string]string{"ca_file": ca}, "request_timeout": "20s",
		}
	}
	data, err := yaml.Marshal(map[string]any{"version": 1, "clusters": clusters})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clusters.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
