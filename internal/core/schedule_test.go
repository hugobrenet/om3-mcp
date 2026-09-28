package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

const scheduleTestPayload = `{"kind":"ScheduleList","items":[
	{"kind":"ScheduleItem","meta":{"node":"node-b","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","last_run_at":"2026-09-28T12:00:00Z","max_parallel":1,"next_run_at":"2026-09-28T12:10:00Z","require":"","require_collector":false,"require_provisioned":false,"schedule":"@10m"}},
	{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"info","key":"info_schedule","last_run_at":null,"max_parallel":1,"next_run_at":null,"require":"","require_collector":false,"require_provisioned":false,"schedule":"@60m"}},
	{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"compliance_auto","key":"comp_schedule","last_run_at":null,"max_parallel":1,"next_run_at":null,"require":"","require_collector":true,"require_provisioned":true,"schedule":"~00:00-06:00"}}
]}`

func TestListSchedulesSortsPreservesNullDatesAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/schedule", query: url.Values{}, payload: scheduleTestPayload,
	}
	service := New(client)
	options := ListSchedulesOptions{Scope: ScheduleScopeObject, Path: "lab/svc/app", Limit: 2}

	first, err := service.ListSchedules(context.Background(), options)
	if err != nil {
		t.Fatalf("list first schedule page: %v", err)
	}
	if first.Scope != ScheduleScopeObject || first.Object == nil || first.Object.Path != "lab/svc/app" || first.Node != "" {
		t.Fatalf("unexpected target: %#v", first)
	}
	if first.Total != 3 || first.Count != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Schedules[0].Node != "node-a" || first.Schedules[0].Key != "comp_schedule" || first.Schedules[1].Key != "info_schedule" {
		t.Errorf("schedules are not deterministically sorted: %#v", first.Schedules)
	}
	if first.Schedules[0].LastRunAt != nil || first.Schedules[0].NextRunAt != nil {
		t.Errorf("null timestamps were not preserved: %#v", first.Schedules[0])
	}

	options.Cursor = first.NextCursor
	second, err := service.ListSchedules(context.Background(), options)
	if err != nil {
		t.Fatalf("list second schedule page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Schedules[0].Node != "node-b" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if second.Schedules[0].LastRunAt == nil || *second.Schedules[0].LastRunAt != "2026-09-28T12:00:00Z" {
		t.Errorf("known timestamp was not preserved: %#v", second.Schedules[0])
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListSchedulesRoutesExplicitScopes(t *testing.T) {
	tests := []struct {
		name    string
		options ListSchedulesOptions
		path    string
		payload string
		node    string
	}{
		{
			name: "node", options: ListSchedulesOptions{Scope: ScheduleScopeNode, Node: "node-a"},
			path: "/api/node/name/node-a/schedule", node: "node-a",
			payload: `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node-a","object":""},"data":{"action":"checks","key":"checks_schedule","max_parallel":1,"schedule":"@1h"}}]}`,
		},
		{
			name: "instance", options: ListSchedulesOptions{Scope: ScheduleScopeInstance, Node: "node-a", Path: "lab/svc/app"},
			path: "/api/node/name/node-a/instance/path/lab/svc/app/schedule", node: "node-a",
			payload: `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","max_parallel":1,"schedule":"@10m"}}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t, path: test.path, query: url.Values{}, payload: test.payload}
			result, err := New(client).ListSchedules(context.Background(), test.options)
			if err != nil {
				t.Fatalf("list schedules: %v", err)
			}
			if result.Scope != test.options.Scope || result.Node != test.node || result.Count != 1 {
				t.Errorf("unexpected result: %#v", result)
			}
		})
	}
}

func TestListSchedulesReturnsTypedEmptyArray(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/empty/schedule", query: url.Values{}, payload: `{"kind":"ScheduleList","items":[]}`,
	}
	result, err := New(client).ListSchedules(context.Background(), ListSchedulesOptions{Scope: ScheduleScopeObject, Path: "lab/svc/empty"})
	if err != nil {
		t.Fatalf("list empty schedules: %v", err)
	}
	if result.Schedules == nil || result.Total != 0 || result.Count != 0 || result.Truncated {
		t.Errorf("unexpected empty result: %#v", result)
	}
}

func TestListSchedulesRejectsInvalidInputsBeforeDaemonCall(t *testing.T) {
	tests := map[string]ListSchedulesOptions{
		"missing scope":         {},
		"unknown scope":         {Scope: "cluster"},
		"node without node":     {Scope: ScheduleScopeNode},
		"node with path":        {Scope: ScheduleScopeNode, Node: "node-a", Path: "lab/svc/app"},
		"object without path":   {Scope: ScheduleScopeObject},
		"object with node":      {Scope: ScheduleScopeObject, Path: "lab/svc/app", Node: "node-a"},
		"instance without node": {Scope: ScheduleScopeInstance, Path: "lab/svc/app"},
		"instance bad path":     {Scope: ScheduleScopeInstance, Node: "node-a", Path: "not/a/path/at/all"},
		"node selector":         {Scope: ScheduleScopeNode, Node: "node*"},
		"invalid limit":         {Scope: ScheduleScopeObject, Path: "lab/svc/app", Limit: maxScheduleLimit + 1},
		"invalid cursor":        {Scope: ScheduleScopeObject, Path: "lab/svc/app", Cursor: "not-a-cursor"},
		"long cursor":           {Scope: ScheduleScopeObject, Path: "lab/svc/app", Cursor: strings.Repeat("x", maxScheduleCursorRunes+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListSchedules(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListSchedulesRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"null items":           `{"kind":"ScheduleList","items":null}`,
		"unexpected item kind": `{"kind":"ScheduleList","items":[{"kind":"ResourceItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","schedule":"@10m"}}]}`,
		"unexpected object":    `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/other"},"data":{"action":"status","key":"status_schedule","schedule":"@10m"}}]}`,
		"invalid node":         `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node*","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","schedule":"@10m"}}]}`,
		"invalid timestamp":    `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","last_run_at":"yesterday","schedule":"@10m"}}]}`,
		"negative parallel":    `{"kind":"ScheduleList","items":[{"kind":"ScheduleItem","meta":{"node":"node-a","object":"lab/svc/app"},"data":{"action":"status","key":"status_schedule","max_parallel":-1,"schedule":"@10m"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/object/path/lab/svc/app/schedule", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListSchedules(context.Background(), ListSchedulesOptions{Scope: ScheduleScopeObject, Path: "lab/svc/app"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListSchedulesRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/object/path/lab/svc/app/schedule", query: url.Values{}, payload: scheduleTestPayload,
	}
	result, err := New(client).ListSchedules(context.Background(), ListSchedulesOptions{Scope: ScheduleScopeObject, Path: "lab/svc/app", Limit: 1})
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	client.payload = `{"kind":"ScheduleList","items":[]}`
	_, err = New(client).ListSchedules(context.Background(), ListSchedulesOptions{
		Scope: ScheduleScopeObject, Path: "lab/svc/app", Cursor: result.NextCursor,
	})
	if err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
