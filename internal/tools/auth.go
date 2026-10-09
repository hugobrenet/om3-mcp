package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type GetCallerIdentityInput struct{}

type GetCallerIdentityOutput = core.CallerIdentity

func RegisterAuthTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_caller_identity",
			Title: "Who am I",
			Description: "Read the user name and the grants the OpenSVC daemon applies to the caller: each role, cluster-wide or limited to a namespace. " +
				"Use it to explain a refused call or objects missing from a listing. " +
				"Reports the grants as the daemon resolves them, without deciding what they allow. Needs no particular grant.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ GetCallerIdentityInput) (*mcp.CallToolResult, GetCallerIdentityOutput, error) {
			identity, err := service.GetCallerIdentity(ctx)
			if err != nil {
				return nil, GetCallerIdentityOutput{}, err
			}
			return nil, identity, nil
		},
	)
}
