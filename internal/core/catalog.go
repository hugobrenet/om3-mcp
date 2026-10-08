package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

type ConfiguredCluster struct {
	ClusterID string `json:"cluster_id" jsonschema:"stable configured cluster identifier for tool calls"`
	Name      string `json:"name" jsonschema:"administrator configured cluster display name"`
}

type ClusterCatalogPage struct {
	Items      []ConfiguredCluster `json:"items"`
	NextCursor *string             `json:"next_cursor" jsonschema:"opaque continuation cursor or null for the last page"`
	Provenance Provenance          `json:"provenance"`
}

// ListConfiguredClusters reads only the local catalogue. It neither checks
// per-cluster grants nor contacts daemons or the token endpoint.
func ListConfiguredClusters(catalog *clusterconfig.Catalog, query string, limit int, cursor string) (ClusterCatalogPage, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 || len(query) > 128 || len(cursor) > 1024 {
		return ClusterCatalogPage{}, errors.New("invalid catalogue query, limit (1–200) or cursor")
	}
	filter := strings.ToLower(strings.TrimSpace(query))
	all := make([]ConfiguredCluster, 0, catalog.Len())
	for _, c := range catalog.List() {
		all = append(all, ConfiguredCluster{ClusterID: c.ID, Name: c.Name})
	}
	snapshot, _ := json.Marshal(all)
	sum := sha256.Sum256(append(append(snapshot, 0), []byte(filter)...))
	digest := hex.EncodeToString(sum[:])
	var page struct {
		Offset int    `json:"offset"`
		Digest string `json:"digest"`
	}
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(data, &page) != nil || page.Digest != digest || page.Offset < 0 {
			return ClusterCatalogPage{}, errors.New("invalid cursor for this catalogue and query")
		}
	}
	items := make([]ConfiguredCluster, 0)
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), filter) {
			items = append(items, c)
		}
	}
	if page.Offset > len(items) {
		return ClusterCatalogPage{}, errors.New("invalid catalogue cursor offset")
	}
	end := min(page.Offset+limit, len(items))
	result := ClusterCatalogPage{Items: items[page.Offset:end], Provenance: Provenance{Source: "opensvc_mcp_catalog", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	if end < len(items) {
		page.Offset = end
		page.Digest = digest
		data, _ := json.Marshal(page)
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}
