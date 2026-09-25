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
	Node string `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
}

type GetNodeConfigOutput = core.NodeConfig

type GetNodeLogsInput struct {
	Node      string `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard or selector"`
	Lines     int    `json:"lines,omitempty" jsonschema:"optional maximum number of recent log entries between 1 and 100; defaults to 50"`
	Component string `json:"component,omitempty" jsonschema:"optional exact OpenSVC component such as daemon/hbctrl; filters the journal PKG field"`
}

type GetNodeLogsOutput = core.NodeLogList

type ListNodeCapabilitiesInput struct {
	Node   string `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call for the same node"`
}

type ListNodeCapabilitiesOutput = core.NodeCapabilityList

type ListNodeDriversInput struct {
	Node   string `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call for the same node"`
}

type ListNodeDriversOutput = core.NodeDriverList

type ListNodePropertiesInput struct {
	Node    string   `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
	Names   []string `json:"names,omitempty" jsonschema:"optional exact property names; combined with sources using AND; at most 32 entries"`
	Sources []string `json:"sources,omitempty" jsonschema:"optional exact collection sources such as probe config or default; combined with names using AND; at most 32 entries"`
	Limit   int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same node and filters"`
}

type ListNodePropertiesOutput = core.NodePropertyList

type ListNodeHardwareInput struct {
	Node    string   `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
	Types   []string `json:"types,omitempty" jsonschema:"optional exact hardware types such as pci or mem; combined with classes and drivers using AND; at most 32 entries"`
	Classes []string `json:"classes,omitempty" jsonschema:"optional exact hardware classes reported by OpenSVC; combined with types and drivers using AND; at most 32 entries"`
	Drivers []string `json:"drivers,omitempty" jsonschema:"optional exact driver names; an empty string selects entries with no reported driver; combined with types and classes using AND; at most 32 entries"`
	Limit   int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"optional opaque next_cursor returned by a previous call with the same node and filters"`
}

type ListNodeHardwareOutput = core.NodeHardwareList

type GetNodeDaemonMetricsInput struct {
	Node     string   `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node through the underscore alias; no wildcard or selector"`
	Names    []string `json:"names,omitempty" jsonschema:"optional exact Prometheus metric family names; combined with prefixes using OR; at most 32 entries"`
	Prefixes []string `json:"prefixes,omitempty" jsonschema:"optional Prometheus metric family name prefixes; combined with names using OR; at most 32 entries"`
	Limit    int      `json:"limit,omitempty" jsonschema:"optional page size in metric families between 1 and 200; defaults to 100"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same node and filters"`
}

type GetNodeDaemonMetricsOutput = core.NodeDaemonMetricList

type ProbeNodeReachabilityInput struct {
	Node string `json:"node" jsonschema:"required exact OpenSVC node name to probe through the daemon proxy path; the underscore local alias is accepted only when explicitly supplied"`
}

type ProbeNodeReachabilityOutput = core.NodeReachabilityProbe

func RegisterNodeTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_node_config",
			Title:       "Get node configuration",
			Description: "Read the bounded raw OpenSVC node configuration file for one exact node, defaulting to the local daemon node through the underscore alias. The MCP always requests daemon-side secret redaction, returns at most 65536 bytes, and does not interpret the configuration. Requires root access to the daemon endpoint.",
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
			Name:  "list_node_hardware",
			Title: "List node hardware",
			Description: "List bounded hardware entries from one OpenSVC node system cache, defaulting to the local node, with exact type, class, and driver filters and stable pagination. " +
				"Preserves paths, descriptions, empty drivers, and duplicate devices without interpreting availability or health. " +
				"This root-only read does not refresh the cache; a missing cache remains an explicit daemon error until asset data has been pushed.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeHardwareInput) (*mcp.CallToolResult, ListNodeHardwareOutput, error) {
			hardware, err := service.ListNodeHardware(ctx, core.ListNodeHardwareOptions{
				Node: input.Node, Types: input.Types, Classes: input.Classes, Drivers: input.Drivers, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodeHardwareOutput{}, err
			}
			return nil, hardware, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_properties",
			Title: "List node properties",
			Description: "List bounded typed properties from one OpenSVC node system cache, defaulting to the local node, with exact name and source filters and stable pagination. " +
				"Preserves string, number, and boolean values plus per-property collection errors without interpreting health. " +
				"This root-only read does not refresh the cache; a missing cache remains an explicit daemon error until asset data has been pushed.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodePropertiesInput) (*mcp.CallToolResult, ListNodePropertiesOutput, error) {
			properties, err := service.ListNodeProperties(ctx, core.ListNodePropertiesOptions{
				Node: input.Node, Names: input.Names, Sources: input.Sources, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodePropertiesOutput{}, err
			}
			return nil, properties, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "get_node_status",
			Title:       "Get node status",
			Description: "Read the last-known OpenSVC status, membership context, monitor state, capacity statistics, overload thresholds, and bounded heartbeat facts for one exact node. Preserves exact stream, alert, peer, and timestamp values without an MCP health or freshness verdict. Uses the cluster status cache; it does not probe the node or refresh drivers.",
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
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_capabilities",
			Title: "List node capabilities",
			Description: "List the exact capability markers cached by OpenSVC for one node, defaulting to the local daemon node, with bounded pagination. " +
				"Capabilities can represent built-in support, environment detections, or driver sub-features; their presence does not prove configuration, use, reachability, or current health. " +
				"This root-only read does not trigger a capability scan or make changes.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeCapabilitiesInput) (*mcp.CallToolResult, ListNodeCapabilitiesOutput, error) {
			capabilities, err := service.ListNodeCapabilities(ctx, core.ListNodeCapabilitiesOptions{
				Node: input.Node, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodeCapabilitiesOutput{}, err
			}
			return nil, capabilities, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_drivers",
			Title: "List node drivers",
			Description: "List the exact driver names registered in one running OpenSVC daemon, defaulting to the local node, with bounded pagination. " +
				"Registration means the daemon knows the driver; it does not prove that runtime dependencies are available, that the driver is configured or used, or that it is healthy. " +
				"This root-only read does not scan capabilities, execute drivers, or make changes.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeDriversInput) (*mcp.CallToolResult, ListNodeDriversOutput, error) {
			drivers, err := service.ListNodeDrivers(ctx, core.ListNodeDriversOptions{
				Node: input.Node, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodeDriversOutput{}, err
			}
			return nil, drivers, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node_daemon_metrics",
			Title: "Get node daemon metrics",
			Description: "Read bounded Prometheus metric families exposed by one OpenSVC daemon, defaulting to the local node, with optional exact-name or prefix filters and family pagination. " +
				"Returns typed counters, gauges, summaries, histograms, labels, and values as source facts for advanced activity or performance diagnosis. " +
				"It does not interpret health, calculate rates, expose workload metrics, or modify daemon state.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeDaemonMetricsInput) (*mcp.CallToolResult, GetNodeDaemonMetricsOutput, error) {
			metrics, err := service.GetNodeDaemonMetrics(ctx, core.GetNodeDaemonMetricsOptions{
				Node: input.Node, Names: input.Names, Prefixes: input.Prefixes, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, GetNodeDaemonMetricsOutput{}, err
			}
			return nil, metrics, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "probe_node_reachability",
			Title: "Probe node reachability",
			Description: "Actively verify that one exact OpenSVC daemon answers through the contacted daemon's node proxy path. " +
				"A successful result means the target returned HTTP 204 and includes the end-to-end round-trip duration; it does not prove cluster, heartbeat, subsystem, object, or resource health. " +
				"The node is required because a default local probe would add little diagnostic evidence. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ProbeNodeReachabilityInput) (*mcp.CallToolResult, ProbeNodeReachabilityOutput, error) {
			probe, err := service.ProbeNodeReachability(ctx, input.Node)
			if err != nil {
				return nil, ProbeNodeReachabilityOutput{}, err
			}
			return nil, probe, nil
		},
	); err != nil {
		return err
	}
	return nil
}
