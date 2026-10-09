package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestGetNodeLogsFiltersAndBoundsRecentEntries(t *testing.T) {
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query: url.Values{"follow": {"false"}, "lines": {"3"}, "filter": {"PKG=daemon/hbctrl"}},
		events: [][]byte{
			nodeLogEvent(t, "old", "info", "6", "daemon/hbctrl"),
			nodeLogEvent(t, "peer lost", "warn", "4", "daemon/hbctrl"),
			nodeLogEvent(t, "peer restored", "info", "6", "daemon/hbctrl"),
		},
	}
	result, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{
		Node: "node-a", Lines: 2, Component: "daemon/hbctrl",
	})
	if err != nil {
		t.Fatalf("get node logs: %v", err)
	}
	if client.calls != 1 || result.Node != "node-a" || result.Component != "daemon/hbctrl" || result.Lines != 2 || result.Count != 2 || !result.Truncated {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Entries[0].Message != "peer lost" || result.Entries[0].Level != "warn" || result.Entries[0].Priority != "4" || result.Entries[0].SystemdUnit != "opensvc-server.service" || result.Entries[1].Message != "peer restored" {
		t.Errorf("unexpected entries: %+v", result.Entries)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), "_MACHINE_ID") || strings.Contains(string(encoded), "hidden-machine-id") {
		t.Errorf("raw journald metadata leaked: %s", encoded)
	}
}

func TestGetNodeLogsPreservesMissingLevelAndUsesJournalTimestamp(t *testing.T) {
	event, err := json.Marshal(daemonNodeLogEnvelope{
		daemonInstanceLogEnvelope: daemonInstanceLogEnvelope{
			Timestamp: "1789732800000000", Message: " peer state changed ", Node: "node-a",
		},
		Priority: "5", SystemdUnit: "opensvc-server.service",
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query: url.Values{"follow": {"false"}, "lines": {"51"}}, events: [][]byte{event},
	}
	result, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a"})
	if err != nil {
		t.Fatalf("get node logs: %v", err)
	}
	entry := result.Entries[0]
	if entry.Level != "" || entry.Priority != "5" || entry.Timestamp != "2026-09-18T12:00:00Z" || entry.Message != "peer state changed" {
		t.Errorf("unexpected fallback entry: %+v", entry)
	}
}

func TestGetNodeLogsRejectsInvalidInputAndUnexpectedEvents(t *testing.T) {
	for _, options := range []GetNodeLogsOptions{
		{}, {Node: "_"}, {Node: "node/a"}, {Node: "node-a", Lines: -1},
		{Node: "node-a", Lines: 101}, {Node: "node-a", Component: "daemon/hbctrl +"},
	} {
		client := &instanceLogsClient{t: t}
		if _, err := New(client).GetNodeLogs(context.Background(), options); err == nil || client.calls != 0 {
			t.Errorf("invalid options %+v: error=%v calls=%d", options, err, client.calls)
		}
	}
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query:      url.Values{"follow": {"false"}, "lines": {"51"}},
		eventTypes: []string{"unexpected"}, events: [][]byte{nodeLogEvent(t, "message", "info", "6", "daemon/hbctrl")},
	}
	if _, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a"}); err == nil {
		t.Fatal("unexpected SSE event was accepted")
	}
	client.eventTypes = nil
	client.events = [][]byte{[]byte(`{"JSON":"not-json"}`)}
	if _, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a"}); err == nil {
		t.Fatal("malformed nested payload was accepted")
	}
	client.events = [][]byte{nodeLogEvent(t, "message", "info", "6", "daemon/imon")}
	client.query.Set("filter", "PKG=daemon/hbctrl")
	if _, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a", Component: "daemon/hbctrl"}); err == nil {
		t.Fatal("unexpected component was accepted")
	}
}

