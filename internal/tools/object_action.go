package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ObjectActionInput struct {
	Path string `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
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
				result, err := service.SubmitObjectAction(ctx, input.Path, declared.action)
				if err != nil {
					return nil, ObjectActionOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	return nil
}
