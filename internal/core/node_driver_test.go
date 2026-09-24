package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodeDriversSortsDeduplicatesAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/node/name/_/drivers",
		query: url.Values{},
		payload: `{"kind":"DriverList","items":[
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.zfs"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"container.docker"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.zfs"}}
		]}`,
	}
	service := New(client)

	first, err := service.ListNodeDrivers(context.Background(), ListNodeDriversOptions{Limit: 2})
	if err != nil {
		t.Fatalf("list first node driver page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 3 || first.Count != 2 || !first.Truncated {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Drivers[0] != "app.simple" || first.Drivers[1] != "container.docker" || first.NextCursor != first.Drivers[1] {
		t.Errorf("drivers are not sorted, deduplicated, or paginated: %#v", first)
	}

	second, err := service.ListNodeDrivers(context.Background(), ListNodeDriversOptions{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second node driver page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Drivers[0] != "fs.zfs" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodeDriversSupportsExplicitNodeAndEmptyList(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/drivers", query: url.Values{},
		payload: `{"kind":"DriverList","items":[]}`,
	}
	result, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a"})
	if err != nil {
		t.Fatalf("list empty node drivers: %v", err)
	}
	if result.Node != "node-a" || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Drivers == nil || result.Truncated {
		t.Errorf("unexpected empty driver list: %#v", result)
	}
}

func TestListNodeDriversRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodeDriversOptions{
		"node selector":  {Node: "node*"},
		"spaced node":    {Node: " node-a"},
		"invalid limit":  {Limit: maxNodeDriverLimit + 1},
		"invalid cursor": {Cursor: "bad\ncursor"},
		"long cursor":    {Cursor: strings.Repeat("x", maxNodeDriverCursorLength+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodeDrivers(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodeDriversRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"DriverList","items":[{"kind":"DriversItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}}]}`,
		"unexpected node":      `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-b"},"data":{"name":"app.simple"}}]}`,
		"empty name":           `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":""}}]}`,
		"control in name":      `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"bad\nname"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodeDriversRejectsInconsistentLocalNodeData(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/drivers", query: url.Values{},
		payload: `{"kind":"DriverList","items":[
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}},
			{"kind":"DriverItem","meta":{"node":"node-b"},"data":{"name":"container.docker"}}
		]}`,
	}
	if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{}); err == nil || !strings.Contains(err.Error(), "inconsistent node") {
		t.Fatalf("got inconsistent local node error %v", err)
	}
}

func TestListNodeDriversRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/drivers", query: url.Values{},
		payload: `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}}]}`,
	}
	if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Cursor: "disk.missing"}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
