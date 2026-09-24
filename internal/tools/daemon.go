package tools

import (
	"context"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDaemonStatusInput struct{}

type GetDaemonStatusOutput = core.DaemonStatus

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
	return nil
}
