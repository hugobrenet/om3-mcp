package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

const resourceInfoTestPayload = `{"kind":"ResourceInfoList","items":[
	{"node":"node-b","object":"lab/svc/app","rid":"app#worker","key":"driver","value":"app.forking"},
	{"node":"node-a","object":"lab/svc/app","rid":"container#redis","key":"driver","value":"container.docker"},
	{"node":"node-a","object":"lab/svc/app","rid":"app#worker","key":"start","value":"/bin/true"}
]}`

func TestListResourceInfoSortsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{}, payload: resourceInfoTestPayload,
	}
	service := New(client)
	options := ListResourceInfoOptions{Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: 2}

	first, err := service.ListResourceInfo(context.Background(), options)
	if err != nil {
		t.Fatalf("list first resource info page: %v", err)
	}
	if first.Scope != ResourceInfoScopeObject || first.Object.Path != "lab/svc/app" || first.Node != "" {
		t.Fatalf("unexpected target: %#v", first)
	}
	if first.ReportedTotal != 3 || first.Total != 3 || first.Count != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Entries[0].Node != "node-a" || first.Entries[0].RID != "app#worker" || first.Entries[0].Key != "start" ||
		first.Entries[1].Node != "node-a" || first.Entries[1].RID != "container#redis" {
		t.Errorf("resource information is not deterministically sorted: %#v", first.Entries)
	}

	options.Cursor = first.NextCursor
	second, err := service.ListResourceInfo(context.Background(), options)
	if err != nil {
		t.Fatalf("list second resource info page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Entries[0].Node != "node-b" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListResourceInfoRoutesInstanceScope(t *testing.T) {
	client := &recordingJSONGetter{
		t:       t,
		path:    "/api/node/name/node-a/instance/path/lab/vol/data/resource/info",
		query:   url.Values{},
		payload: `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/vol/data","rid":"disk#0","key":"size","value":"1073741824"}]}`,
	}
	result, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeInstance, Path: "lab/vol/data", Node: "node-a",
	})
	if err != nil {
		t.Fatalf("list instance resource info: %v", err)
	}
	if result.Scope != ResourceInfoScopeInstance || result.Node != "node-a" || result.Object.Kind != "vol" || result.Count != 1 {
		t.Errorf("unexpected result: %#v", result)
	}
}

func TestListResourceInfoAppliesExactLocalFilters(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{}, payload: resourceInfoTestPayload,
	}
	result, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", RID: "app#worker", Key: "driver",
	})
	if err != nil {
		t.Fatalf("list filtered resource info: %v", err)
	}
	if result.ReportedTotal != 3 || result.Total != 1 || result.Count != 1 || result.Filters.RID != "app#worker" || result.Filters.Key != "driver" {
		t.Fatalf("unexpected filtered result: %#v", result)
	}
	if result.Entries[0].Node != "node-b" || result.Entries[0].Value != "app.forking" {
		t.Errorf("unexpected filtered entry: %#v", result.Entries[0])
	}
}

func TestListResourceInfoReturnsTypedEmptyArray(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/empty/resource/info", query: url.Values{}, payload: `{"kind":"ResourceInfoList","items":[]}`,
	}
	result, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/empty",
	})
	if err != nil {
		t.Fatalf("list empty resource info: %v", err)
	}
	if result.Entries == nil || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Truncated {
		t.Errorf("unexpected empty result: %#v", result)
	}
}

func TestListResourceInfoBoundsValuesAndPage(t *testing.T) {
	items := make([]daemonResourceInfoItem, 40)
	for index := range items {
		items[index] = daemonResourceInfoItem{
			Node: "node-a", Object: "lab/svc/app", RID: "app#worker",
			Key:   fmt.Sprintf("key-%02d", index),
			Value: resourceInfoString(strings.Repeat("x", maxResourceInfoValueRunes+10)),
		}
	}
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{}, payload: marshalResourceInfoResponse(t, items),
	}
	result, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: maxResourceInfoLimit,
	})
	if err != nil {
		t.Fatalf("list bounded resource info: %v", err)
	}
	if result.Count == 0 || result.Count >= len(items) || !result.Truncated || result.NextCursor == "" {
		t.Fatalf("page budget was not applied: %#v", result)
	}
	if result.ValuesTruncated != result.Count {
		t.Errorf("values_truncated = %d, want %d", result.ValuesTruncated, result.Count)
	}
	for _, entry := range result.Entries {
		if !entry.ValueTruncated || len([]rune(entry.Value)) != maxResourceInfoValueRunes || !strings.HasSuffix(entry.Value, "…") {
			t.Errorf("value was not bounded: length=%d entry=%#v", len([]rune(entry.Value)), entry)
		}
	}
}

