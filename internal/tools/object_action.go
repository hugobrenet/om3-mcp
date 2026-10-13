package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ObjectActionInput struct {
	Path string `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
}

type StopObjectInput struct {
	Path           string `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
	InterruptSyncs bool   `json:"interrupt_syncs,omitempty" jsonschema:"optional; true ends the data syncs running on the instances instead of waiting for them to end before stopping"`
}

type ObjectActionOutput = core.ObjectActionResult

// objectActionFollowUp tells the agent how to follow the queued orchestration.
const objectActionFollowUp = "Returns as soon as the daemon queued the orchestration, with its orchestration_id: follow it with list_daemon_orchestrations and get_node_logs filtered on that id, and check the outcome with get_object_status. " +
	"Requires operator access on the object namespace."

// RegisterObjectActionTools registers the tools that submit an orchestrated
// action on one object. They are registered only when actions are enabled.
func RegisterObjectActionTools(registrar *Registrar, service *core.Service) error {
	for _, declared := range []struct {
		action      core.ObjectAction
		name, title string
		description string
		destructive bool
		idempotent  bool
	}{
		{
			action: core.ObjectActionFreeze, name: "freeze_object", title: "Freeze object",
			description: "Freeze one OpenSVC svc or vol object on all its nodes: the daemon stops its automatic actions, such as an ha failover or a restart of a degraded monitored resource, until it is unfrozen. Running instances keep running. ",
			idempotent:  true,
		},
		{
			action: core.ObjectActionUnfreeze, name: "unfreeze_object", title: "Unfreeze object",
			description: "Unfreeze one OpenSVC svc or vol object on all its nodes: the daemon resumes its automatic actions, which may then start, fail over or restart instances to reach the object placement. ",
			idempotent:  true,
		},
		{
			action: core.ObjectActionAbort, name: "abort_object_orchestration", title: "Abort object orchestration",
			description: "Abort the orchestration running on one OpenSVC object: the daemon drops the pending target state. An action already running on an instance is not interrupted: the abort is queued and applies when that action ends. ",
			idempotent:  true,
		},
		{
			action: core.ObjectActionStart, name: "start_object", title: "Start object",
			description: "Start one OpenSVC svc object: the daemon starts the instances its placement and topology call for, a failover service on one node, a flex service on its target number of nodes. A frozen service is started too, and stays frozen. " +
				"The daemon refuses with 409 a service already started, and a start it cannot run, such as a failover service in warn state or an unprovisioned one, with the reason. ",
			idempotent: true,
		},
		{
			action: core.ObjectActionRestart, name: "restart_object", title: "Restart object",
			description: "Restart one OpenSVC svc object: the daemon stops then starts each running instance, one node after the other, so the service is interrupted on each node in turn. " +
				"Instances that are down or in standby are left as they are, and a service already stopped stays stopped; a restart still clears the stopped flag of its instances, so the daemon may then start an ha service on its own. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name:        declared.name,
				Title:       declared.title,
				Description: declared.description + objectActionFollowUp,
				Annotations: actionAnnotations(declared.destructive, declared.idempotent),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input ObjectActionInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
				result, err := service.SubmitObjectAction(ctx, input.Path, declared.action, core.ObjectActionOptions{})
				if err != nil {
					return nil, ObjectActionOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "stop_object",
			Title: "Stop object",
			Description: "Stop one OpenSVC svc object on all its nodes: the daemon stops every running instance, which makes the service unavailable. A frozen service is stopped too, and stays frozen; an unfrozen ha service is not restarted by the daemon after this stop. " +
				"By default, a stop waits for the data syncs running on the instances to end; interrupt_syncs ends them instead. Stopping a service already stopped is accepted and changes nothing. " +
				objectActionFollowUp,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input StopObjectInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
			result, err := service.SubmitObjectAction(ctx, input.Path, core.ObjectActionStop, core.ObjectActionOptions{InterruptSyncs: input.InterruptSyncs})
			if err != nil {
				return nil, ObjectActionOutput{}, err
			}
			return nil, result, nil
		},
	)
}
