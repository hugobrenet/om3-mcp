package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type NodeWaitActionInput struct {
	Node        string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the action, 1 to 60; omitted, 10"`
}

type NodeActionInput struct {
	Node string `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
}

type NodeExecActionOutput = core.NodeExecActionResult

type NodeDrainOutput = core.NodeDrainResult

type NodeAbortOutput = core.NodeAbortResult

type NodeClearOutput = core.NodeClearResult

// nodeActionRoot tells the agent the right the node actions require, which
// get_caller_identity tells it has.
const nodeActionRoot = "Requires root, which get_caller_identity tells."

// nodeExecFollowUp tells the agent what the result of a freeze or an
// unfreeze holds.
const nodeExecFollowUp = "The node runs the action at once: the MCP waits for the end of the execution, wait_seconds, and returns completed with its state, exit_code and error, and for a failure log_tail, the end of its journal; an execution still running returns completed false, to follow with list_daemon_executions and get_node_logs on the node, filtered on exec_id. "

// RegisterNodeActionTools registers the tools of the actions on one node.
// They are registered only when actions are enabled.
func RegisterNodeActionTools(registrar *Registrar, service *core.Service) error {
	for _, declared := range []struct {
		action      core.NodeAction
		name, title string
		description string
		destructive bool
	}{
		{
			action: core.NodeActionFreeze, name: "freeze_node", title: "Freeze node",
			description: "Freeze one OpenSVC node: its daemon no longer starts or stops an instance there on its own, and the node is no longer a candidate to start an ha service; the instances running there keep running. This freezes the node, not its objects: freeze_instance freezes one instance, freeze_object an object on every node. ",
		},
		{
			action: core.NodeActionUnfreeze, name: "unfreeze_node", title: "Unfreeze node",
			description: "Unfreeze one OpenSVC node: its daemon resumes its automatic actions there, which may then start, stop or fail over instances to reach the placement of their objects. An ha service that moved away while the node was frozen does not come back on its own: giveback_object brings it back. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name:        declared.name,
				Title:       declared.title,
				Description: declared.description + nodeExecFollowUp + nodeActionRoot,
				Annotations: actionAnnotations(declared.destructive, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input NodeWaitActionInput) (*mcp.CallToolResult, NodeExecActionOutput, error) {
				result, err := service.RunNodeExecAction(ctx, input.Node, declared.action, time.Duration(input.WaitSeconds)*time.Second)
				if err != nil {
					return nil, NodeExecActionOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "drain_node",
			Title: "Drain node",
			Description: "Drain one OpenSVC node before a maintenance: its daemon freezes the node, then shuts down every svc instance it holds, its standby resources too. Returns instances_up, the svc instances up, warn or stdby up on the node before the drain, the ones it stops. " +
				"The shutdown does not mark the instances stopped: an ha service moves to another node while this one is frozen; a service orchestrated no or start stays down, and does not start again, even after the unfreeze. The node stays frozen after the drain: unfreeze_node is the next step, after the maintenance. The daemon refuses with 409 a drain already asked. " +
				"The MCP follows the orchestration, wait_seconds, and returns completed with its state, succeeded or failed, and error, then the node monitor_state, local_expect and frozen_at read again; a drain of several services often outlasts the wait: completed false is then normal, follow it with get_node_status until local_expect is none, or list_daemon_orchestrations. " +
				nodeActionRoot,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input NodeWaitActionInput) (*mcp.CallToolResult, NodeDrainOutput, error) {
			result, err := service.DrainNode(ctx, input.Node, time.Duration(input.WaitSeconds)*time.Second)
			if err != nil {
				return nil, NodeDrainOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "abort_node",
			Title: "Abort node orchestration",
			Description: "Abort the orchestration running on one OpenSVC node, such as a drain: its daemon sets the local target of the node to none. The shutdown already launched is not interrupted, what follows it is; a node the drain froze stays frozen. " +
				"The daemon accepts the abort at once; its node monitor refuses it when the node runs no orchestration, nothing to abort: the MCP reads the orchestration of the abort and returns its state, refused with the reason as error then. " +
				nodeActionRoot,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input NodeActionInput) (*mcp.CallToolResult, NodeAbortOutput, error) {
			result, err := service.AbortNode(ctx, input.Node)
			if err != nil {
				return nil, NodeAbortOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "clear_node",
			Title: "Clear node",
			Description: "Lift the failed monitor state of one OpenSVC node, freeze failed, unfreeze failed, drain failed or shutdown failed: its daemon sets the node monitor idle. A new drain or freeze lifts a failed state itself: the clear serves when nothing is asked again. " +
				"The MCP refuses a node whose monitor state is not a failure: idle, nothing to clear; an action running, which the daemon would set idle under it. " +
				"Returns previous_state and current_state, read again for 10 seconds at most. " +
				nodeActionRoot,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input NodeActionInput) (*mcp.CallToolResult, NodeClearOutput, error) {
			result, err := service.ClearNode(ctx, input.Node)
			if err != nil {
				return nil, NodeClearOutput{}, err
			}
			return nil, result, nil
		},
	)
}
