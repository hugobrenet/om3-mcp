package clusterconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/opensvc/om3-mcp/internal/testutil"
	"go.yaml.in/yaml/v2"
)

func TestCatalogueUniqueIDsOptionalTLSAndNodeBounds(t *testing.T) {
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
	byID, ok := catalog.LookupID(got.ExpectedClusterID)
	if !ok || byID.Ref != got.Ref {
		t.Fatal("ID lookup failed")
	}
	if _, ok := catalog.LookupID("unknown"); ok {
		t.Fatal("unknown ID resolved")
	}
	nodes := make(map[string]string)
	for i := 0; i <= maxNodes; i++ {
		nodes[strings.Repeat("a", i+1)] = "https://example.com"
	}
	cluster["nodes"] = nodes
	data, err = yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(writeConfig(t, data)); err == nil || !strings.Contains(err.Error(), "between 1 and") {
		t.Fatalf("oversized nodes accepted: %v", err)
	}
}
