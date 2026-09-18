package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
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
