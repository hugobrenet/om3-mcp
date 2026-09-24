package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDaemonStatusInput struct{}

type GetDaemonStatusOutput = core.DaemonStatus

type ListDaemonExecutionsInput struct {
	Node            string   `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node"`
	States          []string `json:"states,omitempty" jsonschema:"optional exact daemon execution states; at most 16 values; unknown states are accepted"`
	Origins         []string `json:"origins,omitempty" jsonschema:"optional exact execution origins such as api, imon, nmon, or scheduler; at most 16 values"`
	SessionID       string   `json:"session_id,omitempty" jsonschema:"optional exact canonical UUID shared by related executions"`
	OrchestrationID string   `json:"orchestration_id,omitempty" jsonschema:"optional exact canonical orchestration UUID"`
	ExecID          string   `json:"exec_id,omitempty" jsonschema:"optional exact canonical execution UUID"`
	ObjectPath      string   `json:"object_path,omitempty" jsonschema:"optional exact canonical OpenSVC object path; no wildcard or selector"`
	RID             string   `json:"rid,omitempty" jsonschema:"optional OpenSVC resource selector passed to the daemon"`
	Limit           int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 50"`
	Cursor          string   `json:"cursor,omitempty" jsonschema:"optional opaque next_cursor returned by a previous call with the same filters"`
}

type ListDaemonExecutionsOutput = core.DaemonExecutionList

type ListDaemonOrchestrationsInput struct {
	Node       string   `json:"node,omitempty" jsonschema:"optional exact OpenSVC node name; defaults to the local daemon node"`
	States     []string `json:"states,omitempty" jsonschema:"optional exact orchestration states; at most 16 values; unknown states are accepted"`
	ObjectPath string   `json:"object_path,omitempty" jsonschema:"optional exact canonical OpenSVC object path; the daemon calls this filter selector but matches one exact path"`
	Limit      int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 50"`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"optional opaque next_cursor returned by a previous call with the same filters"`
}

type ListDaemonOrchestrationsOutput = core.DaemonOrchestrationList

func RegisterDaemonTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_daemon_status",
			Title: "Get daemon status",
			Description: "Inspect the local OpenSVC daemon process, target identity, compatibility metadata, and exact subsystem states. " +
				"Use this first to confirm the target and inspect daemon services; it returns no health verdict and makes no changes.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ GetDaemonStatusInput) (*mcp.CallToolResult, GetDaemonStatusOutput, error) {
			status, err := service.GetDaemonStatus(ctx)
			if err != nil {
				return nil, GetDaemonStatusOutput{}, err
			}
			return nil, status, nil
		},
	); err != nil {
		return err
	}

	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_daemon_executions",
			Title: "List daemon executions",
			Description: "List bounded, paginated commands running or recently completed on one exact OpenSVC node. " +
				"The tool preserves daemon-reported states, errors, and exit codes without deriving a health verdict. " +
				"It requires root access; command and error fields can contain sensitive operational arguments. This tool makes no changes.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListDaemonExecutionsInput) (*mcp.CallToolResult, ListDaemonExecutionsOutput, error) {
			executions, err := service.ListDaemonExecutions(ctx, core.ListDaemonExecutionsOptions{
				Node:            input.Node,
				States:          input.States,
				Origins:         input.Origins,
				SessionID:       input.SessionID,
				OrchestrationID: input.OrchestrationID,
				ExecID:          input.ExecID,
				ObjectPath:      input.ObjectPath,
				RID:             input.RID,
				Limit:           input.Limit,
				Cursor:          input.Cursor,
			})
			if err != nil {
				return nil, ListDaemonExecutionsOutput{}, err
			}
			return nil, executions, nil
		},
	); err != nil {
		return err
	}

	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_daemon_orchestrations",
			Title: "List daemon orchestrations",
			Description: "List bounded, paginated orchestrations running or recently completed on one OpenSVC daemon. " +
				"Use orchestration_id to correlate a requested target state and its exact outcome with list_daemon_executions. " +
				"The tool preserves unknown states and errors without deriving a health verdict. It requires root access and makes no changes.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListDaemonOrchestrationsInput) (*mcp.CallToolResult, ListDaemonOrchestrationsOutput, error) {
			orchestrations, err := service.ListDaemonOrchestrations(ctx, core.ListDaemonOrchestrationsOptions{
				Node:       input.Node,
				States:     input.States,
				ObjectPath: input.ObjectPath,
				Limit:      input.Limit,
				Cursor:     input.Cursor,
			})
			if err != nil {
				return nil, ListDaemonOrchestrationsOutput{}, err
			}
			return nil, orchestrations, nil
		},
	); err != nil {
		return err
	}
	return nil
}
