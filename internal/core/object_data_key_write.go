package core

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// dataPropagationExpired reads the 408 of a data write whose wait expired:
// the keys committed, and the nodes they had not reached. The daemon says
// "have not reached" there, "has not reached" for a configuration write:
// both are read, should it align them.
var dataPropagationExpired = regexp.MustCompile(`committed as of (\S+), and ha(?:s|ve) not reached (.+) before the wait expired`)

var dataKeyKinds = []string{"cfg", "sec", "usr"}

type SetObjectDataKeyOptions struct {
	Path string
	Name string
	// Value is a text value; ValueBase64 a binary one. One of the two.
	Value       *string
	ValueBase64 *string
	WaitSeconds int
}

type DeleteObjectDataKeyOptions struct {
	Path        string
	Name        string
	WaitSeconds int
}

type ObjectDataKeyWrite struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Name       string                 `json:"name" jsonschema:"the key name"`
	Outcome    string                 `json:"outcome" jsonschema:"set, the key added or its value replaced, or deleted"`
	// Existed is read on the list of the keys before the write: it tells,
	// it does not guard.
	Existed            bool     `json:"existed" jsonschema:"whether the key existed before the write: a set replaced its value, a delete of a key absent changed nothing"`
	SizeBytes          int      `json:"size_bytes,omitempty" jsonschema:"for a set, the size of the value written, in bytes; the value is never returned"`
	LastModified       string   `json:"last_modified,omitempty" jsonschema:"the time the daemon gives the configuration written, OM-Last-Modified"`
	PropagationPending []string `json:"propagation_pending,omitempty" jsonschema:"with wait_seconds, the nodes the keys had not reached when the wait expired; the write is done"`
}

type daemonDataKeyPatch struct {
	Action string  `json:"action"`
	Name   string  `json:"name"`
	String *string `json:"string,omitempty"`
	Bytes  []byte  `json:"bytes,omitempty"`
}

// SetObjectDataKey adds a key to a cfg, sec or usr object, or replaces its
// value: the change operation of the daemon is an upsert.
func (s *Service) SetObjectDataKey(ctx context.Context, options SetObjectDataKeyOptions) (ObjectDataKeyWrite, error) {
	reference, err := validateDataKeyTarget(options.Path, options.Name, dataKeyKinds)
	if err != nil {
		return ObjectDataKeyWrite{}, err
	}
	patch := daemonDataKeyPatch{Action: "change", Name: options.Name}
	size := 0
	switch {
	case (options.Value == nil) == (options.ValueBase64 == nil):
		return ObjectDataKeyWrite{}, fmt.Errorf("pass one of value, a text, and value_base64, binary bytes")
	case options.Value != nil:
		size = len(*options.Value)
		value := *options.Value
		patch.String = &value
	default:
		decoded, err := base64.StdEncoding.DecodeString(*options.ValueBase64)
		if err != nil {
			return ObjectDataKeyWrite{}, fmt.Errorf("value_base64 must be standard base64: %v", err)
		}
		size = len(decoded)
		// An empty slice would be left out of the body.
		if len(decoded) == 0 {
			empty := ""
			patch.String = &empty
		} else {
			patch.Bytes = decoded
		}
	}
	if size > maxObjectDataValueBytes {
		return ObjectDataKeyWrite{}, fmt.Errorf("the value exceeds %d bytes", maxObjectDataValueBytes)
	}
	result, err := s.patchObjectData(ctx, reference, patch, options.WaitSeconds)
	if err != nil {
		return ObjectDataKeyWrite{}, err
	}
	result.Outcome, result.SizeBytes = "set", size
	return result, nil
}

// DeleteObjectDataKey removes a key from a cfg, sec or usr object. The
// daemon does not refuse a key absent: existed tells.
func (s *Service) DeleteObjectDataKey(ctx context.Context, options DeleteObjectDataKeyOptions) (ObjectDataKeyWrite, error) {
	reference, err := validateDataKeyTarget(options.Path, options.Name, dataKeyKinds)
	if err != nil {
		return ObjectDataKeyWrite{}, err
	}
	result, err := s.patchObjectData(ctx, reference, daemonDataKeyPatch{Action: "remove", Name: options.Name}, options.WaitSeconds)
	if err != nil {
		return ObjectDataKeyWrite{}, err
	}
	result.Outcome = "deleted"
	return result, nil
}

// patchObjectData reads whether the key exists, then sends one operation:
// a write the daemon answers with the time of the configuration, and holds
// until it reached every node when asked to wait.
func (s *Service) patchObjectData(ctx context.Context, reference ClusterObjectReference, patch daemonDataKeyPatch, waitSeconds int) (ObjectDataKeyWrite, error) {
	if waitSeconds < 0 || waitSeconds > maxConfigWaitSeconds {
		return ObjectDataKeyWrite{}, fmt.Errorf("wait_seconds must be between 0 and %d", maxConfigWaitSeconds)
	}
	patcher, ok := s.client.(JSONBodyPatcher)
	if !ok {
		return ObjectDataKeyWrite{}, fmt.Errorf("OpenSVC daemon client does not support PATCH requests with a body")
	}
	keys, err := s.ListObjectDataKeys(ctx, ListObjectDataKeysOptions{Path: reference.Path, Name: patch.Name})
	if err != nil {
		return ObjectDataKeyWrite{}, err
	}
	query := url.Values{}
	if waitSeconds > 0 {
		query.Set("wait", fmt.Sprintf("%ds", waitSeconds))
	}
	endpoint := fmt.Sprintf("/api/object/path/%s/%s/%s/data", reference.Namespace, reference.Kind, reference.Name)
	header, err := patcher.PatchJSONBody(ctx, endpoint, query, []daemonDataKeyPatch{patch}, nil)
	result := ObjectDataKeyWrite{Object: reference, Name: patch.Name, Existed: keys.Total > 0}
	if err != nil {
		// A wait that expired is no failure of the write: the keys are
		// committed, and still on their way to the nodes the daemon names.
		var problem daemonProblem
		if !errors.As(err, &problem) || problem.HTTPStatus() != http.StatusRequestTimeout {
			return ObjectDataKeyWrite{}, fmt.Errorf("%s data key %s of %s: %w", patch.Action, patch.Name, reference.Path, err)
		}
		match := dataPropagationExpired.FindStringSubmatch(problem.ProblemDetail())
		if match == nil {
			return ObjectDataKeyWrite{}, fmt.Errorf("%s data key %s of %s: %w", patch.Action, patch.Name, reference.Path, err)
		}
		result.LastModified = match[1]
		result.PropagationPending = strings.Split(match[2], ", ")
	}
	if modified := header.Get(configLastModifiedHeader); modified != "" {
		result.LastModified = modified
	}
	result.Provenance = s.newProvenance()
	return result, nil
}
