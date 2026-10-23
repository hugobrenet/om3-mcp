package core

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// daemonLogLevels are the levels the daemon log is emitted at: it writes to
// journald, which is given nothing below info.
var daemonLogLevels = []string{"info", "warn", "error", "none"}

// heartbeatName is a heartbeat, hb#1, or one of its streams, hb#1.rx, with
// or without the hb# prefix.
var heartbeatName = regexp.MustCompile(`^(hb#)?[A-Za-z0-9_-]{1,64}(\.(rx|tx))?$`)

// daemonActionQueued is the problem body the daemon answers a queued
// subsystem action with, at 200.
type daemonActionQueued struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type DaemonLogLevel struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       string     `json:"node" jsonschema:"the node whose daemon was read or set"`
	Level      string     `json:"level" jsonschema:"the level below which the daemon log is not emitted: info, warn, error or none"`
	Previous   string     `json:"previous,omitempty" jsonschema:"for a change, the level read before it"`
}

type daemonLogControl struct {
	Level string `json:"level"`
}

// GetDaemonLogLevel reads the level the daemon log of one node is emitted at.
func (s *Service) GetDaemonLogLevel(ctx context.Context, node string) (DaemonLogLevel, error) {
	if err := validateNodeTarget(node); err != nil {
		return DaemonLogLevel{}, err
	}
	var response daemonLogControl
	if err := s.client.GetJSON(ctx, fmt.Sprintf("/api/node/name/%s/daemon/log/control", node), nil, &response); err != nil {
		return DaemonLogLevel{}, fmt.Errorf("get daemon log level of %s: %w", node, err)
	}
	return DaemonLogLevel{Provenance: s.newProvenance(), Node: node, Level: response.Level}, nil
}

// SetDaemonLogLevel sets the level the daemon log of one node is emitted at,
// until the daemon restarts, which takes the level of its configuration.
func (s *Service) SetDaemonLogLevel(ctx context.Context, node string, level string) (DaemonLogLevel, error) {
	if err := validateNodeTarget(node); err != nil {
		return DaemonLogLevel{}, err
	}
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "debug" || level == "trace" {
		return DaemonLogLevel{}, fmt.Errorf("the daemon log is not emitted below the info level: a %s feed is the daemon audit, which this MCP does not offer", level)
	}
	if !slices.Contains(daemonLogLevels, level) {
		return DaemonLogLevel{}, fmt.Errorf("level must be one of %s", strings.Join(daemonLogLevels, ", "))
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return DaemonLogLevel{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	before, err := s.GetDaemonLogLevel(ctx, node)
	if err != nil {
		return DaemonLogLevel{}, err
	}
	var response daemonLogControl
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/daemon/log/control", node), nil, daemonLogControl{Level: level}, &response); err != nil {
		return DaemonLogLevel{}, fmt.Errorf("set daemon log level of %s: %w", node, err)
	}
	return DaemonLogLevel{Provenance: s.newProvenance(), Node: node, Level: response.Level, Previous: before.Level}, nil
}

// HeartbeatAction is an action on the heartbeat streams of one node. A stop
// is not offered: stopping the last stream a node sends on cuts what it
// publishes to its peers, the view any check of the client would read, and a
// peer that no longer hears a node declares it dead and takes its services
// over, or, with an arbitrator, may stop the services of the minority side.
type HeartbeatAction string

const (
	HeartbeatStart   HeartbeatAction = "start"
	HeartbeatRestart HeartbeatAction = "restart"
)

type HeartbeatActionResult struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       string     `json:"node" jsonschema:"the node whose heartbeat streams were acted on"`
	Action     string     `json:"action" jsonschema:"start or restart"`
	// Streams are the streams the action addresses.
	Streams   []string `json:"streams" jsonschema:"the streams the action addresses: both of a heartbeat, hb#1.rx and hb#1.tx, or the one named"`
	Queued    string   `json:"queued,omitempty" jsonschema:"what the daemon queued, as it says it"`
	Completed bool     `json:"completed" jsonschema:"whether the streams were seen running within the wait, a restarted one registered again; seen from the peer the MCP talks to, the view of the node arriving late"`
	// States are the streams addressed, read again at the end of the wait.
	States      []HeartbeatStreamState `json:"states" jsonschema:"the streams addressed still published, read again at the end of the wait"`
	Absent      []string               `json:"absent,omitempty" jsonschema:"the streams addressed not published"`
	StatusError string                 `json:"status_error,omitempty" jsonschema:"why the node could not be read again at the end of the wait; the action is queued all the same"`
}

