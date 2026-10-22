package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// InstanceAction is an action on one instance of an object, run by the
// daemon of its node at once, outside of the orchestration of the object.
type InstanceAction string

const (
	InstanceActionStart    InstanceAction = "start"
	InstanceActionStop     InstanceAction = "stop"
	InstanceActionRestart  InstanceAction = "restart"
	InstanceActionFreeze   InstanceAction = "freeze"
	InstanceActionUnfreeze InstanceAction = "unfreeze"
)

// instanceActionKinds are the object kinds the instance actions accept, as
// the daemon API declares them.
var instanceActionKinds = []string{"svc", "vol"}

// clearInstanceKinds are the object kinds the clear of an instance accepts.
var clearInstanceKinds = []string{"svc", "vol", "cfg", "nscfg", "sec", "usr"}

const (
	defaultInstanceActionWait = 10 * time.Second
	maxInstanceActionWait     = 60 * time.Second
	clearInstanceWait         = 10 * time.Second
	// instanceActionLogLines are the last lines of the journal of a failed
	// execution the result carries.
	instanceActionLogLines = 20
)

// failedMonitorStates are the monitor states of an action that failed, the
// ones a clear lifts. The daemon sets the state a clear asks without looking
// at the current one: a clear of an action running would set the monitor
// idle under it.
var failedMonitorStates = []string{
	"boot failed", "shutdown failed", "start failed", "stop failed", "freeze failed", "unfreeze failed",
	"provision failed", "unprovision failed", "delete failed", "resize failed", "cap failed", "prepare failed",
}

type InstanceActionOptions struct {
	// Rids are the exact ids of the resources a start, stop or restart
	// applies to; none, the whole instance.
	Rids []string
	// DisableRollback has a start or a restart leave started the resources
	// it started before a failure, rather than stopping them.
	DisableRollback bool
	// InterruptSyncs has a stop end the syncs running on the instance
	// instead of waiting for them to end.
	InterruptSyncs bool
	// ConfigUpdatedAt has the action wait for the configuration written at
	// that time, as update_object_config gives it, on the node.
	ConfigUpdatedAt string
	// Wait is how long the MCP waits for the end of the execution; zero, 10
	// seconds.
	Wait time.Duration
}

type InstanceActionResult struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Node       string                 `json:"node" jsonschema:"the node of the instance"`
	Action     string                 `json:"action" jsonschema:"the instance action run"`
	Resources  []string               `json:"resources" jsonschema:"the resource ids the action applies to; empty for the whole instance"`
	SessionID  string                 `json:"session_id" jsonschema:"the session the execution belongs to"`
	ExecID     string                 `json:"exec_id" jsonschema:"the execution the node forked for the action; pass it, with the node, to list_daemon_executions and get_node_logs"`
	// Instance is the instance as read before the action.
	Instance ObjectInstanceStatus `json:"instance" jsonschema:"the instance as read before the action"`
	// Completed says whether the execution ended within the wait.
	Completed bool    `json:"completed" jsonschema:"whether the execution ended within the wait; false, it still runs, or could not be read"`
	State     string  `json:"state,omitempty" jsonschema:"the state of the execution reported by OpenSVC, when read"`
	ExitCode  *int    `json:"exit_code,omitempty" jsonschema:"the exit code of the execution when it ended: 0 succeeded"`
	Error     *string `json:"error,omitempty" jsonschema:"the bounded error of the execution reported by OpenSVC, when it failed"`
	// ExecutionError is why the execution could not be read: the action is
	// running or ran all the same.
	ExecutionError string `json:"execution_error,omitempty" jsonschema:"why the execution could not be read, such as the root access its read requires, or 410 for an execution the daemon ran and has dropped since; the action was accepted all the same: check get_instance_status"`
	// LogTail is the end of the journal of a failed execution, where its
	// cause is: the error of the execution is the exit of the process.
	LogTail      []InstanceActionLogLine `json:"log_tail,omitempty" jsonschema:"for an execution that failed, the last 20 lines at most of its journal on the node, in time order, where its cause is; get_node_logs with the node and exec_id gives more"`
	LogTruncated bool                    `json:"log_truncated,omitempty" jsonschema:"whether older lines of the journal of the execution, or message content, were left out"`
	LogError     string                  `json:"log_error,omitempty" jsonschema:"why the journal of the failed execution could not be read; the execution result stands"`
}

