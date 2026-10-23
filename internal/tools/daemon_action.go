package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type DaemonNodeInput struct {
	Node string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
}

type SetDaemonLogLevelInput struct {
	Node  string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	Level string `json:"level" jsonschema:"info, warn, error or none: the level below which the daemon log is not emitted; debug and trace are refused, the daemon log being given nothing below info"`
}

type HeartbeatActionInput struct {
	Node        string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	Name        string `json:"name" jsonschema:"a heartbeat, such as hb#1, which addresses both its streams hb#1.rx and hb#1.tx, or one stream, such as hb#1.rx; the hb# prefix is optional"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the streams to reach the end of the action, 1 to 60; omitted, 10"`
}

type DaemonWaitInput struct {
	Node        string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the restart, 1 to 60; omitted, 10"`
}

type DaemonLogLevelOutput = core.DaemonLogLevel

type HeartbeatActionOutput = core.HeartbeatActionResult

type DaemonListenerRestartOutput = core.DaemonListenerRestart

type DaemonRestartOutput = core.DaemonRestart

// RegisterDaemonLogTools registers the tool that reads the level of the
// daemon log of a node.
func RegisterDaemonLogTools(registrar *Registrar, service *core.Service) error {
	return addTool(registrar, &mcp.Tool{
		Name: "get_daemon_log_level", Title: "Get daemon log level",
		Description: "Read the level below which the OpenSVC daemon of one node does not emit its log, to journald: info, warn, error or none. info is the most verbose level of that log. Requires root.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DaemonNodeInput) (*mcp.CallToolResult, DaemonLogLevelOutput, error) {
		result, err := service.GetDaemonLogLevel(ctx, input.Node)
		if err != nil {
			return nil, DaemonLogLevelOutput{}, err
		}
		return nil, result, nil
	})
}

// RegisterDaemonActionTools registers the tools that act on the daemon of a
// node: its log level, its heartbeat streams, its listener and itself. They
// are registered only when actions are enabled.
func RegisterDaemonActionTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "set_daemon_log_level", Title: "Set daemon log level",
		Description: "Set the level below which the OpenSVC daemon of one node does not emit its log: info, warn, error or none, to quiet a daemon during a noisy incident. The daemon log is given nothing below info: debug and trace are refused, their feed being the daemon audit, which this MCP does not offer. " +
			"The level holds until the daemon restarts, which takes the level of its configuration. Returns the level set and the one before. Requires root.",
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetDaemonLogLevelInput) (*mcp.CallToolResult, DaemonLogLevelOutput, error) {
		result, err := service.SetDaemonLogLevel(ctx, input.Node, input.Level)
		if err != nil {
			return nil, DaemonLogLevelOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	heartbeatFollowUp := "The daemon queues the action at once, without an id: the MCP reads the streams again with the node status, wait_seconds, and returns completed, each stream addressed with its state, updated_at and whether each peer beats. That status is the view of the peer the MCP talks to, which no route lets it read on the node itself: it arrives late, and may still show the streams as they were. The daemon refuses with 400 a heartbeat not configured, naming those it has. Stopping a stream is not offered. Requires root."
	for _, declared := range []struct {
		action      core.HeartbeatAction
		name, title string
		description string
		destructive bool
	}{
		{
			action: core.HeartbeatStart, name: "start_heartbeat", title: "Start heartbeat",
			description: "Start a heartbeat stream of one OpenSVC node that is stopped: the daemon registers it again, running. A stream already running is left as it is. ",
		},
		{
			action: core.HeartbeatRestart, name: "restart_heartbeat", title: "Restart heartbeat",
			description: "Restart a heartbeat stream of one OpenSVC node, stopped then started, such as one stuck or failed: the stream is cut for the cycle, and one that fails to start stays cut. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + heartbeatFollowUp,
			Annotations: actionAnnotations(declared.destructive, true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input HeartbeatActionInput) (*mcp.CallToolResult, HeartbeatActionOutput, error) {
			result, err := service.RunHeartbeatAction(ctx, input.Node, input.Name, declared.action, time.Duration(input.WaitSeconds)*time.Second)
			if err != nil {
				return nil, HeartbeatActionOutput{}, err
			}
			return nil, result, nil
		}); err != nil {
			return err
		}
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "restart_daemon_listener", Title: "Restart daemon listener",
		Description: "Restart the HTTPS listener of the OpenSVC daemon of one node, api.inet, the only listener with a life cycle: the API of the node is cut while it rebinds, the daemon answering before; a rebind that fails leaves the node without its API for the MCP and its peers. " +
			"The MCP reads the listener again with the node status, wait_seconds, and returns completed, its state, updated_at, address and port; a read failing while the API restarts is told in status_error. Stopping or starting the listener is not offered. Requires root.",
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DaemonWaitInput) (*mcp.CallToolResult, DaemonListenerRestartOutput, error) {
		result, err := service.RestartDaemonListener(ctx, input.Node, time.Duration(input.WaitSeconds)*time.Second)
		if err != nil {
			return nil, DaemonListenerRestartOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "restart_daemon", Title: "Restart daemon",
		Description: "Restart the OpenSVC daemon of one node, in the background: the instances keep their state, and the peers, told the node is in maintenance, take nothing over. " +
			"The MCP reads the node again until a daemon started after the request answers, wait_seconds, and returns the pid and started_at before and after, and restarted. Restarting the daemon of the node the MCP talks to cuts the MCP from the cluster for the restart: the read back then fails, told in status_error, the restart being launched all the same. Stopping or shutting down the daemon is not offered. Requires root.",
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DaemonWaitInput) (*mcp.CallToolResult, DaemonRestartOutput, error) {
		result, err := service.RestartDaemon(ctx, input.Node, time.Duration(input.WaitSeconds)*time.Second)
		if err != nil {
			return nil, DaemonRestartOutput{}, err
		}
		return nil, result, nil
	})
}
