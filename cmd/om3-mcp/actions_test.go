package main

import (
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestActionToolsAreRegisteredOnlyWhenEnabled(t *testing.T) {
	actions := []string{"freeze_object", "unfreeze_object", "abort_object_orchestration", "start_object", "stop_object", "restart_object", "switch_object", "giveback_object", "update_object_config", "create_object",
		"provision_object", "resize_object", "cap_object", "enable_object", "disable_object", "delete_object"}
	for _, enabled := range []bool{false, true} {
		handler, err := newConfiguredToolsHandler(nil, nil, nil, enabled, nil)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		list, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
		session.Close()
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]*mcp.Tool{}
		for _, tool := range list.Tools {
			found[tool.Name] = tool
		}
		for _, name := range actions {
			tool, present := found[name]
			if present != enabled {
				t.Fatalf("actions enabled=%v: %s present=%v", enabled, name, present)
			}
			if present && (tool.Annotations == nil || tool.Annotations.ReadOnlyHint) {
				t.Fatalf("%s must be announced as a state-changing tool: %+v", name, tool.Annotations)
			}
			// Unfreezing resumes automatic actions that may interrupt the service.
			if present && name == "unfreeze_object" && (tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
				t.Fatalf("unfreeze_object must be destructive: %+v", tool.Annotations)
			}
		}
		if _, present := found["get_node_status"]; !present {
			t.Fatalf("actions enabled=%v: read-only tools missing", enabled)
		}
	}
}
