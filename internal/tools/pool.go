package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ListStoragePoolsInput struct {
	Pool    string `json:"pool,omitempty" jsonschema:"optional exact pool name; omit for every pool"`
	Node    string `json:"node,omitempty" jsonschema:"optional exact node name for the usage of the pools on that node; omit for the cluster view"`
	PerNode bool   `json:"per_node,omitempty" jsonschema:"optional; true returns one record per node and pool for every node; exclusive with node"`
}

type ListStoragePoolsOutput = core.StoragePoolList

type ListPoolVolumesInput struct {
	Pool        string `json:"pool,omitempty" jsonschema:"optional exact pool name; omit for the volumes of every pool"`
	OrphansOnly bool   `json:"orphans_only,omitempty" jsonschema:"optional; true lists only the volumes no object uses"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call"`
}

type ListPoolVolumesOutput = core.PoolVolumeList

func RegisterPoolTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_storage_pools",
			Title: "List storage pools",
			Description: "Read the capacity and usage of the OpenSVC storage pools: physical size, used and free bytes, and the logical capacity the pool can still hand out to volumes. " +
				"Without node, one cluster record per pool: for a non-shared pool, physical figures sum the nodes and logical figures are those of the node with the least room left. " +
				"With node, the records of that node; with per_node, one record per node and pool. " +
				"Reports daemon facts and errors without verdicts. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListStoragePoolsInput) (*mcp.CallToolResult, ListStoragePoolsOutput, error) {
			pools, err := service.ListStoragePools(ctx, core.ListStoragePoolsOptions{Pool: input.Pool, Node: input.Node, PerNode: input.PerNode})
			if err != nil {
				return nil, ListStoragePoolsOutput{}, err
			}
			return nil, pools, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_pool_volumes",
			Title: "List pool volumes",
			Description: "List the volumes the OpenSVC storage pools serve, with their size, the objects using them, whether they are orphans, and the storage they take of other pools. " +
				"Use orphans_only for volumes no object uses, and the cursor to continue a page. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListPoolVolumesInput) (*mcp.CallToolResult, ListPoolVolumesOutput, error) {
			volumes, err := service.ListPoolVolumes(ctx, core.ListPoolVolumesOptions{
				Pool: input.Pool, OrphansOnly: input.OrphansOnly, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListPoolVolumesOutput{}, err
			}
			return nil, volumes, nil
		},
	); err != nil {
		return err
	}
	return nil
}
