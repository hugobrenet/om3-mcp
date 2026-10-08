package tools

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ListClustersInput struct {
	Query  string `json:"query,omitempty" jsonschema:"optional case-insensitive substring of the cluster display name"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque next_cursor from the previous page for the same query"`
}

func RegisterCatalogTool(r *Registrar, catalog *clusterconfig.Catalog) error {
	return registerTool(r, &mcp.Tool{
		Name: "list_clusters", Title: "List configured clusters",
		Description: "Discover names and cluster_id values from the administrator catalogue, with search and pagination. Lists all configured clusters, without checking user grants or availability. Does not query daemons. Resolve the operator's cluster before calling daemon tools; if only a node is named or several clusters match, ask for clarification. Never resolve a cluster from a node name alone.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListClustersInput) (*mcp.CallToolResult, core.ClusterCatalogPage, error) {
		out, err := core.ListConfiguredClusters(catalog, in.Query, in.Limit, in.Cursor)
		return nil, out, err
	}, false)
}
