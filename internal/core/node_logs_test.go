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
		{}, {Node: "node/a"}, {Node: "node-a", Lines: -1},
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
