package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type ListDaemonExecutionsInput struct {
	Node            string   `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard, selector or underscore alias"`
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
	Node       string   `json:"node" jsonschema:"required exact OpenSVC node name; no wildcard, selector or underscore alias"`
	States     []string `json:"states,omitempty" jsonschema:"optional exact orchestration states; at most 16 values; unknown states are accepted"`
	ObjectPath string   `json:"object_path,omitempty" jsonschema:"optional exact canonical OpenSVC object path; the daemon calls this filter selector but matches one exact path"`
	Limit      int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 50"`
	Cursor     string   `json:"cursor,omitempty" jsonschema:"optional opaque next_cursor returned by a previous call with the same filters"`
}

type ListDNSRecordsInput struct {
	Node    string `json:"node" jsonschema:"required exact OpenSVC node name whose daemon zone is read; no wildcard, selector or underscore alias"`
	Name    string `json:"name,omitempty" jsonschema:"optional exact record name, with or without its final dot, such as web.prod.svc.mycluster"`
	Type    string `json:"type,omitempty" jsonschema:"optional record type: A, AAAA, PTR, SRV, SOA or NS"`
	Content string `json:"content,omitempty" jsonschema:"optional exact record content, such as an address to find the names pointing to it"`
	Object  string `json:"object,omitempty" jsonschema:"optional exact OpenSVC object path; keeps the object, node, resource, service and reverse records naming it"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 100"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same node and filters"`
}

type ListDNSRecordsOutput = core.DNSRecordList

type ListDaemonOrchestrationsOutput = core.DaemonOrchestrationList

func RegisterDaemonTools(registrar *Registrar, service *core.Service) error {
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
			Description: "List bounded, paginated orchestrations running or recently completed on one exact OpenSVC node. " +
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
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_dns_records",
			Title: "List DNS records",
			Description: "List the records of the cluster DNS zone one OpenSVC daemon serves, built from the instance status of the cluster: object names, node affine names, resource names, service and reverse records. " +
				"Filter by exact name, type, content such as an address, or object path, and use the cursor to continue a page. " +
				"An object name resolves only to the addresses serving the object, while node affine names are published whatever the instance state. " +
				"Reports the zone as the daemon builds it, without checking resolution. Requires root access to the daemon endpoint.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListDNSRecordsInput) (*mcp.CallToolResult, ListDNSRecordsOutput, error) {
			records, err := service.ListDNSRecords(ctx, core.ListDNSRecordsOptions{
				Node: input.Node, Name: input.Name, Type: input.Type, Content: input.Content, Object: input.Object,
				Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListDNSRecordsOutput{}, err
			}
			return nil, records, nil
		},
	); err != nil {
		return err
	}
	return nil
}
