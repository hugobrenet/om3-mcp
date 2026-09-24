package core

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

const daemonExecutionPayload = `{
	"kind": "ExecList",
	"items": [
		{
			"session_id": "10000000-0000-0000-0000-000000000001",
			"exec_id": "20000000-0000-0000-0000-000000000002",
			"node": "node-a",
			"origin": "scheduler",
			"command": "om prod/svc/app status",
			"state": "succeeded",
			"exit_code": 0,
			"started_at": "2026-09-24T10:01:00Z",
			"ended_at": "2026-09-24T10:01:01Z"
		},
		{
			"session_id": "10000000-0000-0000-0000-000000000003",
			"exec_id": "20000000-0000-0000-0000-000000000003",
			"orchestration_id": "30000000-0000-0000-0000-000000000003",
			"node": "node-a",
			"path": "prod/svc/app",
			"origin": "api",
			"rid": "app#main",
			"title": "running status probe",
			"command": "om prod/svc/app status --refresh",
			"state": "future-state",
			"error": "still collecting",
			"started_at": "2026-09-24T10:02:00.123456789Z",
			"pid": 4217
		},
		{
			"session_id": "10000000-0000-0000-0000-000000000001",
			"exec_id": "20000000-0000-0000-0000-000000000001",
			"node": "node-a",
			"origin": "scheduler",
			"command": "om prod/svc/app status",
			"state": "failed",
			"error": "driver timeout",
			"exit_code": 1,
			"started_at": "2026-09-24T10:01:00Z",
			"ended_at": "2026-09-24T10:01:30Z"
		}
	]
}`

func TestListDaemonExecutionsSortsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/daemon/exec", query: url.Values{}, payload: daemonExecutionPayload,
	}
	service := New(client)

	first, err := service.ListDaemonExecutions(context.Background(), ListDaemonExecutionsOptions{Limit: 1})
	if err != nil {
		t.Fatalf("list first daemon execution page: %v", err)
	}
	if first.Total != 3 || first.Count != 1 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	got := first.Executions[0]
	if got.ExecID != "20000000-0000-0000-0000-000000000003" || got.State != "future-state" || got.PID == nil || *got.PID != 4217 {
		t.Errorf("unexpected first execution: %#v", got)
	}
	if got.ExitCode != nil || got.EndedAt != nil || got.Path == nil || *got.Path != "prod/svc/app" || got.OrchestrationID == nil {
		t.Errorf("optional execution facts were not preserved: %#v", got)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first.NextCursor)
	if err != nil || string(decoded) != got.ExecID {
		t.Fatalf("unexpected opaque cursor %q: decoded=%q err=%v", first.NextCursor, decoded, err)
	}

	second, err := service.ListDaemonExecutions(context.Background(), ListDaemonExecutionsOptions{Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second daemon execution page: %v", err)
	}
	if second.Executions[0].ExecID != "20000000-0000-0000-0000-000000000001" {
		t.Errorf("same-timestamp executions are not ordered by id: %#v", second.Executions)
	}
	if second.Executions[0].Path != nil || second.Executions[0].RID != nil || second.Executions[0].Title != nil || second.Executions[0].PID != nil {
		t.Errorf("absent optional facts must remain null: %#v", second.Executions[0])
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListDaemonExecutionsForwardsValidatedNativeFilters(t *testing.T) {
	client := &recordingJSONGetter{
		t:    t,
		path: "/api/node/name/node-a/daemon/exec",
		query: url.Values{
			"state":            {"failed", "running"},
			"origin":           {"api", "scheduler"},
			"session_id":       {"10000000-0000-0000-0000-000000000001"},
			"orchestration_id": {"30000000-0000-0000-0000-000000000003"},
			"exec_id":          {"20000000-0000-0000-0000-000000000003"},
			"selector":         {"prod/svc/app"},
			"rid":              {"app#main"},
		},
		payload: daemonExecutionPayload,
	}
	result, err := New(client).ListDaemonExecutions(context.Background(), ListDaemonExecutionsOptions{
		Node:            " node-a ",
		States:          []string{" failed ", "running", "failed"},
		Origins:         []string{"api", " scheduler "},
		SessionID:       " 10000000-0000-0000-0000-000000000001 ",
		OrchestrationID: "30000000-0000-0000-0000-000000000003",
		ExecID:          "20000000-0000-0000-0000-000000000003",
		ObjectPath:      " prod/svc/app ",
		RID:             " app#main ",
	})
	if err != nil {
		t.Fatalf("list filtered daemon executions: %v", err)
	}
	if result.Total != 3 || result.Count != 3 || result.Truncated {
		t.Errorf("unexpected filtered result metadata: %#v", result)
	}
}

func TestListDaemonExecutionsBoundsTextFields(t *testing.T) {
	payload := `{"kind":"ExecList","items":[{` +
		`"session_id":"10000000-0000-0000-0000-000000000001",` +
		`"exec_id":"20000000-0000-0000-0000-000000000001",` +
		`"node":"node-a","origin":"api","state":"failed",` +
		`"started_at":"2026-09-24T10:00:00Z",` +
		`"title":"` + strings.Repeat("t", maxDaemonExecutionTitleRunes+1) + `",` +
		`"command":"` + strings.Repeat("c", maxDaemonExecutionCommandRunes+1) + `",` +
		`"error":"` + strings.Repeat("e", maxDaemonExecutionErrorRunes+1) + `"}]}`
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/daemon/exec", query: url.Values{}, payload: payload,
	}
	result, err := New(client).ListDaemonExecutions(context.Background(), ListDaemonExecutionsOptions{})
	if err != nil {
		t.Fatalf("list daemon executions: %v", err)
	}
	got := result.Executions[0]
	if got.Title == nil || len([]rune(*got.Title)) != maxDaemonExecutionTitleRunes || !got.TitleTruncated {
		t.Errorf("title was not bounded: %#v", got)
	}
	if len([]rune(got.Command)) != maxDaemonExecutionCommandRunes || !got.CommandTruncated {
		t.Errorf("command was not bounded: %#v", got)
	}
	if got.Error == nil || len([]rune(*got.Error)) != maxDaemonExecutionErrorRunes || !got.ErrorTruncated {
		t.Errorf("error was not bounded: %#v", got)
	}
}

func TestListDaemonExecutionsRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tooManyStates := make([]string, maxDaemonExecutionFilterValues+1)
	for index := range tooManyStates {
		tooManyStates[index] = "state"
	}
	tests := map[string]ListDaemonExecutionsOptions{
		"node selector":   {Node: "node*"},
		"too many states": {States: tooManyStates},
		"invalid UUID":    {ExecID: "not-a-uuid"},
		"object selector": {ObjectPath: "prod/svc/*"},
		"invalid limit":   {Limit: maxDaemonExecutionLimit + 1},
		"invalid cursor":  {Cursor: base64.RawURLEncoding.EncodeToString([]byte("not-a-uuid"))},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListDaemonExecutions(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListDaemonExecutionsRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected kind": `{"kind":"OtherList","items":[]}`,
		"invalid id": `{"kind":"ExecList","items":[{` +
			`"session_id":"bad","exec_id":"20000000-0000-0000-0000-000000000001",` +
			`"node":"node-a","origin":"api","command":"om status","state":"running",` +
			`"started_at":"2026-09-24T10:00:00Z"}]}`,
		"invalid timestamp": `{"kind":"ExecList","items":[{` +
			`"session_id":"10000000-0000-0000-0000-000000000001",` +
			`"exec_id":"20000000-0000-0000-0000-000000000001",` +
			`"node":"node-a","origin":"api","command":"om status","state":"running",` +
			`"started_at":"yesterday"}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/_/daemon/exec", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListDaemonExecutions(context.Background(), ListDaemonExecutionsOptions{}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}
