package core

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestClusterHeartbeatHealth(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-time.Minute).Format(time.RFC3339Nano)
	baseStreams := []clusterHeartbeatStream{
		{ID: "hb#1.rx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: true}}},
		{ID: "hb#1.tx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: true}}},
	}
	tests := []struct {
		name      string
		heartbeat *clusterHeartbeat
		nodes     []string
		state     string
		issueCode string
	}{
		{name: "healthy", heartbeat: &clusterHeartbeat{UpdatedAt: fresh, Streams: baseStreams}, nodes: []string{"node-a", "node-b"}, state: "healthy"},
		{name: "stopped stream", heartbeat: &clusterHeartbeat{UpdatedAt: fresh, Streams: []clusterHeartbeatStream{
			{ID: "hb#1.rx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: true}}},
			{ID: "hb#1.tx", State: "stopped"},
		}}, nodes: []string{"node-a", "node-b"}, state: "degraded", issueCode: "heartbeat_stream_not_running"},
		{name: "redundant RX link lost", heartbeat: &clusterHeartbeat{UpdatedAt: fresh, Streams: []clusterHeartbeatStream{
			{ID: "hb#1.rx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: true}}},
			{ID: "hb#2.rx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: false, ChangedAt: fresh, LastBeatingAt: fresh}}},
		}}, nodes: []string{"node-a", "node-b"}, state: "degraded", issueCode: "heartbeat_peer_not_beating"},
		{name: "configured RX peer absent", heartbeat: &clusterHeartbeat{UpdatedAt: fresh, Streams: []clusterHeartbeatStream{
			{ID: "hb#1.tx", State: "running", Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: true}}},
		}}, nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_rx_peer_missing"},
		{name: "empty multi-node status", heartbeat: &clusterHeartbeat{UpdatedAt: fresh}, nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_rx_peer_missing"},
		{name: "missing status", nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_status_missing"},
		{name: "zero timestamp", heartbeat: &clusterHeartbeat{UpdatedAt: "0001-01-01T00:00:00Z", Streams: baseStreams}, nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_timestamp_missing"},
		{name: "stale status", heartbeat: &clusterHeartbeat{UpdatedAt: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), Streams: baseStreams}, nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_status_stale"},
		{name: "future timestamp", heartbeat: &clusterHeartbeat{UpdatedAt: now.Add(time.Minute).Format(time.RFC3339Nano), Streams: baseStreams}, nodes: []string{"node-a", "node-b"}, state: "unknown", issueCode: "heartbeat_timestamp_in_future"},
		{name: "single node", nodes: []string{"node-a"}, state: "not_applicable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clusterHeartbeatHealth(tt.heartbeat, "node-a", tt.nodes, now)
			if got.State != tt.state {
				t.Fatalf("state = %q, want %q: %+v", got.State, tt.state, got)
			}
			if tt.issueCode != "" && !hasHeartbeatIssue(got.Issues, tt.issueCode) {
				t.Errorf("issues = %+v, want %s", got.Issues, tt.issueCode)
			}
			if tt.issueCode == "" && got.IssuesTotal != 0 {
				t.Errorf("issues = %+v, want none", got.Issues)
			}
		})
	}
}

func TestClusterHeartbeatIssuesAreBounded(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	streams := make([]clusterHeartbeatStream, 60)
	for i := range streams {
		streams[i] = clusterHeartbeatStream{
			ID:    fmt.Sprintf("hb#%02d.rx", i),
			State: "running",
			Peers: map[string]clusterHeartbeatPeer{"node-b": {IsBeating: false}},
		}
	}
	got := clusterHeartbeatHealth(&clusterHeartbeat{UpdatedAt: now.Format(time.RFC3339Nano), Streams: streams}, "node-a", []string{"node-a", "node-b"}, now)
	if got.State != "degraded" || got.IssuesTotal != 61 || len(got.Issues) != maxClusterHeartbeatIssues || !got.IssuesTruncated || got.RXPeersStale != 1 {
		t.Fatalf("bounded issues = %+v", got)
	}
	if got.Issues[0].StreamID != "hb#00.rx" || got.Issues[len(got.Issues)-1].StreamID != "hb#49.rx" {
		t.Errorf("issues are not sorted by stream: first=%+v last=%+v", got.Issues[0], got.Issues[len(got.Issues)-1])
	}
}

func TestGetClusterHealthHeartbeatDegraded(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"id": "cluster-123", "nodes": ["node-a", "node-b"]},
			"status": {"is_compat": true},
			"node": {
				"node-a": {
					"status": {"agent": "v3", "is_leader": true}, "monitor": {"state": "idle"},
					"daemon": {"heartbeat": {"updated_at": "2026-09-17T11:59:00Z", "streams": [
						{"id": "hb#1.rx", "state": "running", "peers": {"node-b": {"is_beating": false, "changed_at": "2026-09-17T11:58:00Z", "last_beating_at": "2026-09-17T11:57:00Z"}}}
					]}}
				},
				"node-b": {
					"status": {"agent": "v3"}, "monitor": {"state": "idle"},
					"daemon": {"heartbeat": {"updated_at": "2026-09-17T11:59:00Z", "streams": [
						{"id": "hb#1.rx", "state": "running", "peers": {"node-a": {"is_beating": true}}}
					]}}
				}
			}
		}
	}`})
	service.now = func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) }

	health, err := service.GetClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("get cluster health: %v", err)
	}
	if health.Healthy || health.NodeSummary.Healthy != 1 {
		t.Fatalf("cluster health = %+v, want one healthy node and unhealthy cluster", health)
	}
	node := health.Nodes[0]
	if node.Healthy || findNodeIssue(node.Issues, "heartbeat_degraded") == nil {
		t.Fatalf("node issues = %+v, want heartbeat_degraded", node.Issues)
	}
	if node.Heartbeat.State != "degraded" || len(node.Heartbeat.Issues) != 2 || node.Heartbeat.RXPeersStale != 1 || health.NodeSummary.HeartbeatDegraded != 1 {
		t.Fatalf("heartbeat = %+v, want one degraded link and one stale peer", node.Heartbeat)
	}
	issue := node.Heartbeat.Issues[0]
	if issue.Code != "heartbeat_peer_not_beating" || issue.StreamID != "hb#1.rx" || issue.Peer != "node-b" || issue.LastBeatingAt != "2026-09-17T11:57:00Z" {
		t.Errorf("heartbeat issue = %+v, want the stale RX link", issue)
	}
	if !hasHeartbeatIssue(node.Heartbeat.Issues, "heartbeat_peer_stale") {
		t.Errorf("heartbeat issues = %+v, want aggregate peer_stale", node.Heartbeat.Issues)
	}
}

func TestGetClusterHealthHeartbeatUnknownPreventsHealthy(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"id": "cluster-123", "nodes": ["node-a", "node-b"]},
			"status": {"is_compat": true},
			"node": {
				"node-a": {"status": {"agent": "v3", "is_leader": true}, "monitor": {"state": "idle"}},
				"node-b": {"status": {"agent": "v3"}, "monitor": {"state": "idle"},
					"daemon": {"heartbeat": {"updated_at": "2026-09-17T11:59:00Z", "streams": [
						{"id": "hb#1.rx", "state": "running", "peers": {"node-a": {"is_beating": true}}}
					]}}
				}
			}
		}
	}`})
	service.now = func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) }
	health, err := service.GetClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("get cluster health: %v", err)
	}
	if health.Healthy || health.NodeSummary.HeartbeatUnknown != 1 || health.NodeSummary.HeartbeatDegraded != 0 {
		t.Fatalf("cluster health = %+v, want one unknown heartbeat and unhealthy cluster", health)
	}
	if health.Nodes[0].Heartbeat.State != "unknown" || findNodeIssue(health.Nodes[0].Issues, "heartbeat_status_unknown") == nil {
		t.Errorf("node-a health = %+v, want unknown heartbeat issue", health.Nodes[0])
	}
}

func hasHeartbeatIssue(issues []ClusterHeartbeatIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
