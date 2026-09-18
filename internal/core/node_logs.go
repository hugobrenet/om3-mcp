package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type GetNodeLogsOptions struct {
	Node      string
	Lines     int
	Component string
}

type NodeLogList struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       string         `json:"node" jsonschema:"the exact OpenSVC node whose journal was queried"`
	Component  string         `json:"component,omitempty" jsonschema:"the exact OpenSVC component filter when requested"`
	Lines      int            `json:"lines" jsonschema:"the requested maximum number of recent log entries"`
	Count      int            `json:"count" jsonschema:"the number of bounded log entries returned"`
	Entries    []NodeLogEntry `json:"entries" jsonschema:"recent OpenSVC node log entries in chronological order"`
	Truncated  bool           `json:"truncated" jsonschema:"whether older entries or message content were omitted by response bounds"`
}

type NodeLogEntry struct {
	Timestamp        string `json:"timestamp" jsonschema:"the OpenSVC log timestamp, or journald timestamp when absent"`
	Level            string `json:"level,omitempty" jsonschema:"the OpenSVC log level when provided"`
	Priority         string `json:"priority,omitempty" jsonschema:"the journald syslog priority when provided; 0 is most severe and 7 is debug"`
	Message          string `json:"message" jsonschema:"the bounded log message without raw journald metadata"`
	MessageTruncated bool   `json:"message_truncated" jsonschema:"whether this log message was shortened by MCP output bounds"`
	Component        string `json:"component,omitempty" jsonschema:"the OpenSVC package or component that emitted the entry"`
	SystemdUnit      string `json:"systemd_unit,omitempty" jsonschema:"the systemd unit recorded by journald when present"`
	ObjectPath       string `json:"object_path,omitempty" jsonschema:"the related OpenSVC object path when present"`
	ResourceID       string `json:"resource_id,omitempty" jsonschema:"the related OpenSVC resource id when present"`
	SessionID        string `json:"session_id,omitempty" jsonschema:"the related OpenSVC session id when present"`
	EventID          string `json:"event_id,omitempty" jsonschema:"the related OpenSVC event id when present"`
	RequestID        string `json:"request_id,omitempty" jsonschema:"the related daemon API request id when present"`
	OrchestrationID  string `json:"orchestration_id,omitempty" jsonschema:"the related OpenSVC orchestration id when present"`
}

type daemonNodeLogEnvelope struct {
	daemonInstanceLogEnvelope
	Priority    string `json:"PRIORITY"`
	SystemdUnit string `json:"_SYSTEMD_UNIT"`
}

func (s *Service) GetNodeLogs(ctx context.Context, options GetNodeLogsOptions) (NodeLogList, error) {
	node := strings.TrimSpace(options.Node)
	if node == "" || len(node) > 255 || node != options.Node || strings.IndexFunc(node, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.-", r))
	}) >= 0 {
		return NodeLogList{}, fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}
	lines := options.Lines
	if lines == 0 {
		lines = defaultGetInstanceLogsLines
	}
	if lines < 1 || lines > maxGetInstanceLogsLines {
		return NodeLogList{}, fmt.Errorf("node log lines must be between 1 and %d", maxGetInstanceLogsLines)
	}
	component := strings.TrimSpace(options.Component)
	if len(component) > maxInstanceLogFieldRunes || strings.IndexFunc(component, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_./:#-", r))
	}) >= 0 {
		return NodeLogList{}, fmt.Errorf("component must be one exact OpenSVC component of at most 255 characters")
	}
	getter, ok := s.client.(SSEGetter)
	if !ok {
		return NodeLogList{}, fmt.Errorf("OpenSVC daemon client does not support SSE requests")
	}

	endpoint := fmt.Sprintf("/api/node/name/%s/log", node)
	query := url.Values{"follow": {"false"}, "lines": {strconv.Itoa(lines + 1)}}
	if component != "" {
		query.Set("filter", "PKG="+component)
	}
	entries := make([]NodeLogEntry, 0, lines+1)
	err := getter.GetSSE(ctx, endpoint, query, func(event string, _ string, data []byte) error {
		if event != "" && event != "log" {
			return fmt.Errorf("unexpected node log SSE event %q", event)
		}
		entry, err := parseNodeLogEntry(data, node, component)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		if len(entries) > lines+1 {
			return fmt.Errorf("node log endpoint returned more than %d requested events", lines+1)
		}
		return nil
	})
	if err != nil {
		return NodeLogList{}, fmt.Errorf("get node logs: %w", err)
	}

	truncated := len(entries) > lines
	if truncated {
		entries = entries[len(entries)-lines:]
	}
	entries, bounded := boundNodeLogEntries(entries)
	truncated = truncated || bounded
	if entries == nil {
		entries = []NodeLogEntry{}
	}
	return NodeLogList{
		Provenance: s.newProvenance(), Node: node, Component: component,
		Lines: lines, Count: len(entries), Entries: entries, Truncated: truncated,
	}, nil
}

