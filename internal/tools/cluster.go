package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetClusterStatusInput struct {
	NodeLimit    int    `json:"node_limit,omitempty" jsonschema:"optional node page size between 1 and 200; defaults to 100"`
	NodeCursor   string `json:"node_cursor,omitempty" jsonschema:"optional nodes.next_cursor returned by a previous call"`
	ObjectLimit  int    `json:"object_limit,omitempty" jsonschema:"optional actor object page size between 1 and 200; defaults to 100"`
	ObjectCursor string `json:"object_cursor,omitempty" jsonschema:"optional objects.next_cursor returned by a previous call"`
}

type GetClusterStatusOutput = core.ClusterStatusSnapshot

type GetClusterConfigInput struct{}

type GetClusterConfigOutput = core.ClusterConfig

func RegisterClusterTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_cluster_config",
			Title:       "Get cluster configuration",
			Description: "Read the bounded raw OpenSVC cluster configuration file for diagnostic context. The MCP always requests daemon-side secret redaction, returns at most 65536 bytes, and does not interpret the configuration. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ GetClusterConfigInput) (*mcp.CallToolResult, GetClusterConfigOutput, error) {
			config, err := service.GetClusterConfig(ctx)
			if err != nil {
				return nil, GetClusterConfigOutput{}, err
			}
			return nil, config, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_cluster_status",
			Title: "Get cluster status snapshot",
			Description: "Read a factual, bounded snapshot of the last-known OpenSVC cluster, node, heartbeat, and visible actor object status. " +
				"Exact daemon values, source timestamps, counts, and truncation metadata are preserved without MCP health verdicts, issue classification, or remediation advice. " +
				"Use cursors to continue node or actor object pages; this read-only call does not refresh instance drivers.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetClusterStatusInput) (*mcp.CallToolResult, GetClusterStatusOutput, error) {
			status, err := service.GetClusterStatus(ctx, core.GetClusterStatusOptions{
				NodeLimit: input.NodeLimit, NodeCursor: input.NodeCursor,
				ObjectLimit: input.ObjectLimit, ObjectCursor: input.ObjectCursor,
			})
			if err != nil {
				return nil, GetClusterStatusOutput{}, err
			}
			return nil, status, nil
		},
	); err != nil {
		return err
	}
	return nil
}
