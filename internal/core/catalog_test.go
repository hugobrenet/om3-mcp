package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func TestCatalogueDiscoveryPaginationAndNoPrivateFields(t *testing.T) {
	clusters := map[string]any{}
	for _, id := range []string{"a", "b", "c"} {
		clusters[id] = map[string]any{"name": "dev" + id, "cluster_id": id, "endpoint": "https://private.test", "auth": map[string]string{"profile": "private-profile", "audience": "private-audience"}, "request_timeout": "20s"}
	}
	c, err := clusterconfig.Load(testutil.WriteYAML(t, map[string]any{"version": 3, "clusters": clusters}))
	if err != nil {
		t.Fatal(err)
	}
	cursor := ""
	var ids []string
	for {
		page, err := ListConfiguredClusters(c, "DEV", 1, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			ids = append(ids, item.ClusterID)
		}
		data, _ := json.Marshal(page)
		if strings.Contains(string(data), "private") {
			t.Fatal("private catalogue fields leaked")
		}
		if page.Provenance.Source != "opensvc_mcp_catalog" {
			t.Fatal("incorrect provenance")
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("wrong pages: %v", ids)
	}
	if _, err := ListConfiguredClusters(c, "other", 1, cursor); err == nil {
		t.Fatal("cursor accepted for other query")
	}
	page, err := ListConfiguredClusters(c, "missing", 0, "")
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil {
		t.Fatal("empty page invalid")
	}
	for _, limit := range []int{-1, 201} {
		if _, err := ListConfiguredClusters(c, "", limit, ""); err == nil {
			t.Fatal("invalid page limit accepted")
		}
	}
}
