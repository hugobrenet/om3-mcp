package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

func readOnlyClosedWorldAnnotations() *mcp.ToolAnnotations {
	destructive := false
	openWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    true,
	}
}

func activeNonDestructiveClosedWorldAnnotations() *mcp.ToolAnnotations {
	destructive := false
	openWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		IdempotentHint:  false,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    false,
	}
}

// actionAnnotations describe a tool that changes the cluster state. Clients
// identify these tools by readOnlyHint false to ask for confirmation.
func actionAnnotations(destructive, idempotent bool) *mcp.ToolAnnotations {
	openWorld := false
	return &mcp.ToolAnnotations{
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &openWorld,
		ReadOnlyHint:    false,
	}
}