func parseNodeLogEntry(data []byte, expectedNode string, expectedComponent string) (NodeLogEntry, error) {
	var envelope daemonNodeLogEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return NodeLogEntry{}, fmt.Errorf("decode node log envelope: %w", err)
	}
	payload := daemonInstanceLogPayload{
		Timestamp: envelope.Timestamp, Level: envelope.Level, Message: envelope.Message,
		Node: envelope.Node, Object: envelope.Object, Component: envelope.Component,
		ResourceID: envelope.ResourceID, SessionID: envelope.SessionID, EventID: envelope.EventID,
		RequestID: envelope.RequestID, OrchestrationID: envelope.OrchestrationID,
	}
	if envelope.JSON != "" {
		var nested daemonInstanceLogPayload
		if err := json.Unmarshal([]byte(envelope.JSON), &nested); err != nil {
			return NodeLogEntry{}, fmt.Errorf("decode nested OpenSVC node log payload: %w", err)
		}
		mergeInstanceLogPayload(&payload, nested)
	}
	if payload.Node != "" && payload.Node != expectedNode {
		return NodeLogEntry{}, fmt.Errorf("node log returned unexpected node %q", payload.Node)
	}
	if expectedComponent != "" && payload.Component != expectedComponent {
		return NodeLogEntry{}, fmt.Errorf("node log returned unexpected component %q", payload.Component)
	}
	message := normalizeInstanceLogText(payload.Message)
	if message == "" {
		return NodeLogEntry{}, fmt.Errorf("node log entry has no message")
	}
	timestamp := payload.Timestamp
	if envelope.JSON == "" || timestamp == envelope.Timestamp {
		if micros, err := strconv.ParseInt(timestamp, 10, 64); err == nil {
			timestamp = time.UnixMicro(micros).UTC().Format(time.RFC3339Nano)
		}
	}
	return NodeLogEntry{
		Timestamp:       boundInstanceLogField(timestamp),
		Level:           strings.ToLower(boundInstanceLogField(payload.Level)),
		Priority:        boundInstanceLogField(envelope.Priority),
		Message:         message,
		Component:       boundInstanceLogField(payload.Component),
		SystemdUnit:     boundInstanceLogField(envelope.SystemdUnit),
		ObjectPath:      boundInstanceLogField(payload.Object),
		ResourceID:      boundInstanceLogField(payload.ResourceID),
		SessionID:       boundInstanceLogField(payload.SessionID),
		EventID:         boundInstanceLogField(payload.EventID),
		RequestID:       boundInstanceLogField(payload.RequestID),
		OrchestrationID: boundInstanceLogField(payload.OrchestrationID),
	}, nil
}

func boundNodeLogEntries(entries []NodeLogEntry) ([]NodeLogEntry, bool) {
	remaining := maxInstanceLogsTotalMessageRunes
	selected := make([]NodeLogEntry, 0, len(entries))
	truncated := false
	for index := len(entries) - 1; index >= 0; index-- {
		if remaining == 0 {
			truncated = true
			break
		}
		entry := entries[index]
		message, shortened, used := boundInstanceLogMessage(entry.Message, remaining)
		entry.Message = message
		entry.MessageTruncated = shortened
		remaining -= used
		truncated = truncated || shortened
		selected = append(selected, entry)
	}
	slices.Reverse(selected)
	return selected, truncated
}
