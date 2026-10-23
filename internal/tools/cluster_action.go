package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ClusterActionInput struct {
	WaitSeconds int `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the orchestration of the node that accepts the action, 1 to 60; omitted, 10"`
}

type ClusterAbortInput struct{}

type ClusterLocksInput struct{}

type ClusterActionOutput = core.ClusterActionResult

type ClusterLocksOutput = core.ClusterLockList

// clusterActionFollowUp tells the agent what the result of a cluster action
// holds.
const clusterActionFollowUp = "The node answering accepts the action and the other nodes converge after it: the MCP follows its orchestration, wait_seconds, and returns completed, state and error, then reads the cluster again: is_frozen, each node and its frozen_at, and pending_nodes, those that have not converged yet. Requires root."

// RegisterClusterLockTools registers the tool that reads the locks of the
// cluster.
func RegisterClusterLockTools(registrar *Registrar, service *core.Service) error {
	return addTool(registrar, &mcp.Tool{
		Name: "list_cluster_locks", Title: "List cluster locks",
		Description: "List the locks the OpenSVC cluster holds: a lock serializes what reads the state of the cluster to change it, such as network/<name> for the address management of a network. Each lock holds its name, the node and holder holding it, when it was acquired and when its lease ends, which frees a lock never released. " +
			"The node speaking for the cluster answers; the daemon answers 503 when none speaks, or a peer it hears does not answer: list_node_locks then reads what one node knows. This tool takes and releases nothing. Requires root.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ClusterLocksInput) (*mcp.CallToolResult, ClusterLocksOutput, error) {
		result, err := service.ListClusterLocks(ctx)
		if err != nil {
			return nil, ClusterLocksOutput{}, err
		}
		return nil, result, nil
	})
}

// RegisterClusterActionTools registers the tools that freeze and unfreeze
// the cluster and abort its orchestration. They are registered only when
// actions are enabled.
func RegisterClusterActionTools(registrar *Registrar, service *core.Service) error {
	for _, declared := range []struct {
		action      core.ClusterAction
		name, title string
		description string
		destructive bool
	}{
		{
			action: core.ClusterActionFreeze, name: "freeze_cluster", title: "Freeze cluster",
			description: "Freeze the whole OpenSVC cluster: the monitor of every node stops its automatic decisions, no failover, no restart, no start of an ha service; the services keep running. Unlike freeze_node, the freeze has the cluster scope: a node down adopts it when it comes back. A cluster already frozen ends at once. The daemon refuses with 409 a freeze while a node holds another global target or runs an action, with the reason. ",
		},
		{
			action: core.ClusterActionUnfreeze, name: "unfreeze_cluster", title: "Unfreeze cluster",
			description: "Unfreeze the whole OpenSVC cluster: the monitors resume their automatic decisions, which may then fail over, restart or start services to reach their placement. The daemon refuses with 409 an unfreeze while a node holds another global target or runs an action, with the reason. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(registrar, &mcp.Tool{
			Name: declared.name, Title: declared.title,
			Description: declared.description + clusterActionFollowUp,
			Annotations: actionAnnotations(declared.destructive, true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, input ClusterActionInput) (*mcp.CallToolResult, ClusterActionOutput, error) {
			result, err := service.RunClusterAction(ctx, declared.action, time.Duration(input.WaitSeconds)*time.Second)
			if err != nil {
				return nil, ClusterActionOutput{}, err
			}
			return nil, result, nil
		}); err != nil {
			return err
		}
	}
	return addTool(registrar, &mcp.Tool{
		Name: "abort_cluster_orchestration", Title: "Abort cluster orchestration",
		Description: "Abort the global orchestration of the OpenSVC cluster, such as a freeze or an unfreeze not converged: every node adopts the global target aborted, then none, and a drain running on any node is interrupted, its node leaving drained, as abort_node would. The actions already running are not interrupted. The MCP follows the orchestration 10 seconds at most. " +
			clusterActionFollowUp,
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ClusterAbortInput) (*mcp.CallToolResult, ClusterActionOutput, error) {
		result, err := service.RunClusterAction(ctx, core.ClusterActionAbort, 0)
		if err != nil {
			return nil, ClusterActionOutput{}, err
		}
		return nil, result, nil
	})
}
