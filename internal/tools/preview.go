package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type PreviewActionInput struct {
	Tool      string         `json:"tool" jsonschema:"the name of the action tool to preview"`
	Arguments map[string]any `json:"arguments" jsonschema:"the arguments of its call, as given to it"`
}

type PreviewOutput = core.Preview

// previewNote ends the description of every preview tool: they are for the
// client that asks the user to confirm a call, not for the model.
const previewNote = " Read-only: it changes nothing. A client calls it before asking the user to confirm the call; it is hidden from the model."

// RegisterPreviewTools registers the tools that tell what a state-changing
// call would do. They read, and are registered whether actions are enabled
// or not.
func RegisterPreviewTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "preview_object_config_update",
			Title:       "Preview object configuration update",
			Description: "Tell what update_object_config would do with the same arguments: the keys it sets or unsets, their configured value read now and the value the MCP computes they would take, and the sections it deletes with their keys." + previewNote,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateObjectConfigInput) (*mcp.CallToolResult, PreviewOutput, error) {
			result, err := service.PreviewObjectConfigUpdate(ctx, core.UpdateObjectConfigOptions{
				Path: input.Path, Set: input.Set, Unset: input.Unset, Delete: input.Delete,
			})
			if err != nil {
				return nil, PreviewOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "preview_node_config_update",
			Title:       "Preview node configuration update",
			Description: "Tell what update_node_config would do with the same arguments, as preview_object_config_update does for an object." + previewNote,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateNodeConfigInput) (*mcp.CallToolResult, PreviewOutput, error) {
			result, err := service.PreviewNodeConfigUpdate(ctx, core.UpdateNodeConfigOptions{
				Node: input.Node, Set: input.Set, Unset: input.Unset, Delete: input.Delete,
			})
			if err != nil {
				return nil, PreviewOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:        "preview_object_create",
			Title:       "Preview object creation",
			Description: "Tell what create_object would do with the same arguments: the configuration file as given, and the set operations applied after it." + previewNote,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(_ context.Context, _ *mcp.CallToolRequest, input CreateObjectInput) (*mcp.CallToolResult, PreviewOutput, error) {
			result, err := service.PreviewObjectCreate(core.CreateObjectOptions{Path: input.Path, Config: input.Config, Set: input.Set})
			if err != nil {
				return nil, PreviewOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	// The sentence of an action reads nothing: it takes no cluster.
	return registerTool(
		registrar,
		&mcp.Tool{
			Name:        "preview_action",
			Title:       "Preview action",
			Description: "Tell, in one sentence, what an action tool would do with the arguments of its call, such as stop_instance or purge_object." + previewNote,
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(_ context.Context, _ *mcp.CallToolRequest, input PreviewActionInput) (*mcp.CallToolResult, PreviewOutput, error) {
			result, err := service.PreviewAction(core.PreviewActionOptions{Tool: input.Tool, Arguments: input.Arguments})
			if err != nil {
				return nil, PreviewOutput{}, err
			}
			return nil, result, nil
		},
		false,
	)
}
