package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetNodeStatusInput struct {
	Node string `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard or selector"`
}

type GetNodeStatusOutput = core.NodeStatus

type GetNodeConfigInput struct {
	Node string `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard or selector"`
}

type GetNodeConfigOutput = core.NodeConfig

type GetNodeLogsInput struct {
	Node      string `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard or selector"`
	Lines     int    `json:"lines,omitempty" jsonschema:"optional maximum number of recent log entries between 1 and 100; defaults to 50"`
	Component string `json:"component,omitempty" jsonschema:"optional exact OpenSVC component such as daemon/hbctrl; filters the journal PKG field"`
}

type GetNodeLogsOutput = core.NodeLogList

func RegisterNodeTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_node_config",
			Title:       "Get node configuration",
			Description: "Read the bounded raw OpenSVC node configuration file for one exact node. The MCP always requests daemon-side secret redaction, returns at most 65536 bytes, and does not interpret the configuration. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeConfigInput) (*mcp.CallToolResult, GetNodeConfigOutput, error) {
			config, err := service.GetNodeConfig(ctx, input.Node)
			if err != nil {
				return nil, GetNodeConfigOutput{}, err
			}
			return nil, config, nil
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
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_node_logs",
			Title:       "Get node logs",
			Description: "Read bounded recent OpenSVC om journal entries on one exact node, optionally filtering by OpenSVC component. Includes the systemd unit when journald provides it. This finite read does not follow the stream, does not return systemd manager messages or workload stdout, and requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeLogsInput) (*mcp.CallToolResult, GetNodeLogsOutput, error) {
			logs, err := service.GetNodeLogs(ctx, core.GetNodeLogsOptions{
				Node: input.Node, Lines: input.Lines, Component: input.Component,
			})
			if err != nil {
				return nil, GetNodeLogsOutput{}, err
			}
			return nil, logs, nil
		},
	); err != nil {
		return err
	}
	return nil
}
