package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ProvisionObjectInput struct {
	Path  string `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
	Adopt bool   `json:"adopt,omitempty" jsonschema:"optional; true takes over the system resources found holding what the provision would take, such as the replicated storage a restored object finds, rather than refusing them"`
}

type ResizeObjectInput struct {
	Path            string `json:"path" jsonschema:"the exact canonical OpenSVC vol object path; no wildcard or selector"`
	Size            string `json:"size,omitempty" jsonschema:"optional size the volume is to hold, such as 11g or 12Gi, or the amount to add, such as +1g; omitted, the size configured, which finishes a resize stopped part way"`
	ConfigUpdatedAt string `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives, of a size written with it: the resize waits for that configuration on every node; used without size"`
}

type CapObjectInput struct {
	Path            string   `json:"path" jsonschema:"the exact canonical OpenSVC object path; no wildcard or selector"`
	Sets            []string `json:"sets,omitempty" jsonschema:"optional cap operations, each [<section>.]pg_<name>[@<node>]=<value>, such as pg_cpus=0-1 or app#1.pg_mem_limit=512m; the value default lifts the cap; none applies the caps configured"`
	ConfigUpdatedAt string   `json:"config_updated_at,omitempty" jsonschema:"optional RFC 3339 time, the last_modified update_object_config gives, of pg_* keywords written with it: the cap waits for that configuration on every node; used without sets"`
}

type EnableObjectInput struct {
	Path string   `json:"path" jsonschema:"the exact canonical OpenSVC svc object path; no wildcard or selector"`
	Rids []string `json:"rids,omitempty" jsonschema:"optional exact resource ids, such as fs#1 or app#2, at most 50; omitted, the whole service"`
}

type ObjectEnableOutput = core.ObjectEnableResult

// lifecycleFollowUp tells the agent how to follow the orchestration a
// lifecycle action queued, and the role it requires.
const lifecycleFollowUp = "Returns as soon as the daemon queued the orchestration, with its orchestration_id: follow it with list_daemon_orchestrations and get_node_logs filtered on that id, and check the outcome with get_object_status. " +
	"Requires admin on the object namespace."

// RegisterObjectLifecycleTools registers the tools of the lifecycle of an
// object: provision, resize, cap, enable, disable and delete. They are
// registered only when actions are enabled.
func RegisterObjectLifecycleTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "provision_object",
			Title: "Provision object",
			Description: "Provision one OpenSVC svc or vol object on its nodes: the daemon creates what its resources hold, a volume, a filesystem, a disk, then starts the leader instance, and lifts the stopped flag create_object leaves. This is what follows create_object. On an object already provisioned, it starts it. " +
				"Without adopt, each node first checks that nothing holds what the object would take, a filesystem mounted on its mount point, a volume group of its name, a device in use: a node refusing ends the orchestration as a provision failure, and nothing is provisioned. " +
				lifecycleFollowUp,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ProvisionObjectInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
			result, err := service.SubmitObjectAction(ctx, input.Path, core.ObjectActionProvision, core.ObjectActionOptions{Adopt: input.Adopt})
			if err != nil {
				return nil, ObjectActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "resize_object",
			Title: "Resize volume",
			Description: "Grow one OpenSVC vol object: the daemon writes the size to its configuration, within the claims of its pool, then queues the orchestration growing the volume on the node holding it. " +
				"size is absolute, such as 11g, or an amount to add, such as +1g; a size under the one configured is refused with 400; omitted, the resize converges to the size configured, which finishes a resize stopped part way. " +
				"The orchestration fails when no instance of the volume is up: it grows on the node holding it. The daemon refuses with 409 a resize already running. " +
				lifecycleFollowUp,
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ResizeObjectInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
			result, err := service.SubmitObjectAction(ctx, input.Path, core.ObjectActionResize, core.ObjectActionOptions{Size: input.Size, ConfigUpdatedAt: input.ConfigUpdatedAt})
			if err != nil {
				return nil, ObjectActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "cap_object",
			Title: "Cap object",
			Description: "Set the resource limits of one OpenSVC svc or vol object, the pg_* keywords, and apply them to its running instances: the daemon queues the orchestration, then writes the keywords the operations name, as a configuration update would, within the same rights and claims; an instance not running applies them at its start. " +
				"An operation is [<section>.]pg_<name>[@<node>]=<value>, the section DEFAULT when not named, a subset#<name> or a resource id, the operator = only; the value default lifts the cap. Without operations, the caps configured are applied. " +
				"A memory limit under what an instance up uses ends its processes. The daemon refuses with 409 a cap while an orchestration runs, and with 403 a cap beyond the claims of the namespace; a cap refused is not written. Returns last_modified, the time of the configuration written. " +
				lifecycleFollowUp,
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CapObjectInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
			result, err := service.SubmitObjectAction(ctx, input.Path, core.ObjectActionCap, core.ObjectActionOptions{Sets: input.Sets, ConfigUpdatedAt: input.ConfigUpdatedAt})
			if err != nil {
				return nil, ObjectActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "delete_object",
			Title: "Delete object",
			Description: "Delete one OpenSVC svc, vol, cfg, sec or usr object: the daemon removes its configuration and the files of every instance. " +
				"The daemon does not look at what the object runs or holds: the MCP refuses to delete an object an instance of which is not down, or, for a svc or a vol, holds provisioned resources, which would stay mounted, or the storage held by nothing. Stop it, then unprovision it, before deleting it. " +
				"An object deleted is gone: a second delete is refused. " +
				lifecycleFollowUp,
			Annotations: actionAnnotations(true, false),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ObjectActionInput) (*mcp.CallToolResult, ObjectActionOutput, error) {
			result, err := service.SubmitObjectAction(ctx, input.Path, core.ObjectActionDelete, core.ObjectActionOptions{})
			if err != nil {
				return nil, ObjectActionOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	for _, declared := range []struct {
		enable      bool
		name, title string
		description string
		destructive bool
	}{
		{
			enable: true, name: "enable_object", title: "Enable object",
			description: "Enable one OpenSVC svc object, or some of its resources by their exact ids: the daemon removes the disable keyword of DEFAULT, or of each resource, at once, without an orchestration. A resource enabled holds its values again: one holding what a resource of another object holds on the same node is refused with 409, naming them. ",
		},
		{
			enable: false, name: "disable_object", title: "Disable object",
			description: "Disable one OpenSVC svc object, or some of its resources by their exact ids: the daemon sets the disable keyword of DEFAULT, or of each resource, at once, without an orchestration. A disabled object is no longer orchestrated, and a disabled resource is no longer started or stopped by the actions of the object. The daemon refuses with 403 to disable a resource up on a node: stop it first. ",
			destructive: true,
		},
	} {
		declared := declared
		if err := addTool(
			registrar,
			&mcp.Tool{
				Name:  declared.name,
				Title: declared.title,
				Description: declared.description +
					"An id that is not a resource of the service is refused before the call, naming the resources of its group. " +
					"Returns the disable keyword of each section touched, read again. Requires admin on the object namespace.",
				Annotations: actionAnnotations(declared.destructive, true),
			},
			func(ctx context.Context, _ *mcp.CallToolRequest, input EnableObjectInput) (*mcp.CallToolResult, ObjectEnableOutput, error) {
				result, err := service.SetObjectEnabled(ctx, input.Path, declared.enable, input.Rids)
				if err != nil {
					return nil, ObjectEnableOutput{}, err
				}
				return nil, result, nil
			},
		); err != nil {
			return err
		}
	}
	return nil
}
