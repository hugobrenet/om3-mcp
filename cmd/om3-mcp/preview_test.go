package main

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

func listedTools(t *testing.T, actions bool) map[string]*mcp.Tool {
	t.Helper()
	handler, err := newConfiguredToolsHandler(nil, nil, nil, actions, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func inputProperties(t *testing.T, tool *mcp.Tool) map[string]any {
	t.Helper()
	data, _ := json.Marshal(tool.InputSchema)
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema.Properties
}

// TestEveryDestructiveToolHasAPreview holds the previews to the tools: each
// tool the user confirms names a preview tool that exists, hidden and
// read-only, and a sentence names only arguments of its tool.
func TestEveryDestructiveToolHasAPreview(t *testing.T) {
	tools := listedTools(t, true)
	previews := 0
	for name, tool := range tools {
		preview, named := tool.Meta["opensvc/preview_tool"].(string)
		destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
		if destructive && !named {
			t.Errorf("destructive tool %s has no preview", name)
			continue
		}
		if !named {
			continue
		}
		previews++
		target, ok := tools[preview]
		if !ok || target.Meta["opensvc/hidden"] != true || !target.Annotations.ReadOnlyHint {
			t.Errorf("%s names the preview %s, which is not a hidden read-only tool", name, preview)
			continue
		}
		if tool.Meta["opensvc/preview_wraps_arguments"] == true {
			properties := inputProperties(t, tool)
			for _, param := range core.ActionSummaryParams()[name] {
				if _, ok := properties[param]; !ok {
					t.Errorf("the sentence of %s names %s, not an argument of the tool", name, param)
				}
			}
			continue
		}
		// A preview of its own takes the arguments of the tool.
		write, _ := json.Marshal(inputProperties(t, tool))
		read, _ := json.Marshal(inputProperties(t, target))
		if string(write) != string(read) {
			t.Errorf("the preview %s does not take the arguments of %s", preview, name)
		}
	}
	if previews == 0 {
		t.Fatal("no tool names a preview")
	}
	// No sentence is left for a tool that is gone.
	for _, name := range core.ActionSummaryTools() {
		if _, ok := tools[name]; !ok {
			t.Errorf("a sentence is kept for %s, which is not a tool", name)
		}
	}
	// The previews read: they are there without the actions.
	reads := listedTools(t, false)
	for _, name := range []string{"preview_object_config_update", "preview_node_config_update", "preview_object_create", "preview_action"} {
		if _, ok := reads[name]; !ok {
			t.Errorf("%s is missing without the actions", name)
		}
	}
	if slices.ContainsFunc([]string{"preview_action"}, func(name string) bool { return reads[name].Meta["opensvc/keywords"] != nil }) {
		t.Error("a hidden tool has search keywords")
	}
}
