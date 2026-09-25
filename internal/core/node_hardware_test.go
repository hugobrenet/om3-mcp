package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodeHardwareFiltersSortsAndPaginatesDuplicates(t *testing.T) {
	payload := `{"kind":"HardwareList","items":[
		{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"mem","class":"4096 MB RAM","driver":"","path":"DIMM 0","description":"Memory"}},
		{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Network controller","driver":"virtio-pci","path":"01:00.0","description":"Virtio network"}},
		{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Mass storage controller","driver":"","path":"00:1f.2","description":"SATA controller"}},
		{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Network controller","driver":"virtio-pci","path":"01:00.0","description":"Virtio network"}}
	]}`
	client := &recordingJSONGetter{t: t, path: "/api/node/name/_/system/hardware", query: url.Values{}, payload: payload}
	service := New(client)

	first, err := service.ListNodeHardware(context.Background(), ListNodeHardwareOptions{Types: []string{"pci"}, Classes: []string{"Network controller"}, Limit: 1})
	if err != nil {
		t.Fatalf("list first node hardware page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 2 || first.Count != 1 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	if first.Hardware[0].Path != "01:00.0" || first.Hardware[0].Driver != "virtio-pci" {
		t.Errorf("unexpected first hardware item: %#v", first.Hardware[0])
	}

	second, err := service.ListNodeHardware(context.Background(), ListNodeHardwareOptions{Types: []string{"pci"}, Classes: []string{"Network controller"}, Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second node hardware page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Hardware[0] != first.Hardware[0] {
		t.Errorf("duplicate hardware entry was not preserved: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodeHardwareCanFilterEmptyDriver(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/system/hardware", query: url.Values{},
		payload: `{"kind":"HardwareList","items":[
			{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}},
			{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Network controller","driver":"virtio-pci","path":"01:00.0","description":"Network"}}
		]}`,
	}
	result, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{Node: "node-a", Drivers: []string{""}})
	if err != nil {
		t.Fatalf("filter empty hardware driver: %v", err)
	}
	if result.Total != 1 || result.Hardware[0].Path != "00:00.0" || result.Hardware[0].Driver != "" {
		t.Errorf("unexpected empty-driver result: %#v", result)
	}
}

func TestListNodeHardwareReturnsEmptyNonNilList(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/_/system/hardware", query: url.Values{}, payload: `{"kind":"HardwareList","items":[]}`}
	result, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{})
	if err != nil {
		t.Fatalf("list empty node hardware: %v", err)
	}
	if result.Node != localDaemonNodeAlias || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Hardware == nil || result.Truncated {
		t.Errorf("unexpected empty hardware list: %#v", result)
	}
}

func TestListNodeHardwareBoundsDescription(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/hardware", query: url.Values{},
		payload: `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"pcieport","path":"00:01.0","description":"` + strings.Repeat("d", maxNodeHardwareDescriptionRunes+1) + `"}}]}`,
	}
	result, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{})
	if err != nil {
		t.Fatalf("list bounded node hardware: %v", err)
	}
	if !result.Hardware[0].DescriptionTruncated || len([]rune(result.Hardware[0].Description)) != maxNodeHardwareDescriptionRunes {
		t.Errorf("description was not bounded: %#v", result.Hardware[0])
	}
}

func TestListNodeHardwareRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodeHardwareOptions{
		"node selector":    {Node: "node*"},
		"empty type":       {Types: []string{""}},
		"spaced class":     {Classes: []string{" Bridge"}},
		"too many drivers": {Drivers: make([]string, maxNodeHardwareFilters+1)},
		"invalid limit":    {Limit: maxNodeHardwareLimit + 1},
		"invalid cursor":   {Cursor: "bad\ncursor"},
		"long cursor":      {Cursor: strings.Repeat("x", maxNodeHardwareCursorRunes+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodeHardware(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodeHardwareRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"HardwareList","items":[{"kind":"OtherItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}}]}`,
		"unexpected node":      `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-b"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}}]}`,
		"empty type":           `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}}]}`,
		"empty path":           `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"","description":"Bridge"}}]}`,
		"control description":  `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"bad\ndescription"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/hardware", query: url.Values{}, payload: payload}
			if _, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodeHardwareRejectsInconsistentLocalNodeData(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/hardware", query: url.Values{},
		payload: `{"kind":"HardwareList","items":[
			{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}},
			{"kind":"HardwareItem","meta":{"node":"node-b"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:01.0","description":"Bridge"}}
		]}`,
	}
	if _, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{}); err == nil || !strings.Contains(err.Error(), "inconsistent node") {
		t.Fatalf("got inconsistent local node error %v", err)
	}
}

func TestListNodeHardwareRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/hardware", query: url.Values{},
		payload: `{"kind":"HardwareList","items":[{"kind":"HardwareItem","meta":{"node":"node-a"},"data":{"type":"pci","class":"Bridge","driver":"","path":"00:00.0","description":"Bridge"}}]}`,
	}
	if _, err := New(client).ListNodeHardware(context.Background(), ListNodeHardwareOptions{Cursor: "missing.0"}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
