package clusterconfig

import (
	"fmt"
	"strings"
	"testing"
)

const v3Example = `version: 3
clusters:
  dev5:
    name: dev5
    cluster_id: 3bc5a684-0f37-4504-9f50-4107ff8d1f24
    endpoint: https://dev5-vip.opensvc.com:1215
    auth:
      profile: customer-sso
      audience: om3-dev5
    request_timeout: 20s
`

func TestVersion3CatalogueAndTLSDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, []byte(v3Example)))
	if err != nil {
		t.Fatal(err)
	}
	cluster, ok := c.LookupID("3bc5a684-0f37-4504-9f50-4107ff8d1f24")
	if !ok || c.Version() != 3 || cluster.Endpoint != "https://dev5-vip.opensvc.com:1215" || cluster.AuthProfile != "customer-sso" || cluster.Audience != "om3-dev5" || cluster.Nodes != nil || cluster.TLSInsecure || len(cluster.CAPEM) != 0 {
		t.Fatalf("wrong catalogue: %+v", cluster)
	}
	for _, replacement := range [][2]string{
		{"cluster_id:", "expected_cluster_id:"},
		{"endpoint: https://dev5-vip.opensvc.com:1215", "endpoint: http://dev5-vip.opensvc.com:1215"},
		{"endpoint: https://dev5-vip.opensvc.com:1215", "endpoint: https://user:secret@dev5-vip.opensvc.com:1215"},
		{"profile: customer-sso", "profile: ''"},
		{"audience: om3-dev5", "audience: ''"},
		{"request_timeout: 20s", "request_timeout: 0s"},
		{"version: 3", "version: 2"},
		{"name: dev5", "name: true"},
		{"auth:", "auth:\n      arbitrary_token_endpoint: https://attacker.test"},
	} {
		if _, err := Load(writeConfig(t, []byte(strings.Replace(v3Example, replacement[0], replacement[1], 1)))); err == nil {
			t.Fatalf("invalid catalogue accepted: %s", replacement[1])
		}
	}
}

func TestVersion3CatalogueSupports800Clusters(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("version: 3\nclusters:\n")
	for i := range 800 {
		fmt.Fprintf(&doc, "  cluster%d:\n    name: cluster%d\n    cluster_id: id-%d\n    endpoint: https://vip%d.test:1215\n    auth:\n      profile: test\n      audience: daemon-%d\n    request_timeout: 20s\n", i, i, i, i, i)
	}
	c, err := Load(writeConfig(t, []byte(doc.String())))
	if err != nil || c.Len() != 800 {
		t.Fatalf("800-cluster catalogue failed: %v", err)
	}
}
