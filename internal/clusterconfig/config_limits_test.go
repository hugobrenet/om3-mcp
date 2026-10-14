package clusterconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/opensvc/om3-mcp/internal/testutil"
	"go.yaml.in/yaml/v2"
)

func TestCatalogueUniqueIDsAndOptionalTLSAndAuth(t *testing.T) {
	data, err := os.ReadFile(testutil.WriteClusters(t, map[string]string{"cluster-a": "First", "cluster-b": "Second"}))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001", 1))
	if _, err := Load(writeConfig(t, data)); err == nil || !strings.Contains(err.Error(), "duplicate cluster identity") {
		t.Fatalf("ambiguous ID accepted: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(fixture(t), &doc); err != nil {
		t.Fatal(err)
	}
	cluster := doc["clusters"].(map[any]any)["cluster-a"].(map[any]any)
	delete(cluster, "tls")
	data, err = yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(writeConfig(t, data))
	if err != nil {
		t.Fatal(err)
	}
	got := catalog.List()[0]
	if got.CAFile != "" || len(got.CAPEM) != 0 {
		t.Fatal("omitted TLS did not select system roots")
	}
	if got.AuthProfile != "" || got.Audience != "" {
		t.Fatal("omitted auth configured an exchange")
	}
}

func TestCatalogueDelegationProvider(t *testing.T) {
	cluster := func(extra map[string]any) map[string]any {
		c := map[string]any{"name": "a", "cluster_id": "cluster-a", "endpoint": "https://192.0.2.20:1215", "request_timeout": "2s"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	load := func(c map[string]any) (*Catalog, error) {
		return Load(testutil.WriteCatalog(t, map[string]any{"a": c}))
	}
	const issuer = "https://idp.example.test/application/o/cluster-a/"
	// The audience defaults to the client ID of the token exchange.
	catalog, err := load(cluster(map[string]any{
		"auth":       map[string]string{"profile": "sso", "audience": "om3-a"},
		"delegation": map[string]string{"issuer": issuer},
	}))
	if err != nil || catalog.List()[0].DelegatedIssuer != issuer || catalog.List()[0].DelegatedAudience != "om3-a" {
		t.Fatalf("catalogue=%+v err=%v", catalog, err)
	}
	catalog, err = load(cluster(map[string]any{"delegation": map[string]string{"issuer": issuer, "audience": "om3-socket"}}))
	if err != nil || catalog.List()[0].DelegatedAudience != "om3-socket" {
		t.Fatalf("explicit audience: %+v %v", catalog, err)
	}
	if catalog, err = load(cluster(nil)); err != nil || catalog.List()[0].DelegatedIssuer != "" {
		t.Fatalf("cluster without delegation: %+v %v", catalog, err)
	}
	for name, delegation := range map[string]map[string]string{
		"no issuer":            {"audience": "om3-a"},
		"issuer not HTTPS":     {"issuer": "http://idp.example.test/", "audience": "om3-a"},
		"issuer with query":    {"issuer": issuer + "?x=1", "audience": "om3-a"},
		"no audience nor auth": {"issuer": issuer},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := load(cluster(map[string]any{"delegation": delegation})); err == nil {
				t.Fatal("invalid delegation accepted")
			}
		})
	}
}
