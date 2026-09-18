package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGetNodeStatusFiltersClusterView(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"nodes": ["node-a", "node-b"]},
			"node": {
				"node-a": {"status": {"agent": "other-node"}},
				"node-b": {
					"config": {"min_avail_mem_pct": 5, "min_avail_swap_pct": 0, "sshkey": "private-key-must-not-leak"},
					"stats": {"load_15m": 0.4, "mem_avail": 82, "mem_total": 4096, "score": 67, "swap_avail": 0, "swap_total": 0},
					"status": {"agent": "v3.0.0", "api": 1, "compat": 2, "is_leader": true,
						"is_overloaded": false, "booted_at": "2026-09-18T09:00:00Z", "frozen_at": "0001-01-01T00:00:00Z"},
					"monitor": {"state": "idle", "global_expect": "none", "local_expect": "none",
						"orchestration_id": "", "orchestration_is_done": true, "updated_at": "2026-09-18T10:00:00Z"},
					"daemon": {"heartbeat": {"updated_at": "2026-09-18T10:00:00Z", "streams": [
						{"id": "hb#1.rx", "state": "running", "peers": {"node-a": {"is_beating": true}}},
						{"id": "hb#1.tx", "state": "running", "peers": {"node-a": {"is_beating": true}}}
					]}}
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
	if status.Heartbeat.State != "healthy" || status.Heartbeat.LinksBeating != 2 {
		t.Errorf("unexpected node heartbeat %+v", status.Heartbeat)
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