type InstanceActionLogLine struct {
	Time    string `json:"time" jsonschema:"the time of the line"`
	Level   string `json:"level,omitempty" jsonschema:"the OpenSVC log level, when given"`
	Message string `json:"message" jsonschema:"the bounded message"`
}

type InstanceClearResult struct {
	Provenance    Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object        ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Node          string                 `json:"node" jsonschema:"the node of the instance"`
	PreviousState string                 `json:"previous_state" jsonschema:"the failed monitor state read before the clear"`
	CurrentState  string                 `json:"current_state" jsonschema:"the monitor state read last after the clear"`
	Cleared       bool                   `json:"cleared" jsonschema:"whether the monitor state was seen to leave the failed state within 10 seconds"`
}

type daemonInstanceActionAccepted struct {
	SessionID string `json:"session_id"`
	ExecID    string `json:"exec_id"`
}

// RunInstanceAction runs one action on the instance of one exact object on
// one node, then waits a bounded time for the end of the execution the node
// forked for it.
func (s *Service) RunInstanceAction(ctx context.Context, path string, node string, action InstanceAction, options InstanceActionOptions) (InstanceActionResult, error) {
	reference, err := validateInstanceTarget(path, node, string(action), instanceActionKinds)
	if err != nil {
		return InstanceActionResult{}, err
	}
	query, err := instanceActionQuery(action, options)
	if err != nil {
		return InstanceActionResult{}, err
	}
	wait := options.Wait
	if wait == 0 {
		wait = defaultInstanceActionWait
	}
	if wait < time.Second || wait > maxInstanceActionWait {
		return InstanceActionResult{}, fmt.Errorf("wait must be between 1 and %d seconds", int(maxInstanceActionWait.Seconds()))
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return InstanceActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	instance, err := s.refuseInstanceAction(ctx, reference, node, action)
	if err != nil {
		return InstanceActionResult{}, err
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/instance/path/%s/%s/%s/action/%s", node, reference.Namespace, reference.Kind, reference.Name, action)
	var accepted daemonInstanceActionAccepted
	if err := poster.PostJSON(ctx, endpoint, query, nil, &accepted); err != nil {
		return InstanceActionResult{}, fmt.Errorf("%s instance %s on %s: %w", action, reference.Path, node, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(accepted.SessionID) || !daemonExecutionUUIDPattern.MatchString(accepted.ExecID) {
		return InstanceActionResult{}, fmt.Errorf("%s instance %s on %s: daemon returned no valid session and exec ids", action, reference.Path, node)
	}
	result := InstanceActionResult{
		Object:    reference,
		Node:      node,
		Action:    string(action),
		Resources: append([]string{}, options.Rids...),
		SessionID: strings.ToLower(accepted.SessionID),
		ExecID:    strings.ToLower(accepted.ExecID),
		Instance:  instance,
	}
	if err := s.waitInstanceExecution(ctx, &result, wait); err != nil {
		return InstanceActionResult{}, err
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// validateInstanceTarget checks the exact object, of a kind the action
// accepts, and the exact node of the instance.
func validateInstanceTarget(path string, node string, action string, kinds []string) (ClusterObjectReference, error) {
	if strings.ContainsAny(path, "*?[],\\") {
		return ClusterObjectReference{}, fmt.Errorf("path must be one exact OpenSVC object path, without wildcard or selector")
	}
	reference, err := validateExactObjectPath(path)
	if err != nil {
		return ClusterObjectReference{}, err
	}
	if !slices.Contains(kinds, reference.Kind) {
		return ClusterObjectReference{}, fmt.Errorf("the %s action applies to %s objects only", action, strings.Join(kinds, ", "))
	}
	if err := validateNodeTarget(node); err != nil {
		return ClusterObjectReference{}, err
	}
	return reference, nil
}

// instanceActionQuery checks the options, each on the action that takes it,
// and gives the query of the action.
func instanceActionQuery(action InstanceAction, options InstanceActionOptions) (url.Values, error) {
	switch action {
	case InstanceActionStart, InstanceActionStop, InstanceActionRestart, InstanceActionFreeze, InstanceActionUnfreeze:
	default:
		return nil, fmt.Errorf("unsupported instance action %q", action)
	}
	query := url.Values{}
	if len(options.Rids) > 0 {
		if action == InstanceActionFreeze || action == InstanceActionUnfreeze {
			return nil, fmt.Errorf("rids apply to the start, stop and restart actions only")
		}
		if len(options.Rids) > maxEnableResources {
			return nil, fmt.Errorf("at most %d resource ids are accepted", maxEnableResources)
		}
		for _, rid := range options.Rids {
			if !exactResourceID.MatchString(rid) {
				return nil, fmt.Errorf("resource %q must be one exact resource id, such as fs#1", rid)
			}
		}
		query.Set("rid", strings.Join(options.Rids, ","))
	}
	if options.DisableRollback {
		if action != InstanceActionStart && action != InstanceActionRestart {
			return nil, fmt.Errorf("disable_rollback applies to the start and restart actions only")
		}
		query.Set("disable_rollback", "true")
	}
	if options.InterruptSyncs {
		if action != InstanceActionStop {
			return nil, fmt.Errorf("interrupt_syncs applies to the stop action only")
		}
		query.Set("interrupt_syncs", "true")
	}
	if options.ConfigUpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, options.ConfigUpdatedAt); err != nil {
			return nil, fmt.Errorf("config_updated_at must be an RFC 3339 time, the last_modified update_object_config gives")
		}
		query.Set("config_updated_at", options.ConfigUpdatedAt)
	}
	return query, nil
}

// refuseInstanceAction reads the instances of the object: the one of the
// node must exist. The daemon starts an instance of a failover service while
// another runs on a peer, which would run it twice, on the same shared
// storage: a start or a restart is refused when another instance is up or
// warn.
func (s *Service) refuseInstanceAction(ctx context.Context, reference ClusterObjectReference, node string, action InstanceAction) (ObjectInstanceStatus, error) {
	instances, err := s.ListObjectInstances(ctx, ListObjectInstancesOptions{Path: reference.Path, Limit: maxListObjectInstancesLimit})
	if err != nil {
		return ObjectInstanceStatus{}, fmt.Errorf("%s instance %s on %s: read its instances: %w", action, reference.Path, node, err)
	}
	index := slices.IndexFunc(instances.Instances, func(instance ObjectInstanceStatus) bool { return instance.Node == node })
	if index < 0 {
		return ObjectInstanceStatus{}, fmt.Errorf("%s instance %s on %s: the object has no instance on that node, or it is not visible to the caller", action, reference.Path, node)
	}
	if action != InstanceActionStart && action != InstanceActionRestart {
		return instances.Instances[index], nil
	}
	status, err := s.GetObjectStatus(ctx, reference.Path)
	if err != nil {
		return ObjectInstanceStatus{}, fmt.Errorf("%s instance %s on %s: read its topology: %w", action, reference.Path, node, err)
	}
	if status.Topology != "failover" {
		return instances.Instances[index], nil
	}
	for _, instance := range instances.Instances {
		if instance.Node != node && (instance.Availability == "up" || instance.Availability == "warn") {
			return ObjectInstanceStatus{}, fmt.Errorf("%s instance %s on %s: the service is failover and its instance on %s is %s: it runs on one node at a time; switch_object moves it", action, reference.Path, node, instance.Node, instance.Availability)
		}
	}
	return instances.Instances[index], nil
}

// waitInstanceExecution reads the execution until it ends or the wait
// expires. A read refused, such as for a caller without root access, ends
// the wait with the reason: the action runs all the same.
func (s *Service) waitInstanceExecution(ctx context.Context, result *InstanceActionResult, wait time.Duration) error {
	endpoint := fmt.Sprintf("/api/node/name/%s/daemon/exec/id/%s", result.Node, result.ExecID)
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for attempt := 0; ; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		var item daemonExecutionResponse
		if err := s.client.GetJSON(waitCtx, endpoint, nil, &item); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return nil
			}
			var problem daemonProblem
			if errors.As(err, &problem) && problem.HTTPStatus() == http.StatusForbidden {
				result.ExecutionError = "reading the execution requires root access: " + err.Error()
			} else {
				result.ExecutionError = err.Error()
			}
			return nil
		}
		if _, err := validateDaemonExecutionResponse(item); err != nil {
			result.ExecutionError = fmt.Sprintf("read execution %s: %s", result.ExecID, err)
			return nil
		}
		execution, _ := daemonExecutionOutput(item)
		result.State = execution.State
		if execution.EndedAt != nil {
			result.Completed = true
			result.ExitCode = execution.ExitCode
			result.Error = execution.Error
			if result.ExitCode == nil || *result.ExitCode != 0 {
				s.readInstanceExecutionLog(ctx, result)
			}
			return nil
		}
	}
}

// readInstanceExecutionLog gives a failed execution the end of its journal,
// read once, where the cause of the failure is.
func (s *Service) readInstanceExecutionLog(ctx context.Context, result *InstanceActionResult) {
	logs, err := s.GetNodeLogs(ctx, GetNodeLogsOptions{Node: result.Node, ExecID: result.ExecID, Lines: instanceActionLogLines})
	if err != nil {
		result.LogError = err.Error()
		return
	}
	result.LogTail = make([]InstanceActionLogLine, 0, len(logs.Entries))
	for _, entry := range logs.Entries {
		result.LogTail = append(result.LogTail, InstanceActionLogLine{Time: entry.Timestamp, Level: entry.Level, Message: entry.Message})
	}
	result.LogTruncated = logs.Truncated
}

// ClearInstance lifts the failed monitor state of the instance of one exact
// object on one node, then reads it again for a bounded time.
func (s *Service) ClearInstance(ctx context.Context, path string, node string) (InstanceClearResult, error) {
	reference, err := validateInstanceTarget(path, node, "clear", clearInstanceKinds)
	if err != nil {
		return InstanceClearResult{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return InstanceClearResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	previous, err := s.getExactObjectInstance(ctx, reference.Path, node)
	if err != nil {
		return InstanceClearResult{}, fmt.Errorf("clear instance %s on %s: %w", reference.Path, node, err)
	}
	switch {
	case slices.Contains(failedMonitorStates, previous.MonitorState):
	case previous.MonitorState == "idle":
		return InstanceClearResult{}, fmt.Errorf("clear instance %s on %s: nothing to clear: its monitor state is idle", reference.Path, node)
	default:
		return InstanceClearResult{}, fmt.Errorf("clear instance %s on %s: its monitor state is %q, not a failure: an action is running or ending, and a clear would set the monitor idle under it", reference.Path, node, previous.MonitorState)
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/instance/path/%s/%s/%s/clear", node, reference.Namespace, reference.Kind, reference.Name)
	// The daemon answers 200 with a null body.
	if err := poster.PostJSON(ctx, endpoint, nil, nil, nil); err != nil {
		return InstanceClearResult{}, fmt.Errorf("clear instance %s on %s: %w", reference.Path, node, err)
	}
	result := InstanceClearResult{Object: reference, Node: node, PreviousState: previous.MonitorState, CurrentState: previous.MonitorState}
	waitCtx, cancel := context.WithTimeout(ctx, clearInstanceWait)
	defer cancel()
	for attempt := 0; !result.Cleared; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return InstanceClearResult{}, ctx.Err()
			}
			result.Provenance = s.newProvenance()
			return result, nil
		case <-timer.C:
		}
		current, err := s.getExactObjectInstance(waitCtx, reference.Path, node)
		if err != nil {
			if ctx.Err() != nil {
				return InstanceClearResult{}, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				break
			}
			return InstanceClearResult{}, fmt.Errorf("clear instance %s on %s: read it again: %w", reference.Path, node, err)
		}
		result.CurrentState = current.MonitorState
		result.Cleared = current.MonitorState != previous.MonitorState
	}
	result.Provenance = s.newProvenance()
	return result, nil
}
