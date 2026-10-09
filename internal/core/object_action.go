package core

import (
	"context"
	"fmt"
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
)

// objectActionKinds are the object kinds each action accepts, as the daemon
// API declares them.
var objectActionKinds = map[ObjectAction][]string{
	ObjectActionFreeze:   {"svc", "vol"},
	ObjectActionUnfreeze: {"svc", "vol"},
	ObjectActionAbort:    {"svc", "vol", "cfg", "sec", "usr", "nscfg"},
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
func (s *Service) SubmitObjectAction(ctx context.Context, path string, action ObjectAction) (ObjectActionResult, error) {
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
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return ObjectActionResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}

	endpoint := fmt.Sprintf("/api/object/path/%s/%s/%s/action/%s", reference.Namespace, reference.Kind, reference.Name, action)
	var queued daemonOrchestrationQueued
	if err := poster.PostJSON(ctx, endpoint, nil, nil, &queued); err != nil {
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