type HeartbeatStreamState struct {
	ID        string          `json:"id" jsonschema:"the stream, such as hb#1.rx"`
	State     string          `json:"state" jsonschema:"running, stopped or failed"`
	UpdatedAt string          `json:"updated_at" jsonschema:"when the stream last changed"`
	Peers     []HeartbeatBeat `json:"peers" jsonschema:"the peers of the stream and whether each beats"`
}

type HeartbeatBeat struct {
	Name      string `json:"name" jsonschema:"the peer node"`
	IsBeating bool   `json:"is_beating" jsonschema:"whether the peer beats on this stream"`
}

// RunHeartbeatAction starts or restarts the heartbeat streams of one node:
// the daemon queues the action, at once, and its janitor runs it. The MCP
// reads the streams again a bounded time, in the cluster status the daemon it
// talks to serves: no route gives the heartbeats as the node reads them.
func (s *Service) RunHeartbeatAction(ctx context.Context, node string, name string, action HeartbeatAction, wait time.Duration) (HeartbeatActionResult, error) {
	if action != HeartbeatStart && action != HeartbeatRestart {
		return HeartbeatActionResult{}, fmt.Errorf("unsupported heartbeat action %q", action)
	}
	if err := validateNodeTarget(node); err != nil {
		return HeartbeatActionResult{}, err
	}
	if !heartbeatName.MatchString(name) {
		return HeartbeatActionResult{}, fmt.Errorf("name must be a heartbeat, such as hb#1, or one of its streams, such as hb#1.rx")
	}
	wait, err := validateActionWait(wait)
	if err != nil {
		return HeartbeatActionResult{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return HeartbeatActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	section := "hb#" + strings.TrimPrefix(name, "hb#")
	streams := []string{section}
	if !strings.HasSuffix(section, ".rx") && !strings.HasSuffix(section, ".tx") {
		streams = []string{section + ".rx", section + ".tx"}
	}
	before, err := s.GetNodeStatus(ctx, node)
	if err != nil {
		return HeartbeatActionResult{}, fmt.Errorf("%s heartbeat %s of %s: %w", action, name, node, err)
	}
	var queued daemonActionQueued
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/daemon/hb/name/%s/action/%s", node, section, action), nil, nil, &queued); err != nil {
		return HeartbeatActionResult{}, fmt.Errorf("%s heartbeat %s of %s: %w", action, name, node, err)
	}
	// A stream restarted is registered again: its creation time is new,
	// where its update time moves too when a peer changes.
	previous := map[string]string{}
	for _, stream := range heartbeatStreams(before) {
		previous[stream.ID] = componentStart(stream.CreatedAt, stream.UpdatedAt)
	}
	result := HeartbeatActionResult{Node: node, Action: string(action), Streams: streams, Queued: queued.Detail, States: []HeartbeatStreamState{}}
	done := func(status NodeStatus) bool {
		published := map[string]HeartbeatStream{}
		for _, stream := range heartbeatStreams(status) {
			published[stream.ID] = stream
		}
		for _, id := range streams {
			stream, ok := published[id]
			if !ok || stream.State != "running" || (action == HeartbeatRestart && componentStart(stream.CreatedAt, stream.UpdatedAt) == previous[id]) {
				return false
			}
		}
		return true
	}
	status, completed, readErr := s.pollNodeStatus(ctx, node, wait, done)
	if readErr != nil && !completed {
		result.StatusError = readErr.Error()
	}
	if status != nil {
		published := map[string]HeartbeatStream{}
		for _, stream := range heartbeatStreams(*status) {
			published[stream.ID] = stream
		}
		for _, id := range streams {
			stream, ok := published[id]
			if !ok {
				result.Absent = append(result.Absent, id)
				continue
			}
			state := HeartbeatStreamState{ID: id, State: stream.State, UpdatedAt: stream.UpdatedAt, Peers: []HeartbeatBeat{}}
			for _, peer := range stream.Peers.Items {
				state.Peers = append(state.Peers, HeartbeatBeat{Name: peer.Name, IsBeating: peer.IsBeating})
			}
			result.States = append(result.States, state)
		}
	}
	result.Completed = completed
	result.Provenance = s.newProvenance()
	return result, nil
}

func heartbeatStreams(status NodeStatus) []HeartbeatStream {
	if status.Heartbeat == nil {
		return nil
	}
	return status.Heartbeat.Streams.Items
}

type DaemonListenerRestart struct {
	Provenance  Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node        string     `json:"node" jsonschema:"the node whose listener api.inet was restarted"`
	Queued      string     `json:"queued,omitempty" jsonschema:"what the daemon queued, as it says it"`
	Completed   bool       `json:"completed" jsonschema:"whether the listener was seen running again, its creation or update time moved, within the wait"`
	State       string     `json:"state,omitempty" jsonschema:"the listener state read last"`
	UpdatedAt   string     `json:"updated_at,omitempty" jsonschema:"when the listener last changed"`
	Address     string     `json:"address,omitempty" jsonschema:"the address the listener is bound to"`
	Port        string     `json:"port,omitempty" jsonschema:"the port the listener is bound to"`
	StatusError string     `json:"status_error,omitempty" jsonschema:"why the node could not be read again, such as while its API restarted; the restart is queued all the same"`
	// AnswerError is the error the request got, the listener being seen
	// restarted all the same.
	AnswerError string `json:"answer_error,omitempty" jsonschema:"the error the request got, such as an EOF relayed in a 500, when the listener closed the connection while restarting before its answer left: the listener was seen restarted all the same"`
}

// RestartDaemonListener restarts the HTTPS listener of the daemon of one
// node, api.inet, the only one with a life cycle: the API of the node is cut
// while it rebinds.
func (s *Service) RestartDaemonListener(ctx context.Context, node string, wait time.Duration) (DaemonListenerRestart, error) {
	if err := validateNodeTarget(node); err != nil {
		return DaemonListenerRestart{}, err
	}
	wait, err := validateActionWait(wait)
	if err != nil {
		return DaemonListenerRestart{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return DaemonListenerRestart{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	before, err := s.GetNodeStatus(ctx, node)
	if err != nil {
		return DaemonListenerRestart{}, fmt.Errorf("restart the listener of %s: %w", node, err)
	}
	// The listener status is built once, with the component: a restart
	// moves its update time, not always its creation time.
	var previousCreated, previousUpdated string
	if listener := before.Daemon.Subsystems.Listener; listener != nil {
		previousCreated, previousUpdated = listener.CreatedAt, listener.UpdatedAt
	}
	// The listener may close the connection while it restarts, before its
	// answer leaves: the request then fails, relayed as an EOF in a 500, the
	// restart done. The listener is read again all the same.
	var queued daemonActionQueued
	postErr := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/daemon/listener/name/api.inet/action/restart", node), nil, nil, &queued)
	result := DaemonListenerRestart{Node: node, Queued: queued.Detail}
	status, completed, readErr := s.pollNodeStatus(ctx, node, wait, func(status NodeStatus) bool {
		listener := status.Daemon.Subsystems.Listener
		return listener != nil && listener.State == "running" && (listener.CreatedAt != previousCreated || listener.UpdatedAt != previousUpdated)
	})
	if postErr != nil {
		if !completed {
			return DaemonListenerRestart{}, fmt.Errorf("restart the listener of %s: %w", node, postErr)
		}
		result.AnswerError = postErr.Error()
	}
	if readErr != nil && !completed {
		result.StatusError = readErr.Error()
	}
	if status != nil && status.Daemon.Subsystems.Listener != nil {
		listener := status.Daemon.Subsystems.Listener
		result.State, result.UpdatedAt, result.Address, result.Port = listener.State, listener.UpdatedAt, listener.Address, listener.Port
	}
	result.Completed = completed
	result.Provenance = s.newProvenance()
	return result, nil
}

type DaemonRestart struct {
	Provenance        Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node              string     `json:"node" jsonschema:"the node whose daemon was restarted"`
	Queued            string     `json:"queued,omitempty" jsonschema:"what the daemon answered, as it says it"`
	PreviousPID       int        `json:"previous_pid" jsonschema:"the daemon pid read before the restart"`
	PreviousStartedAt string     `json:"previous_started_at" jsonschema:"when the daemon started, read before the restart"`
	Restarted         bool       `json:"restarted" jsonschema:"whether a daemon started after the request was seen within the wait"`
	PID               int        `json:"pid,omitempty" jsonschema:"the daemon pid read last"`
	StartedAt         string     `json:"started_at,omitempty" jsonschema:"when the daemon read last started"`
	StatusError       string     `json:"status_error,omitempty" jsonschema:"why the node could not be read again before the end of the wait; the restart is launched all the same"`
}

// RestartDaemon restarts the daemon of one node, in the background: the
// instances keep their state, and the peers, told of the maintenance, take
// nothing over. The MCP reads the node again until a daemon started after
// the request answers, a bounded time.
func (s *Service) RestartDaemon(ctx context.Context, node string, wait time.Duration) (DaemonRestart, error) {
	if err := validateNodeTarget(node); err != nil {
		return DaemonRestart{}, err
	}
	wait, err := validateActionWait(wait)
	if err != nil {
		return DaemonRestart{}, err
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return DaemonRestart{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	before, err := s.GetNodeStatus(ctx, node)
	if err != nil {
		return DaemonRestart{}, fmt.Errorf("restart the daemon of %s: %w", node, err)
	}
	var queued daemonActionQueued
	if err := poster.PostJSON(ctx, fmt.Sprintf("/api/node/name/%s/daemon/action/restart", node), nil, nil, &queued); err != nil {
		return DaemonRestart{}, fmt.Errorf("restart the daemon of %s: %w", node, err)
	}
	result := DaemonRestart{Node: node, Queued: queued.Title, PreviousPID: before.Daemon.PID, PreviousStartedAt: before.Daemon.StartedAt}
	status, restarted, readErr := s.pollNodeStatus(ctx, node, wait, func(status NodeStatus) bool {
		return status.Daemon.StartedAt != "" && status.Daemon.StartedAt != before.Daemon.StartedAt
	})
	if readErr != nil && !restarted {
		result.StatusError = readErr.Error()
	}
	if status != nil {
		result.PID, result.StartedAt = status.Daemon.PID, status.Daemon.StartedAt
	}
	result.Restarted = restarted
	result.Provenance = s.newProvenance()
	return result, nil
}

// componentStart is the time a daemon component was registered, its
// creation time, or its update time for a daemon that gives none: a
// component restarted is registered again.
func componentStart(createdAt string, updatedAt string) string {
	if createdAt != "" {
		return createdAt
	}
	return updatedAt
}

// validateActionWait checks the wait of a daemon action; zero, 10 seconds.
func validateActionWait(wait time.Duration) (time.Duration, error) {
	if wait == 0 {
		wait = defaultInstanceActionWait
	}
	if wait < time.Second || wait > maxInstanceActionWait {
		return 0, fmt.Errorf("wait must be between 1 and %d seconds", int(maxInstanceActionWait.Seconds()))
	}
	return wait, nil
}

// pollNodeStatus reads the status of a node until done says so or the wait
// expires. A read that fails, such as while the API of the node restarts,
// does not end the wait: the last error is returned with the last status
// read, if any.
func (s *Service) pollNodeStatus(ctx context.Context, node string, wait time.Duration, done func(NodeStatus) bool) (*NodeStatus, bool, error) {
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var last *NodeStatus
	var lastErr error
	for attempt := 0; ; attempt++ {
		timer := time.NewTimer(refreshPollDelay(attempt))
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return last, false, lastErr
		case <-timer.C:
		}
		status, err := s.GetNodeStatus(waitCtx, node)
		if err != nil {
			if ctx.Err() != nil {
				return last, false, ctx.Err()
			}
			lastErr = err
			continue
		}
		last, lastErr = &status, nil
		if done(status) {
			return last, true, nil
		}
	}
}
