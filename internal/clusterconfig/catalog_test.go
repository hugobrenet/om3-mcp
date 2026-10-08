package clusterconfig

import (
	"fmt"
	"strings"
	"testing"
)

const exampleCatalog = `clusters:
  cluster-a:
    name: cluster-a
    cluster_id: 00000000-0000-4000-8000-000000000001
    endpoint: https://cluster-a-vip.example.com:1215
    auth:
      profile: customer-sso
      audience: opensvc-cluster-a
    request_timeout: 20s
`

func TestCatalogueWithExchangeAndTLSDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, []byte(exampleCatalog)))
	if err != nil {
		t.Fatal(err)
	}
	cluster := c.List()[0]
	if cluster.ID != "00000000-0000-4000-8000-000000000001" || cluster.Endpoint != "https://cluster-a-vip.example.com:1215" || cluster.AuthProfile != "customer-sso" || cluster.Audience != "opensvc-cluster-a" || cluster.TLSInsecure || len(cluster.CAPEM) != 0 {
		t.Fatalf("wrong catalogue: %+v", cluster)
	}
	for _, replacement := range [][2]string{
		{"cluster_id:", "expected_cluster_id:"},
		{"endpoint: https://cluster-a-vip.example.com:1215", "endpoint: http://cluster-a-vip.example.com:1215"},
		{"endpoint: https://cluster-a-vip.example.com:1215", "endpoint: https://user:secret@cluster-a-vip.example.com:1215"},
		{"profile: customer-sso", "profile: ''"},
		{"audience: opensvc-cluster-a", "audience: ''"},
		{"request_timeout: 20s", "request_timeout: 0s"},
		{"clusters:", "version: 3\nclusters:"},
		{"name: cluster-a", "name: true"},
		{"auth:", "auth:\n      arbitrary_token_endpoint: https://attacker.test"},
	} {
		if _, err := Load(writeConfig(t, []byte(strings.Replace(exampleCatalog, replacement[0], replacement[1], 1)))); err == nil {
			t.Fatalf("invalid catalogue accepted: %s", replacement[1])
		}
	}
}

func TestCatalogueSupports800Clusters(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("clusters:\n")
	for i := range 800 {
		fmt.Fprintf(&doc, "  cluster%d:\n    name: cluster%d\n    cluster_id: id-%d\n    endpoint: https://vip%d.test:1215\n    auth:\n      profile: test\n      audience: daemon-%d\n    request_timeout: 20s\n", i, i, i, i, i)
	}
	c, err := Load(writeConfig(t, []byte(doc.String())))
	if err != nil || c.Len() != 800 {
		t.Fatalf("800-cluster catalogue failed: %v", err)
	}
}
