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
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type StopInstanceInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or app#2, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	InterruptSyncs  bool     `json:"interrupt_syncs,omitempty" jsonschema:"optional; true ends the data syncs running on the instance instead of waiting for them to end before stopping"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type FreezeInstanceInput struct {
	Path            string `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	ConfigUpdatedAt string `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type UnprovisionInstanceInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	Leader          bool     `json:"leader,omitempty" jsonschema:"optional; true also unprovisions the resources the instances share, such as a shared disk, which the leader of the placement holds; false by default, the shared resources left for the other instances"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type MarkInstanceUnprovisionedInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or disk#1, at most 50: the resources freed by hand; omitted, the whole instance"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type RunTaskInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node to run the tasks on; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids" jsonschema:"the tasks to run, each an exact task#<name> resource id, one at least, at most 50"`
	Env             []string `json:"env,omitempty" jsonschema:"optional KEY=VALUE variables exported to the commands of the tasks, after those of their configuration, at most 32, each at most 4096 bytes on one line, the key of capitals, digits and underscores; PATH, LD_* and OPENSVC_* are refused; their values are never returned"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
	WaitSeconds     int      `json:"wait_seconds,omitempty" jsonschema:"optional seconds to wait for the end of the execution, 1 to 60; omitted, 10"`
}

type InstanceResourcesInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC svc or vol object path; no wildcard or selector"`
	Node            string   `json:"node" jsonschema:"the exact name of the node of the instance; no wildcard, selector or the _ alias"`
	Rids            []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or app#2, at most 50; omitted, the whole instance; the daemon refuses an id the object has not"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time: leave it out unless this call follows update_object_config in this turn, then the last_modified it gave; the status or frozen times of a listing are not it. The daemon refuses with 409 a node whose configuration is older"`
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
				"The standby resources of a passive node, standby=true, are started by start_instance_standby instead, without starting the service there. " +
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
				"A stop leaves the standby resources up: before a reboot or the shutdown of the node, shutdown_instance stops them too. " +
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
			Description: "Unprovision the instance of one OpenSVC svc or vol object on one node, or some of its resources, destroying the data its disk and fs resources hold, to free the storage of that node: it always acts on them, and cannot mark an instance unprovisioned without touching it. " +
				"The object and its other instances are left: it is not stopped, nor purged. " +
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
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "run_task",
			Title: "Run task",
			Description: "Run tasks of the instance of one OpenSVC svc or vol object on one node, task#<name> resources, each the command, script or container its configuration names: the daemon of that node runs them at once, outside of the orchestration, and a task may do anything its command does. Name the tasks in rids: a run without them would run every task. " +
				"A task whose author asks a human to confirm each run, confirmation=true, is refused: the MCP does not confirm for them; an operator runs om <path> instance run --rid task#<name> --confirm on the node. " +
				"env exports variables to the commands; their values are never returned. The task decides whether it runs: run_requires or max_parallel refuse a run in the execution, the reason in log_tail. " +
				"Returns, per task, output_logged, its log keyword: true, get_node_logs on the node filtered on the exec_id gives its stdout and stderr; and its last run read again, last_run_exitcode and last_run_at, this_run telling whether it is the run of this call. A task longer than wait_seconds returns completed false, to follow with list_daemon_executions. " +
				instanceExecutionFollowUp + "Requires admin on the object namespace.",
			Annotations: actionAnnotations(true, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input RunTaskInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionRun, core.InstanceActionOptions{Rids: input.Rids, Env: input.Env, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "start_instance_standby",
			Title: "Start instance standby resources",
			Description: "Start the standby resources of the instance of one OpenSVC svc or vol object on one node, those standby=true, such as a disk or a filesystem kept up on a passive node, or some of them: the other resources are left as they are. " +
				"The daemon also lifts the stopped flag of every resource of the instance, the flag a stop with rids sets, which gives back to the daemon the right to start again a resource stopped that way. The instance is not flagged started, nor its stopped flag lifted: the orchestration of the object is unchanged. " +
				instanceActionFollowUp,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StartInstanceInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionStartStandby, core.InstanceActionOptions{Rids: input.Rids, DisableRollback: input.DisableRollback, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "shutdown_instance",
			Title: "Shutdown instance",
			Description: "Shut down the instance of one OpenSVC svc or vol object on one node, or some of its resources, as before the shutdown of the node: the daemon of that node stops every resource, the standby ones a stop leaves up too, interrupts the data syncs, and runs the pre and post shutdown triggers. " +
				"It does not flag the instance stopped: on an object orchestrated ha, the daemon would start it again here, or fail over, at once. The MCP refuses it there unless the object, the instance or the node is frozen: freeze_instance on that node first, then shutdown_instance again. Without rids, it also lifts the stopped flag of the resources. " +
				instanceActionFollowUp,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input InstanceResourcesInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
			return run(ctx, input.Path, input.Node, core.InstanceActionShutdown, core.InstanceActionOptions{Rids: input.Rids, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
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
			action: core.InstanceActionPGUpdate, name: "update_instance_caps", title: "Update instance caps",
			description: "Apply the resource limits of the configuration, the pg_* keywords, to the instance of one OpenSVC svc or vol object on one node, or to some of its resources: the daemon writes them in the cgroups of the instance on that node, as a start does. A memory limit lowered under what the instance uses ends its processes. " +
				"To change a value first, update_object_config; to apply the limits on every node by orchestration, cap_object. " +
				"The writes are not atomic: an execution that fails may have written part of the limits; log_tail names the file refused, the others are applied. ",
			destructive: true,
		},
		{
			action: core.InstanceActionPGReset, name: "reset_instance_caps", title: "Reset instance caps",
			description: "Lift every limit on the cgroups of the instance of one OpenSVC svc or vol object on one node, or of some of its resources, within the slice of the object: the daemon writes what a node that never capped anything holds, including a capping the configuration does not know, left by an older agent, by systemd or by hand. " +
				"The instance runs without limits until the next update_instance_caps or start, which writes the pg_* keywords configured again; to lift one for good, set it to default with cap_object. " +
				"The writes are not atomic: an execution that fails may have lifted part of the limits; log_tail names the file refused, the others are lifted. ",
		},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name:        declared.name,
				Title:       declared.title,
				Description: declared.description + instanceExecutionFollowUp + "Requires admin on the object namespace.",
				Annotations: actionAnnotations(declared.destructive, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input InstanceResourcesInput) (*mcp.CallToolResult, InstanceActionOutput, error) {
				return run(ctx, input.Path, input.Node, declared.action, core.InstanceActionOptions{Rids: input.Rids, ConfigUpdatedAt: input.ConfigUpdatedAt}, input.WaitSeconds)
			},
		); err != nil {
			return err
		}
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