func TestListResourceInfoPaginatesDuplicateEntries(t *testing.T) {
	duplicate := daemonResourceInfoItem{Node: "node-a", Object: "lab/svc/app", RID: "app#worker", Key: "driver", Value: resourceInfoString("app.forking")}
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{},
		payload: marshalResourceInfoResponse(t, []daemonResourceInfoItem{duplicate, duplicate}),
	}
	first, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: 1,
	})
	if err != nil {
		t.Fatalf("list first duplicate: %v", err)
	}
	second, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: 1, Cursor: first.NextCursor,
	})
	if err != nil {
		t.Fatalf("list second duplicate: %v", err)
	}
	if first.Count != 1 || !first.Truncated || second.Count != 1 || second.Truncated {
		t.Errorf("duplicate pagination failed: first=%#v second=%#v", first, second)
	}
}

func TestListResourceInfoRejectsInvalidInputsBeforeDaemonCall(t *testing.T) {
	tests := map[string]ListResourceInfoOptions{
		"missing scope":          {Path: "lab/svc/app"},
		"unknown scope":          {Scope: "node", Path: "lab/svc/app"},
		"unsupported kind":       {Scope: ResourceInfoScopeObject, Path: "lab/sec/secret"},
		"object with node":       {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Node: "node-a"},
		"instance without node":  {Scope: ResourceInfoScopeInstance, Path: "lab/svc/app"},
		"instance node selector": {Scope: ResourceInfoScopeInstance, Path: "lab/svc/app", Node: "node*"},
		"bad path":               {Scope: ResourceInfoScopeObject, Path: "not/a/path/at/all"},
		"long rid":               {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", RID: strings.Repeat("x", maxResourceInfoFilterRunes+1)},
		"control key":            {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Key: "driver\n"},
		"invalid limit":          {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: maxResourceInfoLimit + 1},
		"invalid cursor":         {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Cursor: "not-a-cursor"},
		"long cursor":            {Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Cursor: strings.Repeat("x", maxResourceInfoCursorRunes+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListResourceInfo(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListResourceInfoRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected kind":   `{"kind":"OtherList","items":[]}`,
		"null items":        `{"kind":"ResourceInfoList","items":null}`,
		"invalid node":      `{"kind":"ResourceInfoList","items":[{"node":"node*","object":"lab/svc/app","rid":"app#worker","key":"driver","value":"app.forking"}]}`,
		"unexpected node":   `{"kind":"ResourceInfoList","items":[{"node":"node-b","object":"lab/svc/app","rid":"app#worker","key":"driver","value":"app.forking"}]}`,
		"invalid object":    `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"bad/path","rid":"app#worker","key":"driver","value":"app.forking"}]}`,
		"unexpected object": `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/other","rid":"app#worker","key":"driver","value":"app.forking"}]}`,
		"empty rid":         `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/app","rid":"","key":"driver","value":"app.forking"}]}`,
		"empty key":         `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/app","rid":"app#worker","key":"","value":"app.forking"}]}`,
		"missing value":     `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/app","rid":"app#worker","key":"driver"}]}`,
		"null value":        `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/app","rid":"app#worker","key":"driver","value":null}]}`,
		"control value":     `{"kind":"ResourceInfoList","items":[{"node":"node-a","object":"lab/svc/app","rid":"app#worker","key":"driver","value":"app\nforking"}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/node-a/instance/path/lab/svc/app/resource/info", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
				Scope: ResourceInfoScopeInstance, Path: "lab/svc/app", Node: "node-a",
			}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListResourceInfoRejectsOversizedRawValue(t *testing.T) {
	item := daemonResourceInfoItem{
		Node: "node-a", Object: "lab/svc/app", RID: "app#worker", Key: "output",
		Value: resourceInfoString(strings.Repeat("x", maxResourceInfoRawValueRunes+1)),
	}
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{}, payload: marshalResourceInfoResponse(t, []daemonResourceInfoItem{item}),
	}
	if _, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app",
	}); err == nil {
		t.Fatal("expected oversized value error")
	}
}

func TestListResourceInfoRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/resource/info", query: url.Values{}, payload: resourceInfoTestPayload,
	}
	result, err := New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Limit: 1,
	})
	if err != nil {
		t.Fatalf("list resource info: %v", err)
	}
	_, err = New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", RID: "app#worker", Cursor: result.NextCursor,
	})
	if err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got changed-filter cursor error %v", err)
	}
	client.payload = `{"kind":"ResourceInfoList","items":[]}`
	_, err = New(client).ListResourceInfo(context.Background(), ListResourceInfoOptions{
		Scope: ResourceInfoScopeObject, Path: "lab/svc/app", Cursor: result.NextCursor,
	})
	if err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}

func marshalResourceInfoResponse(t *testing.T, items []daemonResourceInfoItem) string {
	t.Helper()
	payload, err := json.Marshal(daemonResourceInfoList{Kind: "ResourceInfoList", Items: items})
	if err != nil {
		t.Fatalf("marshal resource info response: %v", err)
	}
	return string(payload)
}

func resourceInfoString(value string) *string {
	return &value
}
