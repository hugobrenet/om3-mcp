package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

// NodeAction is an action on one node of the cluster. The node actions
// require root.
type NodeAction string

const (
	NodeActionFreeze   NodeAction = "freeze"
	NodeActionUnfreeze NodeAction = "unfreeze"
	NodeActionDrain    NodeAction = "drain"
	NodeActionAbort    NodeAction = "abort"
)

const (
	clearNodeWait = 10 * time.Second
	// abortNodeWait bounds the read of an abort, which the node monitor
	// accepts or refuses at once.
	abortNodeWait = 10 * time.Second
	// maxNodeInstances bounds the instances a drain lists.
	maxNodeInstances = 100
)

// failedNodeMonitorStates are the node monitor states of an action that
// failed, the ones a clear lifts: the daemon sets the state a clear asks
// without looking at the current one.
var failedNodeMonitorStates = []string{"freeze failed", "unfreeze failed", "drain failed", "shutdown failed"}

type NodeExecActionResult struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       string     `json:"node" jsonschema:"the node acted on"`
	Action     string     `json:"action" jsonschema:"the node action run"`
	SessionID  string     `json:"session_id" jsonschema:"the session the execution belongs to"`
	ExecID     string     `json:"exec_id" jsonschema:"the execution the node forked for the action; pass it, with the node, to list_daemon_executions and get_node_logs"`
	ExecutionOutcome
}

// NodeOrchestrationOutcome is what the MCP read of an orchestration a node
// accepted: whether it ended within the wait, and how.
type NodeOrchestrationOutcome struct {
	Completed          bool    `json:"completed" jsonschema:"whether the orchestration ended within the wait; false, it still runs, or could not be read"`
	State              string  `json:"state,omitempty" jsonschema:"the state of the orchestration reported by OpenSVC, when read: running, succeeded, failed, aborted or refused"`
	Error              *string `json:"error,omitempty" jsonschema:"the bounded error of the orchestration reported by OpenSVC, such as the monitor state it failed on, or why the monitor refused it"`
	OrchestrationError string  `json:"orchestration_error,omitempty" jsonschema:"why the orchestration could not be read; the request was accepted all the same"`
}

// NodeInstance is an instance a node holds.
type NodeInstance struct {
	Object       string `json:"object" jsonschema:"the canonical path of the object"`
	Availability string `json:"availability" jsonschema:"the instance availability reported by OpenSVC"`
}

type NodeDrainResult struct {
	Provenance      Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node            string     `json:"node" jsonschema:"the node drained"`
	OrchestrationID string     `json:"orchestration_id" jsonschema:"the orchestration of the drain; pass it, with the node, to list_daemon_orchestrations and get_node_logs"`
	// InstancesUp are the svc instances the shutdown of the drain stops, as
	// read before it.
	InstancesUp          []NodeInstance `json:"instances_up" jsonschema:"the svc instances up, warn or stdby up on the node before the drain, the ones its shutdown stops"`
	InstancesUpTruncated bool           `json:"instances_up_truncated,omitempty" jsonschema:"whether instances were left out after the first 100"`
	InstancesError       string         `json:"instances_error,omitempty" jsonschema:"why the instances of the node could not be read before the drain"`
	NodeOrchestrationOutcome
	// The node, read again at the end of the wait.
	MonitorState string `json:"monitor_state,omitempty" jsonschema:"the node monitor state read at the end of the wait: idle after a drain done, drain failed after a failure"`
	LocalExpect  string `json:"local_expect,omitempty" jsonschema:"the node monitor local target read at the end of the wait: drained while the drain runs, none once it ended"`
	FrozenAt     string `json:"frozen_at,omitempty" jsonschema:"the time the node was frozen, read at the end of the wait; the drain leaves the node frozen"`
	StatusError  string `json:"status_error,omitempty" jsonschema:"why the node status could not be read again at the end of the wait"`
}

type NodeAbortResult struct {
	Provenance      Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node            string     `json:"node" jsonschema:"the node whose orchestration is aborted"`
	OrchestrationID string     `json:"orchestration_id" jsonschema:"the identifier the daemon gives the abort"`
	NodeOrchestrationOutcome
}

type NodeClearResult struct {
	Provenance    Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node          string     `json:"node" jsonschema:"the node cleared"`
	PreviousState string     `json:"previous_state" jsonschema:"the failed node monitor state read before the clear"`
	CurrentState  string     `json:"current_state" jsonschema:"the node monitor state read last after the clear"`
	Cleared       bool       `json:"cleared" jsonschema:"whether the node monitor state was seen to leave the failed state within 10 seconds"`
}

// RunNodeExecAction freezes or unfreezes one node, then waits a bounded
// time for the end of the execution the node forked for it.
func (s *Service) RunNodeExecAction(ctx context.Context, node string, action NodeAction, wait time.Duration) (NodeExecActionResult, error) {
	if action != NodeActionFreeze && action != NodeActionUnfreeze {
		return NodeExecActionResult{}, fmt.Errorf("unsupported node action %q", action)
	}
	wait, err := validateNodeAction(node, wait)
	if err != nil {
		return NodeExecActionResult{}, err
	}
	return s.runNodeExec(ctx, node, string(action), nil, wait)
}

