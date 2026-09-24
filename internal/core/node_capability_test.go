package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodeCapabilitiesSortsDeduplicatesAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/node/name/_/capabilities",
		query: url.Values{},
		payload: `{"kind":"CapabilityList","items":[
			{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"node.x.systemd"}},
			{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"drivers.resource.container.docker"}},
			{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"node.x.path.tool=/usr/bin/tool"}},
			{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"node.x.systemd"}}
		]}`,
	}
	service := New(client)

	first, err := service.ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list first node capability page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 3 || first.Count != 2 || !first.Truncated {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Capabilities[0] != "drivers.resource.container.docker" || first.Capabilities[1] != "node.x.path.tool=/usr/bin/tool" || first.NextCursor != first.Capabilities[1] {
		t.Errorf("capabilities are not sorted, deduplicated, or paginated: %#v", first)
	}

	second, err := service.ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{
		Limit: 2, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatalf("list second node capability page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Capabilities[0] != "node.x.systemd" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodeCapabilitiesReturnsEmptyNonNilList(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/capabilities", query: url.Values{},
		payload: `{"kind":"CapabilityList","items":[]}`,
	}
	result, err := New(client).ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{})
	if err != nil {
		t.Fatalf("list empty node capabilities: %v", err)
	}
	if result.Node != localDaemonNodeAlias || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Capabilities == nil || result.Truncated {
		t.Errorf("unexpected empty capability list: %#v", result)
	}
}

func TestListNodeCapabilitiesRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodeCapabilitiesOptions{
		"node selector":  {Node: "node*"},
		"spaced node":    {Node: " node-a"},
		"invalid limit":  {Node: "node-a", Limit: maxNodeCapabilityLimit + 1},
		"invalid cursor": {Node: "node-a", Cursor: "bad\ncursor"},
		"long cursor":    {Node: "node-a", Cursor: strings.Repeat("x", maxNodeCapabilityCursorLength+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodeCapabilities(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodeCapabilitiesRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"CapabilityList","items":[{"kind":"OtherItem","meta":{"node":"node-a"},"data":{"name":"node.x.systemd"}}]}`,
		"unexpected node":      `{"kind":"CapabilityList","items":[{"kind":"CapabilityItem","meta":{"node":"node-b"},"data":{"name":"node.x.systemd"}}]}`,
		"empty name":           `{"kind":"CapabilityList","items":[{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":""}}]}`,
		"control in name":      `{"kind":"CapabilityList","items":[{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"bad\nname"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/node-a/capabilities", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodeCapabilitiesRejectsInconsistentLocalNodeData(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/capabilities", query: url.Values{},
		payload: `{"kind":"CapabilityList","items":[
			{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"node.x.systemd"}},
			{"kind":"CapabilityItem","meta":{"node":"node-b"},"data":{"name":"drivers.resource.container.docker"}}
		]}`,
	}
	if _, err := New(client).ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{}); err == nil || !strings.Contains(err.Error(), "inconsistent node") {
		t.Fatalf("got inconsistent local node error %v", err)
	}
}

func TestListNodeCapabilitiesRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/capabilities", query: url.Values{},
		payload: `{"kind":"CapabilityList","items":[{"kind":"CapabilityItem","meta":{"node":"node-a"},"data":{"name":"node.x.systemd"}}]}`,
	}
	if _, err := New(client).ListNodeCapabilities(context.Background(), ListNodeCapabilitiesOptions{
		Node: "node-a", Cursor: "drivers.missing",
	}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
