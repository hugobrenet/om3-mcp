package tools

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type StartInstanceInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or app#2, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	DisableRollback bool     `json:"disable_rollback,omitempty" jsonschema:"optional; true leaves started the resources started before a failure, rather than stopping them"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives: the daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type StopInstanceInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or app#2, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	InterruptSyncs  bool     `json:"interrupt_syncs,omitempty" jsonschema:"optional; true ends the data syncs running on the instance instead of waiting for them to end before stopping"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives: the daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type FreezeInstanceInput struct {
	Path            string `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	ConfigUpdatedAt string `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives: the daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type UnprovisionInstanceInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	Leader          bool     `json:"leader,omitempty" jsonschema:"optional; true also unprovisions the resources the instances share, such as a shared disk, which the leader of the placement holds; false by default, the shared resources left for the other instances"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives: the daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type MarkInstanceUnprovisionedInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or disk#1, at most 50: the resources freed by hand; omitted, the whole instance"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives: the daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type ClearInstanceInput struct {
	Path string `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
	Node string `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
}

type InstanceActionOutput = core.InstanceActionResult

type InstanceClearOutput = core.InstanceClearResult

// instanceExecutionFollowUp tells the agent what the result of an instance
// action holds, and how to follow an execution still running.
const instanceExecutionFollowUp = "The node runs the action at once, outside of the orchestration of the object: the MCP waits for the end of the execution, wait_seconds, and returns completed with its state, exit_code and error; an execution still running returns completed false, to follow with list_daemon_executions and get_node_logs on the node, filtered on exec_id. " +
	"An execution that failed returns log_tail, the end of its journal on the node, where its cause is; get_node_logs with exec_id gives more. " +
	"Reading the execution and its journal requires root access: for an operator, execution_error says so, and get_instance_status gives the outcome. A 410 in execution_error is an execution the daemon ran and has dropped since. "

// instanceActionFollowUp adds the role the instance actions require.
const instanceActionFollowUp = instanceExecutionFollowUp + "Requires operator access on the object namespace."

// RegisterInstanceActionTools registers the tools of the actions on one
// instance of an object. They are registered only when actions are enabled.
func RegisterInstanceActionTools(registrar *Registrar, service *core.Service) error {
	run := func(ctx context.Context, path, node string, action core.InstanceAction, options core.InstanceActionOptions, waitSeconds int) (*mcp.CallToolResult, InstanceActionOutput, error) {
		options.Wait = time.Duration(waitSeconds) * time.Second
		result, err := service.RunInstanceAction(ctx, path, node, action, options)
		if err != nil {
			return nil, InstanceActionOutput{}, err
		}
		return nil, result, nil
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "start_instance",
			Title: "Start instance",
			Description: "Start the instance of one OpenSVC svc or vol object on one node, or some of its resources: the daemon of that node runs the start, and lifts the stopped flag of the instance. " +
				"The MCP refuses to start an instance of a failover service while another of its instances is up or warn: the daemon would run it on two nodes at once; switch_object moves it. " +
				"On a flex service above its target number of instances, the daemon stops a non-leader instance in return. The daemon refuses with 403 a start beyond the claims of the namespace. " +
				instanceActionFollowUp,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StartInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionStart, core.InstanceActionOptions{Rids: input.Rids, DisableRollback: input.DisableRollback, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "stop_instance",
			Title: "Stop instance",
			Description: "Stop the instance of one OpenSVC svc or vol object on one node, or some of its resources: the daemon of that node runs the stop. " +
				"Without rids, on a service orchestrated ha or start, the stop sets the stopped flag of this instance only: the daemon does not start it again here, but on an unfrozen ha failover service another instance becomes candidate and the daemon starts the service there, a failover. To stop the service everywhere, stop_object; to stop this instance without the daemon acting, freeze_object first. " +
				"By default, a stop waits for the data syncs running on the instance to end; interrupt_syncs ends them instead. " +
				instanceActionFollowUp,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StopInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionStop, core.InstanceActionOptions{Rids: input.Rids, InterruptSyncs: input.InterruptSyncs, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "restart_instance",
			Title: "Restart instance",
			Description: "Restart the instance of one OpenSVC svc or vol object on one node, or some of its resources: the daemon of that node stops then starts it, which interrupts the service on that node, and starts an instance that was down. " +
				"The MCP refuses to restart an instance of a failover service while another of its instances is up or warn, as for start_instance. " +
				instanceActionFollowUp,
			Annotations: actionAnnotations(true, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StartInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionRestart, core.InstanceActionOptions{Rids: input.Rids, DisableRollback: input.DisableRollback, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "unprovision_instance",
			Title: "Unprovision instance",
			Description: "Unprovision the instance of one OpenSVC svc or vol object on one node, or some of its resources, destroying the data its disk and fs resources hold: it always acts on them, and cannot mark an instance unprovisioned without touching it. " +
				"To record as unprovisioned resources a sysadmin freed by hand, touching nothing, call mark_instance_unprovisioned instead. " +
				"The daemon of that node stops then unprovisions them, triggers included, outside of the orchestration of the object. " +
				"Without leader, the resources the instances share are left; with leader, they are unprovisioned too, which the other instances lose: unprovision_object unprovisions every node in order. " +
				"The daemon does not look at what the instance runs: the MCP refuses an instance up or warn; stop it first, stop_instance. A resource with unprovision=false fails the action. " +
				instanceExecutionFollowUp + "Requires admin on the object namespace.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UnprovisionInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionUnprovision, core.InstanceActionOptions{Rids: input.Rids, Leader: input.Leader, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "mark_instance_unprovisioned",
			Title: "Mark instance unprovisioned",
			Description: "Record the resources of the instance of one OpenSVC svc or vol object on one node, or some of them, as unprovisioned, touching nothing: for a sysadmin who freed them by hand, such as a logical volume removed or a filesystem destroyed. " +
				"The resources still there are then orphaned: nothing frees them later, and a disk or a filesystem may outlive the object. The record is undone by provisioning again, provision_object, with adopt for resources still there. To unprovision for real, destroying the data, unprovision_instance. " +
				"Nothing is stopped: the MCP does not refuse an instance up, whose resources keep running, recorded unprovisioned. " +
				instanceExecutionFollowUp + "Requires admin on the object namespace.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input MarkInstanceUnprovisionedInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionUnprovision, core.InstanceActionOptions{Rids: input.Rids, StateOnly: true, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	for _, declared := range []struct {
		action      core.InstanceAction
		name, title string
		description string
		destructive bool
	}{
		{
			action: core.InstanceActionFreeze, name: "freeze_instance", title: "Freeze instance",
			description: "Freeze the instance of one OpenSVC svc or vol object on one node: the daemon stops its automatic actions on that node only, such as a restart of a degraded monitored resource, and no longer elects it to run an ha service; the other instances are left as they are, and the instance keeps running. freeze_object freezes every node. ",
		},
		{
			action: core.InstanceActionUnfreeze, name: "unfreeze_instance", title: "Unfreeze instance",
			description: "Unfreeze the instance of one OpenSVC svc or vol object on one node: the daemon resumes its automatic actions on that node, which may then start, fail over or restart the instance to reach the object placement. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name:        declared.name,
				Title:       declared.title,
				Description: declared.description + instanceActionFollowUp,
				Annotations: actionAnnotations(declared.destructive, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input FreezeInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
				return run(ctx, input.Path, input.Node, declared.action, core.InstanceActionOptions{ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
			},
		); err != nil {
			return err
		}
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "clear_instance",
			Title: "Clear instance",
			Description: "Lift the failed monitor state of the instance of one OpenSVC object on one node, such as start failed, which holds the orchestration of that instance: the daemon sets its monitor idle. The resources are left as they are. " +
				"The daemon lifts a start failed itself when the object becomes up on another node: the clear serves when the object stays down. " +
				"The MCP refuses an instance whose monitor state is not a failure: idle, nothing to clear; an action running, which the daemon would set idle under it. " +
				"Returns previous_state and current_state, read again for 10 seconds at most. Requires operator access on the object namespace.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ClearInstanceInput) (*mcp.CallToolResult, InstanceClearOutput, error) {
			result, err := service.ClearInstance(ctx, input.Path, input.Node)
			if err != nil {
				return nil, InstanceClearOutput{}, err
			}
			return nil, result, nil
		},
	)
}
