package core

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

const daemonOrchestrationPayload = `{
	"kind": "OrchestrationList",
	"items": [
		{
			"orchestration_id": "30000000-0000-0000-0000-000000000002",
			"node": "node-a",
			"path": "prod/svc/app",
			"expect": "started",
			"state": "succeeded",
			"started_at": "2026-09-24T10:01:00Z",
			"ended_at": "2026-09-24T10:01:30Z"
		},
		{
			"orchestration_id": "30000000-0000-0000-0000-000000000003",
			"node": "",
			"expect": "frozen",
			"state": "future-state",
			"started_at": "2026-09-24T10:02:00.123456789Z"
		},
		{
			"orchestration_id": "30000000-0000-0000-0000-000000000001",
			"node": "node-a",
			"path": "prod/svc/app",
			"expect": "stopped",
			"state": "failed",
			"error": "start failed on node-a",
			"started_at": "2026-09-24T10:01:00Z",
			"ended_at": "2026-09-24T10:01:20Z"
		}
	]
}`

func TestListDaemonOrchestrationsUsesLocalAliasSortsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/daemon/orchestration", query: url.Values{}, payload: daemonOrchestrationPayload,
	}
	service := New(client)

	first, err := service.ListDaemonOrchestrations(context.Background(), ListDaemonOrchestrationsOptions{Limit: 1})
	if err != nil {
		t.Fatalf("list first daemon orchestration page: %v", err)
	}
	if first.Total != 3 || first.Count != 1 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	got := first.Orchestrations[0]
	if got.OrchestrationID != "30000000-0000-0000-0000-000000000003" || got.Node != "" || got.State != "future-state" || got.Expect == nil || *got.Expect != "frozen" {
		t.Errorf("unexpected first orchestration: %#v", got)
	}
	if got.Path != nil || got.Error != nil || got.EndedAt != nil {
		t.Errorf("absent optional orchestration facts must remain null: %#v", got)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil || string(decoded) != got.OrchestrationID {
		t.Fatalf("unexpected opaque cursor %q: decoded=%q err=%v", first.NextCursor, decoded, err)
	}

	second, err := service.ListDaemonOrchestrations(context.Background(), ListDaemonOrchestrationsOptions{Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second daemon orchestration page: %v", err)
	}
	if second.Orchestrations[0].OrchestrationID != "30000000-0000-0000-0000-000000000001" || second.Orchestrations[0].State != "failed" {
		t.Errorf("same-timestamp orchestrations are not ordered by id or failed state was lost: %#v", second.Orchestrations)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListDaemonOrchestrationsForwardsValidatedNativeFilters(t *testing.T) {
	client := &recordingJSONGetter{
		t:    t,
		path: "/api/node/name/node-a/daemon/orchestration",
		query: url.Values{
			"state":    {"failed", "running"},
			"selector": {"prod/svc/app"},
		},
		payload: daemonOrchestrationPayload,
	}
	result, err := New(client).ListDaemonOrchestrations(context.Background(), ListDaemonOrchestrationsOptions{
		Node:       " node-a ",
		States:     []string{" failed ", "running", "failed"},
		ObjectPath: " prod/svc/app ",
	})
	if err != nil {
		t.Fatalf("list filtered daemon orchestrations: %v", err)
	}
	if result.Total != 3 || result.Count != 3 || result.Truncated {
		t.Errorf("unexpected filtered result metadata: %#v", result)
	}
}

func TestListDaemonOrchestrationsBoundsTextFields(t *testing.T) {
	payload := `{"kind":"OrchestrationList","items":[{` +
		`"orchestration_id":"30000000-0000-0000-0000-000000000001",` +
		`"node":"node-a","state":"failed","started_at":"2026-09-24T10:00:00Z",` +
		`"expect":"` + strings.Repeat("t", maxDaemonOrchestrationExpectRunes+1) + `",` +
		`"error":"` + strings.Repeat("e", maxDaemonOrchestrationErrorRunes+1) + `"}]}`
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/daemon/orchestration", query: url.Values{}, payload: payload,
	}
	result, err := New(client).ListDaemonOrchestrations(context.Background(), ListDaemonOrchestrationsOptions{})
	if err != nil {
		t.Fatalf("list daemon orchestrations: %v", err)
	}
	got := result.Orchestrations[0]
	if got.Expect == nil || len([]rune(*got.Expect)) != maxDaemonOrchestrationExpectRunes || !got.ExpectTruncated {
		t.Errorf("expect was not bounded: %#v", got)
	}
	if got.Error == nil || len([]rune(*got.Error)) != maxDaemonOrchestrationErrorRunes || !got.ErrorTruncated {
		t.Errorf("error was not bounded: %#v", got)
	}
}

func TestListDaemonOrchestrationsRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tooManyStates := make([]string, maxDaemonOrchestrationFilterValues+1)
	for index := range tooManyStates {
		tooManyStates[index] = "state"
	}
	tests := map[string]ListDaemonOrchestrationsOptions{
		"node selector":   {Node: "node*"},
		"too many states": {States: tooManyStates},
		"object selector": {ObjectPath: "prod/svc/*"},
		"invalid limit":   {Limit: maxDaemonOrchestrationLimit + 1},
		"invalid cursor":  {Cursor: base64.RawURLEncoding.EncodeToString([]byte("not-a-uuid"))},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListDaemonOrchestrations(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListDaemonOrchestrationsRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected kind": `{"kind":"OtherList","items":[]}`,
		"invalid id": `{"kind":"OrchestrationList","items":[{` +
			`"orchestration_id":"bad","node":"node-a","state":"running",` +
			`"started_at":"2026-09-24T10:00:00Z"}]}`,
		"invalid path": `{"kind":"OrchestrationList","items":[{` +
			`"orchestration_id":"30000000-0000-0000-0000-000000000001",` +
			`"node":"node-a","path":"prod/svc/*","state":"running",` +
			`"started_at":"2026-09-24T10:00:00Z"}]}`,
		"invalid timestamp": `{"kind":"OrchestrationList","items":[{` +
			`"orchestration_id":"30000000-0000-0000-0000-000000000001",` +
			`"node":"node-a","state":"running","started_at":"yesterday"}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/_/daemon/orchestration", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListDaemonOrchestrations(context.Background(), ListDaemonOrchestrationsOptions{}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}
