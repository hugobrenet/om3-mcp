package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// ObjectAction is an orchestrated action on one object. The daemon records
// the target state and its monitors reach it on every node of the object.
type ObjectAction string

const (
	ObjectActionFreeze   ObjectAction = "freeze"
	ObjectActionUnfreeze ObjectAction = "unfreeze"
	ObjectActionAbort    ObjectAction = "abort"
	ObjectActionStart    ObjectAction = "start"
	ObjectActionStop     ObjectAction = "stop"
	ObjectActionRestart  ObjectAction = "restart"
	ObjectActionSwitch   ObjectAction = "switch"
	ObjectActionGiveback ObjectAction = "giveback"
)

// objectActionKinds are the object kinds each action accepts, as the daemon
// API declares them.
var objectActionKinds = map[ObjectAction][]string{
	ObjectActionFreeze:   {"svc", "vol"},
	ObjectActionUnfreeze: {"svc", "vol"},
	ObjectActionAbort:    {"svc", "vol", "cfg", "sec", "usr", "nscfg"},
	ObjectActionStart:    {"svc"},
	ObjectActionStop:     {"svc"},
	ObjectActionRestart:  {"svc"},
	ObjectActionSwitch:   {"svc"},
	ObjectActionGiveback: {"svc"},
}

// ObjectActionOptions are the options of the actions that take some.
type ObjectActionOptions struct {
	// InterruptSyncs has a stop or a switch end the syncs running on the
	// instances it stops instead of waiting for them to end.
	InterruptSyncs bool
	// To is the one node a switch moves the object to. Empty, the daemon
	// selects it.
	To string
	// Live has a switch migrate the object live when its resources allow it.
	Live bool
}

// daemonSwitchRequest is the body of a switch. The daemon takes a list of
// destinations, which it reads as a node selector: the MCP sends at most one
// exact node.
type daemonSwitchRequest struct {
	Destination    []string `json:"destination,omitempty"`
	Live           bool     `json:"live"`
	InterruptSyncs bool     `json:"interrupt_syncs,omitempty"`
}

type ObjectActionResult struct {
	Provenance      Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object          ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference the action was submitted for"`
	Action          string                 `json:"action" jsonschema:"the action submitted"`
	OrchestrationID string                 `json:"orchestration_id" jsonschema:"the identifier of the orchestration the daemon queued; pass it to list_daemon_orchestrations and get_node_logs to follow it"`
}

type daemonOrchestrationQueued struct {
	OrchestrationID string `json:"orchestration_id"`
}

// SubmitObjectAction submits one orchestrated action on one exact object and
// returns as soon as the daemon queued it. It does not wait for the object to
// reach the target state.
func (s *Service) SubmitObjectAction(ctx context.Context, path string, action ObjectAction, options ObjectActionOptions) (ObjectActionResult, error) {
	kinds, known := objectActionKinds[action]
	if !known {
		return ObjectActionResult{}, fmt.Errorf("unsupported object action %q", action)
	}
	if strings.ContainsAny(path, "*?[],\\") {
		return ObjectActionResult{}, fmt.Errorf("path must be one exact OpenSVC object path, without wildcard or selector")
	}
	reference, err := validateExactObjectPath(path)
	if err != nil {
		return ObjectActionResult{}, err
	}
	if !slices.Contains(kinds, reference.Kind) {
		return ObjectActionResult{}, fmt.Errorf("the %s action applies to %s objects only", action, strings.Join(kinds, ", "))
	}
	if options.InterruptSyncs && action != ObjectActionStop && action != ObjectActionSwitch {
		return ObjectActionResult{}, fmt.Errorf("interrupt_syncs applies to the stop and switch actions only")
	}
	if (options.To != "" || options.Live) && action != ObjectActionSwitch {
		return ObjectActionResult{}, fmt.Errorf("to and live apply to the switch action only")
	}
	var query url.Values
	var body any
	switch action {
	case ObjectActionStop:
		if options.InterruptSyncs {
			query = url.Values{"interrupt_syncs": {"true"}}
		}
	case ObjectActionSwitch:
		request := daemonSwitchRequest{Live: options.Live, InterruptSyncs: options.InterruptSyncs}
		if options.To != "" {
			if err := validateNodeTarget(options.To); err != nil {
				return ObjectActionResult{}, fmt.Errorf("to: %w", err)
			}
			request.Destination = []string{options.To}
		}
		body = request
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return ObjectActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}

	endpoint := fmt.Sprintf("/api/object/path/%s/%s/%s/action/%s", reference.Namespace, reference.Kind, reference.Name, action)
	var queued daemonOrchestrationQueued
	if err := poster.PostJSON(ctx, endpoint, query, body, &queued); err != nil {
		return ObjectActionResult{}, fmt.Errorf("%s object %s: %w", action, reference.Path, err)
	}
	if !daemonExecutionUUIDPattern.MatchString(queued.OrchestrationID) {
		return ObjectActionResult{}, fmt.Errorf("%s object %s: daemon returned no valid orchestration id", action, reference.Path)
	}
	return ObjectActionResult{
		Provenance:      s.newProvenance(),
		Object:          reference,
		Action:          string(action),
		OrchestrationID: strings.ToLower(queued.OrchestrationID),
	}, nil
}
