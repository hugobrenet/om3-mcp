package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetClusterHealthInput struct{}

type GetClusterHealthOutput = core.ClusterHealth

type GetNodeStatusInput struct {
	Node string `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard or selector"`
}

type GetNodeStatusOutput = core.NodeStatus

func RegisterClusterTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_cluster_health",
			Title: "Assess cluster health",
			Description: "Compute a deterministic health assessment from the last-known OpenSVC cluster status for the cluster, nodes, heartbeat streams and peer links, and visible actor objects. " +
				"Node issues include stable codes and bounded evidence; remediation options are conditional candidates and are never selected or applied by this read-only tool. " +
				"Missing or stale heartbeat data is reported as unknown and prevents a healthy result. The call does not refresh instance drivers; healthy means no problem in the status currently published by the daemon, not a real-time probe.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ GetClusterHealthInput) (*mcp.CallToolResult, GetClusterHealthOutput, error) {
			health, err := service.GetClusterHealth(ctx)
			if err != nil {
				return nil, GetClusterHealthOutput{}, err
			}
			return nil, health, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_node_status",
			Title:       "Get node status",
			Description: "Read the last-known OpenSVC status, monitor state, capacity statistics, overload thresholds, and bounded heartbeat assessment for one exact node. Uses the cluster status cache; it does not probe the node or refresh drivers.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeStatusInput) (*mcp.CallToolResult, GetNodeStatusOutput, error) {
			status, err := service.GetNodeStatus(ctx, input.Node)
			if err != nil {
				return nil, GetNodeStatusOutput{}, err
			}
			return nil, status, nil
		},
	); err != nil {
		return err
	}
	return nil
}
