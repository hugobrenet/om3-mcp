package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSchedulesInput struct {
	Scope  string `json:"scope" jsonschema:"required exact schedule scope: node, object, or instance"`
	Path   string `json:"path,omitempty" jsonschema:"exact canonical OpenSVC object path; required for object and instance scopes and forbidden for node scope"`
	Node   string `json:"node,omitempty" jsonschema:"exact OpenSVC node name; required for node and instance scopes and forbidden for object scope"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional opaque next_cursor returned by a previous call with the same scope, path, and node"`
}

type ListSchedulesOutput = core.ScheduleList

func RegisterScheduleTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:        "list_schedules",
			Title:       "List schedules",
			Description: "List bounded OpenSVC scheduler entries for one explicit node, object, or object instance scope. The MCP preserves nullable run timestamps and raw schedule and requirement expressions without predicting executions or deriving overdue state. Results are deterministically sorted and paginated by the MCP because the daemon API does not guarantee response order. Node scope requires root access; object and instance scopes require guest or higher access on the object namespace.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListSchedulesInput) (*mcp.CallToolResult, ListSchedulesOutput, error) {
			schedules, err := service.ListSchedules(ctx, core.ListSchedulesOptions{
				Scope: input.Scope, Path: input.Path, Node: input.Node, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListSchedulesOutput{}, err
			}
			return nil, schedules, nil
		},
	)
}