// runNodeExec posts one node action the daemon runs as an execution, then
// waits a bounded time for its end.
func (s *Service) runNodeExec(ctx context.Context, node string, action string, query url.Values, wait time.Duration) (NodeExecActionResult, error) {
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return NodeExecActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	if _, err := s.GetNodeStatus(ctx, node); err != nil {
		return NodeExecActionResult{}, fmt.Errorf("%s node %s: %w", action, node, err)
	}
	var accepted daemonInstanceActionAccepted
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/action/%s", node, action), query, nil, &accepted); err != nil {
		return NodeExecActionResult{}, fmt.Errorf("%s node %s: %w", action, node, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(accepted.SessionID) || !daemonExecutionUUIDPattern.MatchString(accepted.ExecID) {
		return NodeExecActionResult{}, fmt.Errorf("%s node %s: daemon returned no valid session and exec ids", action, node)
	}
	result := NodeExecActionResult{
		Node:      node,
		Action:    string(action),
		SessionID: strings.ToLower(accepted.SessionID),
		ExecID:    strings.ToLower(accepted.ExecID),
	}
	outcome, err := s.waitExecution(ctx, node, result.ExecID, wait)
	if err != nil {
		return NodeExecActionResult{}, err
	}
	result.ExecutionOutcome = outcome
	result.Provenance = s.newProvenance()
	return result, nil
}

// validateNodeAction checks the exact node and the wait; zero, 10 seconds.
func validateNodeAction(node string, wait time.Duration) (time.Duration, error) {
	if err := validateNodeTarget(node); err != nil {
		return 0, err
	}
	if wait == 0 {
		wait = defaultInstanceActionWait
	}
	if wait < time.Second || wait > maxInstanceActionWait {
		return 0, fmt.Errorf("wait must be between 1 and %d seconds", int(maxInstanceActionWait.Seconds()))
	}
	return wait, nil
}

// DrainNode drains one node: its monitor freezes it, then shuts down every
// svc instance it holds. The MCP follows the orchestration a bounded time,
// then reads the node again.
func (s *Service) DrainNode(ctx context.Context, node string, wait time.Duration) (NodeDrainResult, error) {
	wait, err := validateNodeAction(node, wait)
	if err != nil {
		return NodeDrainResult{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return NodeDrainResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	if _, err := s.GetNodeStatus(ctx, node); err != nil {
		return NodeDrainResult{}, fmt.Errorf("drain node %s: %w", node, err)
	}
	instances, truncated, instancesErr := s.nodeInstancesUp(ctx, node)
	var queued daemonOrchestrationQueued
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/action/drain", node), nil, nil, &queued); err != nil {
		return NodeDrainResult{}, fmt.Errorf("drain node %s: %w", node, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(queued.OrchestrationID) {
		return NodeDrainResult{}, fmt.Errorf("drain node %s: daemon returned no valid orchestration id", node)
	}
	result := NodeDrainResult{Node: node, OrchestrationID: strings.ToLower(queued.OrchestrationID), InstancesUp: instances, InstancesUpTruncated: truncated}
	if instancesErr != nil {
		result.InstancesError = instancesErr.Error()
	}
	outcome, err := s.waitNodeOrchestration(ctx, node, result.OrchestrationID, wait)
	if err != nil {
		return NodeDrainResult{}, err
	}
	result.NodeOrchestrationOutcome = outcome
	if status, err := s.GetNodeStatus(ctx, node); err != nil {
		result.StatusError = err.Error()
	} else {
		result.MonitorState = status.Monitor.State
		result.LocalExpect = status.Monitor.LocalExpect
		result.FrozenAt = status.Status.FrozenAt
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// nodeInstancesUp reads the svc instances the node holds up, warn or in
// standby, the ones the shutdown of a drain stops, sorted by object.
func (s *Service) nodeInstancesUp(ctx context.Context, node string) ([]NodeInstance, bool, error) {
	var response daemonInstanceList
	if err := s.client.GetJSON(ctx, "/api/instance", url.Values{"node": {node}}, &response); err != nil {
		return []NodeInstance{}, false, fmt.Errorf("list the instances of node %s: %w", node, err)
	}
	instances := []NodeInstance{}
	for _, item := range response.Items {
		reference, err := validateExactObjectPath(item.Meta.Object)
		if item.Meta.Node != node || err != nil || reference.Kind != "svc" || item.Data.Status == nil {
			continue
		}
		switch item.Data.Status.Availability {
		case "up", "warn", "stdby up":
			instances = append(instances, NodeInstance{Object: reference.Path, Availability: item.Data.Status.Availability})
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Object < instances[j].Object })
	if len(instances) > maxNodeInstances {
		return instances[:maxNodeInstances], true, nil
	}
	return instances, false, nil
}

// waitNodeOrchestration reads the orchestration on the node that accepted it
// until it ends or the wait expires. The node answers 410 for an
// orchestration it does not know yet, or no more: the read goes on until the
// wait expires.
func (s *Service) waitNodeOrchestration(ctx context.Context, node string, orchestrationID string, wait time.Duration) (NodeOrchestrationOutcome, error) {
	var outcome NodeOrchestrationOutcome
	endpoint := fmt.Sprintf("/api/node/name/%s/daemon/orchestration/id/%s", node, orchestrationID)
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for attempt := 0; ; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return outcome, ctx.Err()
		case <-timer.C:
		}
		var item daemonOrchestrationResponse
		if err := s.client.GetJSON(waitCtx, endpoint, nil, &item); err != nil {
			if ctx.Err() != nil {
				return outcome, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return outcome, nil
			}
			var problem daemonProblem
			if errors.As(err, &problem) && problem.HTTPStatus() == http.StatusGone {
				outcome.OrchestrationError = err.Error()
				continue
			}
			if errors.As(err, &problem) && problem.HTTPStatus() == http.StatusForbidden {
				outcome.OrchestrationError = "reading the orchestration requires root access: " + err.Error()
			} else {
				outcome.OrchestrationError = err.Error()
			}
			return outcome, nil
		}
		outcome.OrchestrationError = ""
		if item.State == "" || len(item.State) > maxDaemonOrchestrationStateLength {
			outcome.OrchestrationError = fmt.Sprintf("read orchestration %s: invalid state", orchestrationID)
			return outcome, nil
		}
		outcome.State = item.State
		if item.EndedAt != nil {
			outcome.Completed = true
			if item.Error != nil {
				value, _ := boundedRunes(*item.Error, maxDaemonOrchestrationErrorRunes)
				outcome.Error = &value
			}
			return outcome, nil
		}
	}
}

// AbortNode aborts the orchestration running on one node, such as a drain:
// the daemon sets its local target to none. It answers at once, and its
// monitor refuses a node running none: the MCP reads the orchestration of
// the abort, refused with the reason then.
func (s *Service) AbortNode(ctx context.Context, node string) (NodeAbortResult, error) {
	if err := validateNodeTarget(node); err != nil {
		return NodeAbortResult{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return NodeAbortResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	if _, err := s.GetNodeStatus(ctx, node); err != nil {
		return NodeAbortResult{}, fmt.Errorf("abort node %s: %w", node, err)
	}
	var queued daemonOrchestrationQueued
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/action/abort", node), nil, nil, &queued); err != nil {
		return NodeAbortResult{}, fmt.Errorf("abort node %s: %w", node, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(queued.OrchestrationID) {
		return NodeAbortResult{}, fmt.Errorf("abort node %s: daemon returned no valid orchestration id", node)
	}
	result := NodeAbortResult{Node: node, OrchestrationID: strings.ToLower(queued.OrchestrationID)}
	outcome, err := s.waitNodeOrchestration(ctx, node, result.OrchestrationID, abortNodeWait)
	if err != nil {
		return NodeAbortResult{}, err
	}
	result.NodeOrchestrationOutcome = outcome
	result.Provenance = s.newProvenance()
	return result, nil
}

// ClearNode lifts the failed monitor state of one node, then reads it again
// for a bounded time.
func (s *Service) ClearNode(ctx context.Context, node string) (NodeClearResult, error) {
	if err := validateNodeTarget(node); err != nil {
		return NodeClearResult{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return NodeClearResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	previous, err := s.GetNodeStatus(ctx, node)
	if err != nil {
		return NodeClearResult{}, fmt.Errorf("clear node %s: %w", node, err)
	}
	switch state := previous.Monitor.State; {
	case slices.Contains(failedNodeMonitorStates, state):
	case state == "idle":
		return NodeClearResult{}, fmt.Errorf("clear node %s: nothing to clear: its monitor state is idle", node)
	default:
		return NodeClearResult{}, fmt.Errorf("clear node %s: its monitor state is %q, not a failure: an action is running or ending, and a clear would set the monitor idle under it", node, state)
	}
	// The daemon answers 200 with a null body.
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/action/clear", node), nil, nil, nil); err != nil {
		return NodeClearResult{}, fmt.Errorf("clear node %s: %w", node, err)
	}
	result := NodeClearResult{Node: node, PreviousState: previous.Monitor.State, CurrentState: previous.Monitor.State}
	waitCtx, cancel := context.WithTimeout(ctx, clearNodeWait)
	defer cancel()
	for attempt := 0; !result.Cleared; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return NodeClearResult{}, ctx.Err()
			}
			result.Provenance = s.newProvenance()
			return result, nil
		case <-timer.C:
		}
		current, err := s.GetNodeStatus(waitCtx, node)
		if err != nil {
			if ctx.Err() != nil {
				return NodeClearResult{}, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				break
			}
			return NodeClearResult{}, fmt.Errorf("clear node %s: read it again: %w", node, err)
		}
		result.CurrentState = current.Monitor.State
		result.Cleared = current.Monitor.State != previous.Monitor.State
	}
	result.Provenance = s.newProvenance()
	return result, nil
}
