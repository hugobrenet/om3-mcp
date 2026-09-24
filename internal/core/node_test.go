package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGetNodeStatusFiltersClusterView(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"nodes": ["node-b", "node-a", "node-a"]},
			"node": {
				"node-a": {"status": {"agent": "other-node"}},
				"node-b": {
					"config": {"min_avail_mem_pct": 5, "min_avail_swap_pct": 0, "sshkey": "private-key-must-not-leak"},
					"stats": {"load_15m": 0.4, "mem_avail": 82, "mem_total": 4096, "score": 67, "swap_avail": 0, "swap_total": 0},
					"status": {"agent": "v3.0.0", "api": 1, "compat": 2, "is_leader": true,
						"is_overloaded": false, "booted_at": "2026-09-18T09:00:00Z", "frozen_at": "0001-01-01T00:00:00Z"},
					"monitor": {"state": "idle", "global_expect": "none", "local_expect": "none",
						"orchestration_id": "", "orchestration_is_done": true, "updated_at": "2026-09-18T10:00:00Z"},
					"daemon": {"heartbeat": {
						"updated_at": "2026-09-18T09:00:00Z",
						"last_message": {"from":"node-a","patch_length":3,"type":"patch"},
						"last_messages": [{"from":"node-a","patch_length":4,"type":"full"}],
						"secret_version": {"main":5,"alt":4},
						"streams": [
							{"id":"hb#2.tx","type":"unicast","state":"unexpected-state","configured_at":"c2","created_at":"r2","updated_at":"u2",
							 "alerts":[{"severity":"warning","message":"raw alert"}],"peers":{"node-a":{"desc":"to node-a","is_beating":false,"changed_at":"x","last_beating_at":"y"}}},
							{"id":"hb#1.rx","type":"unicast","state":"running","configured_at":"c1","created_at":"r1","updated_at":"0001-01-01T00:00:00Z","peers":{}}
						]
					}}
				}
			}
		}
	}`})
	service.now = func() time.Time { return time.Date(2026, 9, 18, 10, 0, 30, 0, time.UTC) }

	status, err := service.GetNodeStatus(context.Background(), "node-b")
	if err != nil {
		t.Fatalf("get node status: %v", err)
	}
	if status.Node != "node-b" || status.Status.AgentVersion != "v3.0.0" || !status.Status.IsLeader {
		t.Errorf("unexpected selected node status %+v", status)
	}
	if status.Monitor.State != "idle" || status.Monitor.UpdatedAt != "2026-09-18T10:00:00Z" {
		t.Errorf("unexpected node monitor %+v", status.Monitor)
	}
	if status.Stats == nil || status.Stats.MemAvailPct != 82 || status.Stats.SwapTotalMB != 0 {
		t.Errorf("unexpected node stats %+v", status.Stats)
	}
	if status.Policy == nil || status.Policy.MinAvailMemPct != 5 || status.Policy.MinAvailSwapPct != 0 {
		t.Errorf("unexpected node policy %+v", status.Policy)
	}
	if !status.Membership.IsConfigured || status.Membership.ConfiguredPeers.Total != 1 || status.Membership.ConfiguredPeers.Items[0] != "node-a" {
		t.Errorf("unexpected node membership %+v", status.Membership)
	}
	if status.Heartbeat == nil || status.Heartbeat.UpdatedAt != "2026-09-18T09:00:00Z" || status.Heartbeat.Streams.Count != 2 {
		t.Errorf("unexpected node heartbeat %+v", status.Heartbeat)
	} else {
		first := status.Heartbeat.Streams.Items[0]
		second := status.Heartbeat.Streams.Items[1]
		if first.ID != "hb#1.rx" || first.UpdatedAt != "0001-01-01T00:00:00Z" || second.State != "unexpected-state" || second.Peers.Items[0].IsBeating || second.Alerts.Items[0].Message != "raw alert" {
			t.Errorf("heartbeat facts were changed: %+v", status.Heartbeat)
		}
	}
	if status.Provenance.Source != provenanceSourceOpenSVCDaemon || status.Provenance.ObservedAt != "2026-09-18T10:00:30Z" {
		t.Errorf("unexpected provenance %+v", status.Provenance)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal node status: %v", err)
	}
	if strings.Contains(string(encoded), "private-key-must-not-leak") || strings.Contains(string(encoded), "other-node") {
		t.Errorf("result includes unrelated or private node data: %s", encoded)
	}
	for _, forbidden := range []string{`"healthy"`, `"degraded"`, `"heartbeat_status_stale"`, `"heartbeat_peer_not_beating"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("factual result contains deterministic diagnosis %q: %s", forbidden, encoded)
		}
	}
}

func TestGetNodeStatusKeepsMissingHeartbeatNull(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"nodes": ["node-a"]},
			"node": {"node-a": {"status": {"agent": "v3.0.0"}}}
		}
	}`})
	status, err := service.GetNodeStatus(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("get node status: %v", err)
	}
	if status.Heartbeat != nil {
		t.Errorf("heartbeat = %+v, want null", status.Heartbeat)
	}
	if !status.Membership.IsConfigured || status.Membership.ConfiguredPeers.Total != 0 || status.Membership.ConfiguredPeers.Items == nil {
		t.Errorf("membership = %+v", status.Membership)
	}
}

func TestNodeMembershipFactsAreDistinctSortedAndBounded(t *testing.T) {
	nodes := make([]string, 0, maxNodeStatusConfiguredPeers+3)
	nodes = append(nodes, "selected", "peer-999", "peer-999")
	for i := 0; i <= maxNodeStatusConfiguredPeers; i++ {
		nodes = append(nodes, fmt.Sprintf("peer-%03d", i))
	}
	result := nodeMembershipFacts("selected", nodes)
	if !result.IsConfigured || result.ConfiguredPeers.Total != maxNodeStatusConfiguredPeers+2 || result.ConfiguredPeers.Count != maxNodeStatusConfiguredPeers || !result.ConfiguredPeers.Truncated {
		t.Fatalf("membership = %+v", result)
	}
	if result.ConfiguredPeers.Items[0] != "peer-000" || result.ConfiguredPeers.Items[len(result.ConfiguredPeers.Items)-1] != "peer-199" {
		t.Errorf("configured peers are not sorted and bounded: first=%q last=%q", result.ConfiguredPeers.Items[0], result.ConfiguredPeers.Items[len(result.ConfiguredPeers.Items)-1])
	}
}

func TestGetNodeStatusRejectsUnknownAndMissingNodes(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{"cluster":{"config":{"nodes":["node-a"]},"node":{}}}`})
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "", want: "one exact"},
		{name: "node-*", want: "one exact"},
		{name: "node-a ", want: "one exact"},
		{name: "node-a", want: "no published status data"},
		{name: "node-b", want: "not present"},
	} {
		_, err := service.GetNodeStatus(context.Background(), test.name)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("node %q: got error %v, want %q", test.name, err, test.want)
		}
	}
}
