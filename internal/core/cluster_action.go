package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ClusterAction is an action on the whole cluster. The cluster actions
// require root.
type ClusterAction string

const (
	ClusterActionFreeze   ClusterAction = "freeze"
	ClusterActionUnfreeze ClusterAction = "unfreeze"
	ClusterActionAbort    ClusterAction = "abort"
)

type ClusterActionResult struct {
	Provenance      Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Action          string     `json:"action" jsonschema:"freeze, unfreeze or abort"`
	OrchestrationID string     `json:"orchestration_id" jsonschema:"the orchestration of the node that accepted the action; pass it, with node, to list_daemon_orchestrations and get_node_logs"`
	Node            string     `json:"node" jsonschema:"the node that accepted the action, whose orchestration the MCP follows; the other nodes converge after it"`
	NodeOrchestrationOutcome
	// The cluster, read again at the end of the wait.
	IsFrozen     *bool               `json:"is_frozen,omitempty" jsonschema:"whether the cluster is frozen, read again at the end of the wait"`
	Nodes        []ClusterNodeFreeze `json:"nodes,omitempty" jsonschema:"each node of the cluster and the time it was frozen, read again at the end of the wait"`
	PendingNodes []string            `json:"pending_nodes,omitempty" jsonschema:"for a freeze, the nodes not frozen yet at the end of the wait; for an unfreeze, those still frozen: they converge after the node that accepted the action, a node down adopts it when it comes back"`
	StatusError  string              `json:"status_error,omitempty" jsonschema:"why the cluster could not be read again at the end of the wait"`
}

type ClusterNodeFreeze struct {
	Node     string `json:"node" jsonschema:"the node name"`
	FrozenAt string `json:"frozen_at,omitempty" jsonschema:"when the node was frozen, empty when it is not"`
}

// RunClusterAction freezes, unfreezes the cluster, or aborts its
// orchestration: the node answering sets the global target of its monitor,
// which the other nodes adopt. The MCP follows the orchestration of that
// node a bounded time, then reads the cluster again.
func (s *Service) RunClusterAction(ctx context.Context, action ClusterAction, wait time.Duration) (ClusterActionResult, error) {
	switch action {
	case ClusterActionFreeze, ClusterActionUnfreeze:
		if wait == 0 {
			wait = defaultInstanceActionWait
		}
		if wait < time.Second || wait > maxInstanceActionWait {
			return ClusterActionResult{}, fmt.Errorf("wait must be between 1 and %d seconds", int(maxInstanceActionWait.Seconds()))
		}
	case ClusterActionAbort:
		wait = abortNodeWait
	default:
		return ClusterActionResult{}, fmt.Errorf("unsupported cluster action %q", action)
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return ClusterActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	before, err := s.getClusterStatus(ctx)
	if err != nil {
		return ClusterActionResult{}, fmt.Errorf("%s cluster: %w", action, err)
	}
	var queued daemonOrchestrationQueued
	if err := poster.PostJSON(ctx, "/api/cluster/action/"+string(action), nil, nil, &queued); err != nil {
		return ClusterActionResult{}, fmt.Errorf("%s cluster: %w", action, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(queued.OrchestrationID) {
		return ClusterActionResult{}, fmt.Errorf("%s cluster: daemon returned no valid orchestration id", action)
	}
	node := queued.Node
	if node == "" {
		node = servedBy(before)
	}
	if !validExactNodeName(node) {
		return ClusterActionResult{}, fmt.Errorf("%s cluster: daemon named no valid node for the orchestration", action)
	}
	result := ClusterActionResult{Action: string(action), OrchestrationID: strings.ToLower(queued.OrchestrationID), Node: node}
	outcome, err := s.waitNodeOrchestration(ctx, node, result.OrchestrationID, wait)
	if err != nil {
		return ClusterActionResult{}, err
	}
	result.NodeOrchestrationOutcome = outcome
	if after, err := s.getClusterStatus(ctx); err != nil {
		result.StatusError = err.Error()
	} else {
		isFrozen := after.Cluster.Status.IsFrozen
		result.IsFrozen = &isFrozen
		for name, status := range after.Cluster.Node {
			frozenAt := ""
			if isSetTime(status.Status.FrozenAt) {
				frozenAt = status.Status.FrozenAt
			}
			result.Nodes = append(result.Nodes, ClusterNodeFreeze{Node: name, FrozenAt: frozenAt})
			if (action == ClusterActionFreeze && frozenAt == "") || (action == ClusterActionUnfreeze && frozenAt != "") {
				result.PendingNodes = append(result.PendingNodes, name)
			}
		}
		sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Node < result.Nodes[j].Node })
		sort.Strings(result.PendingNodes)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// isSetTime says whether a daemon time is set: the daemon gives the zero
// time for one that is not.
func isSetTime(value string) bool {
	if value == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || !parsed.IsZero()
}
