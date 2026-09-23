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

type NodeStatus struct {
	Provenance Provenance                 `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       string                     `json:"node" jsonschema:"the exact OpenSVC node name selected from the cluster status"`
	Status     NodeReportedStatus         `json:"status" jsonschema:"the node status last published by OpenSVC"`
	Monitor    NodeMonitorStatus          `json:"monitor" jsonschema:"the node monitor state last published by OpenSVC"`
	Stats      *NodeCapacityStats         `json:"stats" jsonschema:"published node capacity statistics, or null when unavailable"`
	Policy     *NodeCapacityPolicy        `json:"policy" jsonschema:"configured memory and swap availability thresholds, or null when unavailable"`
	Heartbeat  ClusterNodeHeartbeatHealth `json:"heartbeat" jsonschema:"bounded assessment of the node's published heartbeat streams and peers"`
}

type NodeReportedStatus struct {
	AgentVersion string `json:"agent_version" jsonschema:"the OpenSVC agent version reported by this node"`
	APIVersion   int    `json:"api_version" jsonschema:"the node daemon API compatibility version"`
	Compat       int    `json:"compat_version" jsonschema:"the node daemon compatibility version"`
	IsLeader     bool   `json:"is_leader" jsonschema:"whether the node reports itself as cluster leader"`
	IsOverloaded bool   `json:"is_overloaded" jsonschema:"whether the node reports overload"`
	BootedAt     string `json:"booted_at" jsonschema:"the node boot timestamp reported by OpenSVC"`
	FrozenAt     string `json:"frozen_at" jsonschema:"the node freeze timestamp reported by OpenSVC"`
}

type NodeMonitorStatus struct {
	State               string `json:"state" jsonschema:"the current node monitor state reported by OpenSVC"`
	GlobalExpect        string `json:"global_expect" jsonschema:"the cluster-wide target state for this node"`
	LocalExpect         string `json:"local_expect" jsonschema:"the local target state for this node"`
	OrchestrationID     string `json:"orchestration_id" jsonschema:"the current node orchestration identifier, if any"`
	OrchestrationIsDone bool   `json:"orchestration_is_done" jsonschema:"whether the node orchestration reports completion"`
	UpdatedAt           string `json:"updated_at" jsonschema:"the node monitor update timestamp reported by OpenSVC"`
}

type NodeCapacityStats struct {
	Load15M      float64 `json:"load_15m" jsonschema:"published fifteen-minute system load"`
	MemAvailPct  int     `json:"mem_available_pct" jsonschema:"available physical memory as a percentage"`
	MemTotalMB   uint64  `json:"mem_total_mb" jsonschema:"total physical memory in megabytes"`
	Score        int     `json:"score" jsonschema:"OpenSVC node capacity score"`
	SwapAvailPct int     `json:"swap_available_pct" jsonschema:"available swap as a percentage"`
	SwapTotalMB  uint64  `json:"swap_total_mb" jsonschema:"total swap capacity in megabytes"`
}

type NodeCapacityPolicy struct {
	MinAvailMemPct  int `json:"min_avail_mem_pct" jsonschema:"configured minimum available physical memory percentage; zero disables this check"`
	MinAvailSwapPct int `json:"min_avail_swap_pct" jsonschema:"configured minimum available swap percentage; zero disables this check"`
}

type NodeConfig struct {
	Provenance         Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node               string     `json:"node" jsonschema:"the exact OpenSVC node whose configuration file was requested"`
	Content            string     `json:"content" jsonschema:"bounded OpenSVC node configuration file content returned by the daemon"`
	SizeBytes          int        `json:"size_bytes" jsonschema:"complete redacted configuration file size in bytes before MCP output truncation"`
	ReturnedBytes      int        `json:"returned_bytes" jsonschema:"number of configuration content bytes included in this result"`
	Truncated          bool       `json:"truncated" jsonschema:"whether configuration content was omitted after the 65536-byte output limit"`
	RedactionRequested bool       `json:"redaction_requested" jsonschema:"whether the MCP required daemon-side secret redaction for this request; always true"`
}

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

func (s *Service) GetNodeStatus(ctx context.Context, nodeName string) (NodeStatus, error) {
	if nodeName == "" || len(nodeName) > 255 || nodeName != strings.TrimSpace(nodeName) || strings.ContainsAny(nodeName, "*?[]/\\") {
		return NodeStatus{}, fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}

	clusterStatus, err := s.getClusterStatus(ctx)
	if err != nil {
		return NodeStatus{}, fmt.Errorf("get node status: %w", err)
	}
	node, reported := clusterStatus.Cluster.Node[nodeName]
	if !reported {
		if slices.Contains(clusterStatus.Cluster.Config.Nodes, nodeName) {
			return NodeStatus{}, fmt.Errorf("configured node %q has no published status data", nodeName)
		}
		return NodeStatus{}, fmt.Errorf("node %q is not present in the cluster status", nodeName)
	}

	result := NodeStatus{
		Node: nodeName,
		Status: NodeReportedStatus{
			AgentVersion: node.Status.Agent,
			APIVersion:   node.Status.API,
			Compat:       node.Status.Compat,
			IsLeader:     node.Status.IsLeader,
			IsOverloaded: node.Status.IsOverloaded,
			BootedAt:     node.Status.BootedAt,
			FrozenAt:     node.Status.FrozenAt,
		},
		Monitor: NodeMonitorStatus{
			State:               node.Monitor.State,
			GlobalExpect:        node.Monitor.GlobalExpect,
			LocalExpect:         node.Monitor.LocalExpect,
			OrchestrationID:     node.Monitor.OrchestrationID,
			OrchestrationIsDone: node.Monitor.OrchestrationIsDone,
			UpdatedAt:           node.Monitor.UpdatedAt,
		},
		Heartbeat: clusterHeartbeatHealth(node.Daemon.Heartbeat, nodeName, clusterStatus.Cluster.Config.Nodes, s.now()),
	}
	if node.Stats != nil {
		result.Stats = &NodeCapacityStats{
			Load15M:      node.Stats.Load15M,
			MemAvailPct:  node.Stats.MemAvailPct,
			MemTotalMB:   node.Stats.MemTotalMB,
			Score:        node.Stats.Score,
			SwapAvailPct: node.Stats.SwapAvailPct,
			SwapTotalMB:  node.Stats.SwapTotalMB,
		}
	}
	if node.Config != nil {
		result.Policy = &NodeCapacityPolicy{
			MinAvailMemPct:  node.Config.MinAvailMemPct,
			MinAvailSwapPct: node.Config.MinAvailSwapPct,
		}
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

func (s *Service) GetNodeConfig(ctx context.Context, node string) (NodeConfig, error) {
	if node == "" || len(node) > 255 || node != strings.TrimSpace(node) || strings.IndexFunc(node, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.-", r))
	}) >= 0 {
		return NodeConfig{}, fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}
	payload, err := s.getRedactedConfigFile(ctx, fmt.Sprintf("/api/node/name/%s/config/file", node))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("get node config: %w", err)
	}
	content, truncated, err := boundConfigFile(payload)
	if err != nil {
		return NodeConfig{}, fmt.Errorf("get node config: %w", err)
	}
	return NodeConfig{
		Provenance: s.newProvenance(), Node: node, Content: content, SizeBytes: len(payload),
		ReturnedBytes: len(content), Truncated: truncated, RedactionRequested: true,
	}, nil
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