func nodeLogEvent(t *testing.T, message string, level string, priority string, component string) []byte {
	t.Helper()
	nested, err := json.Marshal(daemonInstanceLogPayload{
		Timestamp: "2026-09-18T12:00:00Z", Level: level, Message: message,
		Node: "node-a", Component: component,
	})
	if err != nil {
		t.Fatalf("marshal nested event: %v", err)
	}
	event, err := json.Marshal(map[string]string{
		"JSON": string(nested), "PRIORITY": priority,
		"_SYSTEMD_UNIT": "opensvc-server.service", "_MACHINE_ID": "hidden-machine-id",
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return event
}

// Shaped after a dev5 entry of a daemon execution: the ids are journald fields
// and fields of the nested OpenSVC payload.
const nodeLogExecEvent = `{"__REALTIME_TIMESTAMP":"1789732800000000","MESSAGE":"instance: lab/svc/web: removed /etc/opensvc/namespaces/lab/svc/web.conf","NODE":"node-a","OBJ_PATH":"lab/svc/web","PRIORITY":"6","EXEC_ID":"2fc52a5a-741b-414a-9325-a382bab43282","SESSION_ID":"ee34b12e-9f4b-4b39-9fd9-bd01bca40a99","ORCHESTRATION_ID":"30000000-0000-0000-0000-000000000003","JSON":"{\"level\":\"info\",\"node\":\"node-a\",\"session_id\":\"ee34b12e-9f4b-4b39-9fd9-bd01bca40a99\",\"exec_id\":\"2fc52a5a-741b-414a-9325-a382bab43282\",\"obj_path\":\"lab/svc/web\",\"time\":\"2026-10-09T12:35:01+02:00\",\"message\":\"instance: lab/svc/web: removed /etc/opensvc/namespaces/lab/svc/web.conf\"}"}`

func TestGetNodeLogsFiltersByDaemonIDs(t *testing.T) {
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query: url.Values{"follow": {"false"}, "lines": {"11"}, "filter": {
			"EXEC_ID=2fc52a5a-741b-414a-9325-a382bab43282",
			"SESSION_ID=ee34b12e-9f4b-4b39-9fd9-bd01bca40a99",
			"ORCHESTRATION_ID=30000000-0000-0000-0000-000000000003",
		}},
		events: [][]byte{[]byte(nodeLogExecEvent)},
	}
	result, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{
		Node: "node-a", Lines: 10,
		ExecID:          "2FC52A5A-741B-414A-9325-A382BAB43282",
		SessionID:       "ee34b12e-9f4b-4b39-9fd9-bd01bca40a99",
		OrchestrationID: "30000000-0000-0000-0000-000000000003",
	})
	if err != nil {
		t.Fatalf("get node logs: %v", err)
	}
	if result.IDs.ExecID != "2fc52a5a-741b-414a-9325-a382bab43282" || result.Count != 1 || result.Truncated {
		t.Fatalf("unexpected result: %+v", result)
	}
	entry := result.Entries[0]
	if entry.ExecID != "2fc52a5a-741b-414a-9325-a382bab43282" || entry.SessionID != "ee34b12e-9f4b-4b39-9fd9-bd01bca40a99" ||
		entry.OrchestrationID != "30000000-0000-0000-0000-000000000003" || entry.ObjectPath != "lab/svc/web" {
		t.Fatalf("daemon ids not reported: %+v", entry)
	}
}

func TestGetNodeLogsCombinesTheComponentWithAnID(t *testing.T) {
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query: url.Values{"follow": {"false"}, "lines": {"51"}, "filter": {"PKG=daemon/imon", "EXEC_ID=2fc52a5a-741b-414a-9325-a382bab43282"}},
	}
	result, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{
		Node: "node-a", Component: "daemon/imon", ExecID: "2fc52a5a-741b-414a-9325-a382bab43282",
	})
	if err != nil || result.Count != 0 || result.Entries == nil {
		t.Fatalf("got %+v, %v", result, err)
	}
}

func TestGetNodeLogsReadsTheLegacySessionField(t *testing.T) {
	event := `{"__REALTIME_TIMESTAMP":"1789732800000000","MESSAGE":"done","NODE":"node-a","SID":"ee34b12e-9f4b-4b39-9fd9-bd01bca40a99"}`
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query: url.Values{"follow": {"false"}, "lines": {"51"}}, events: [][]byte{[]byte(event)},
	}
	result, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a"})
	if err != nil || result.Entries[0].SessionID != "ee34b12e-9f4b-4b39-9fd9-bd01bca40a99" {
		t.Fatalf("got %+v, %v", result, err)
	}
}

func TestGetNodeLogsRejectsInvalidIDsAndForeignEntries(t *testing.T) {
	for _, options := range []GetNodeLogsOptions{
		{Node: "node-a", ExecID: "not-a-uuid"},
		{Node: "node-a", SessionID: "ee34b12e-9f4b-4b39-9fd9-bd01bca40a99,x"},
		{Node: "node-a", OrchestrationID: " 30000000-0000-0000-0000-000000000003"},
	} {
		client := &instanceLogsClient{t: t}
		if _, err := New(client).GetNodeLogs(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
	client := &instanceLogsClient{
		t: t, path: "/api/node/name/node-a/log",
		query:  url.Values{"follow": {"false"}, "lines": {"51"}, "filter": {"EXEC_ID=20000000-0000-0000-0000-000000000001"}},
		events: [][]byte{[]byte(nodeLogExecEvent)},
	}
	if _, err := New(client).GetNodeLogs(context.Background(), GetNodeLogsOptions{Node: "node-a", ExecID: "20000000-0000-0000-0000-000000000001"}); err == nil || !strings.Contains(err.Error(), "unexpected execution id") {
		t.Fatalf("an entry of another execution was accepted: %v", err)
	}
}
