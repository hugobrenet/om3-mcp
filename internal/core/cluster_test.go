package core

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestGetClusterHealthDegraded(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"id": "cluster-123", "name": "prod", "nodes": ["node-a", "node-b"]},
			"status": {"is_compat": true, "is_frozen": false},
			"node": {
				"node-a": {
					"status": {"agent": "v3.0.0", "is_leader": true, "frozen_at": "0001-01-01T00:00:00Z"},
					"monitor": {"state": "idle"}
				},
				"node-b": {
					"config": {"min_avail_mem_pct": 2, "min_avail_swap_pct": 10},
					"stats": {"mem_avail": 89, "mem_total": 3900, "swap_avail": 0, "swap_total": 0},
					"status": {"agent": "v3.0.0", "is_overloaded": true, "frozen_at": "2026-07-14T10:00:00Z"},
					"monitor": {"state": "maintenance"}
				}
			},
			"object": {
				"prod/svc/api": {
					"avail": "up", "overall": "up", "provisioned": "true",
					"frozen": "unfrozen", "placement_state": "optimal", "up_instances_count": 2,
					"scope": ["node-a", "node-b"]
				},
				"prod/svc/db": {
					"avail": "down", "overall": "down", "provisioned": "false",
					"frozen": "frozen", "placement_state": "non-optimal", "scope": ["node-b"]
				},
				"system/sec/ca": {"overall": "up"}
			}
		}
	}`})

	health, err := service.GetClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("get cluster health: %v", err)
	}
	if health.Healthy {
		t.Fatal("expected degraded cluster health")
	}
	if len(health.Cluster.LeaderNodes) != 1 || health.Cluster.LeaderNodes[0] != "node-a" {
		t.Fatalf("got leaders %v, want [node-a]", health.Cluster.LeaderNodes)
	}
	if health.NodeSummary.Total != 2 || health.NodeSummary.Healthy != 1 {
		t.Errorf("got node summary %+v, want total=2 healthy=1", health.NodeSummary)
	}
	if health.NodeSummary.Frozen != 1 || health.NodeSummary.Overloaded != 1 || health.NodeSummary.NonIdle != 1 {
		t.Errorf("got node summary %+v, want one frozen, overloaded, and non-idle node", health.NodeSummary)
	}
	overloadIssue := findNodeIssue(health.Nodes[1].Issues, "swap_below_threshold")
	if overloadIssue == nil {
		t.Fatalf("got node issues %+v, want swap_below_threshold", health.Nodes[1].Issues)
	}
	if overloadIssue.Evidence == nil || overloadIssue.Evidence.SwapTotalMB == nil || *overloadIssue.Evidence.SwapTotalMB != 0 ||
		overloadIssue.Evidence.SwapAvailablePct == nil || *overloadIssue.Evidence.SwapAvailablePct != 0 ||
		overloadIssue.Evidence.MinimumSwapAvailPct == nil || *overloadIssue.Evidence.MinimumSwapAvailPct != 10 {
		t.Fatalf("got overload evidence %+v, want swap total=0 available=0 minimum=10", overloadIssue.Evidence)
	}
	if overloadIssue.Policy == nil || overloadIssue.Policy.ConfigKey != "node.min_avail_swap_pct" {
		t.Fatalf("got overload policy %+v, want node.min_avail_swap_pct", overloadIssue.Policy)
	}
	disableOption := findRemediationOption(overloadIssue.RemediationOptions, "disable_swap_threshold")
	if disableOption == nil || disableOption.Configuration == nil || disableOption.Configuration.Key != "node.min_avail_swap_pct" || disableOption.Configuration.Value != 0 {
		t.Fatalf("got remediation option %+v, want node.min_avail_swap_pct=0", disableOption)
	}
	encodedIssue, err := json.Marshal(overloadIssue)
	if err != nil {
		t.Fatalf("marshal overload issue: %v", err)
	}
	for _, expected := range []string{`"swap_total_mb":0`, `"swap_available_pct":0`, `"disabled_when_value_is":0`, `"value":0`} {
		if !bytes.Contains(encodedIssue, []byte(expected)) {
			t.Errorf("encoded issue %s does not preserve %s", encodedIssue, expected)
		}
	}
	if health.ObjectSummary.Total != 2 || health.ObjectSummary.Up != 1 || health.ObjectSummary.Down != 1 || health.ObjectSummary.Problems != 1 {
		t.Errorf("got object summary %+v, want two actor objects and one problem", health.ObjectSummary)
	}
	if len(health.ProblemObjects) != 1 || health.ProblemObjects[0].Path != "prod/svc/db" {
		t.Fatalf("got problem objects %+v, want prod/svc/db", health.ProblemObjects)
	}
}

func TestOverloadIssuesMemoryAndFallback(t *testing.T) {
	issues := overloadIssues(
		&clusterNodeConfig{MinAvailMemPct: 15, MinAvailSwapPct: 10},
		&clusterNodeStats{MemAvailPct: 8, MemTotalMB: 4096, SwapAvailPct: 80, SwapTotalMB: 1024},
	)
	if len(issues) != 1 || issues[0].Code != "memory_below_threshold" {
		t.Fatalf("got issues %+v, want memory_below_threshold", issues)
	}
	if issues[0].Evidence == nil || issues[0].Evidence.MemoryAvailablePct == nil || *issues[0].Evidence.MemoryAvailablePct != 8 {
		t.Fatalf("got memory evidence %+v, want available memory 8%%", issues[0].Evidence)
	}

	issues = overloadIssues(
		&clusterNodeConfig{MinAvailMemPct: 2, MinAvailSwapPct: 0},
		&clusterNodeStats{MemAvailPct: 80},
	)
	if len(issues) != 1 || issues[0].Code != "overload_cause_undetermined" {
		t.Fatalf("got issues %+v, want overload_cause_undetermined", issues)
	}
}

func TestGetClusterHealthHealthy(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {"id": "cluster-123", "name": "prod", "nodes": ["node-a"]},
			"status": {"is_compat": true, "is_frozen": false},
			"node": {
				"node-a": {
					"status": {"agent": "v3.0.0", "is_leader": true, "frozen_at": "0001-01-01T00:00:00Z"},
					"monitor": {"state": "idle"}
				}
			},
			"object": {
				"prod/svc/api": {
					"avail": "up", "overall": "up", "provisioned": "true",
					"frozen": "unfrozen", "placement_state": "optimal", "up_instances_count": 1,
					"scope": ["node-a"]
				}
			}
		}
	}`})

	health, err := service.GetClusterHealth(context.Background())
	if err != nil {
		t.Fatalf("get cluster health: %v", err)
	}
	if !health.Healthy {
		t.Fatalf("expected healthy cluster, got %+v", health)
	}
	if len(health.Cluster.Issues) != 0 || len(health.ProblemObjects) != 0 {
		t.Fatalf("expected no issues, got %+v", health)
	}
}

func findNodeIssue(issues []ClusterNodeHealthIssue, code string) *ClusterNodeHealthIssue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

func findRemediationOption(options []ClusterNodeHealthRemediationOption, id string) *ClusterNodeHealthRemediationOption {
	for i := range options {
		if options[i].ID == id {
			return &options[i]
		}
	}
	return nil
}
