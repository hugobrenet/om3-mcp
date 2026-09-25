package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type NodeReachabilityProbe struct {
	Provenance  Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node        string     `json:"node" jsonschema:"exact requested OpenSVC node name or the underscore alias when explicitly requested"`
	Reachable   bool       `json:"reachable" jsonschema:"whether the target daemon returned the expected HTTP 204 response through the OpenSVC proxy path; always true in a successful result"`
	StatusCode  int        `json:"status_code" jsonschema:"HTTP status returned by the target daemon through the OpenSVC proxy path; 204 in a successful result"`
	RoundTripMS float64    `json:"round_trip_ms" jsonschema:"elapsed milliseconds for the complete MCP to local daemon to optional remote daemon request and response path"`
}

func (s *Service) ProbeNodeReachability(ctx context.Context, node string) (NodeReachabilityProbe, error) {
	if node == "" || (node != localDaemonNodeAlias && !validExactNodeName(node)) {
		return NodeReachabilityProbe{}, fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}
	getter, ok := s.client.(NoContentGetter)
	if !ok {
		return NodeReachabilityProbe{}, fmt.Errorf("probe node reachability: daemon client does not support no-content requests")
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/ping", node)
	startedAt := time.Now()
	if err := getter.GetNoContent(ctx, endpoint, url.Values{}); err != nil {
		return NodeReachabilityProbe{}, fmt.Errorf("probe node reachability: %w", err)
	}
	return NodeReachabilityProbe{
		Provenance:  s.newProvenance(),
		Node:        node,
		Reachable:   true,
		StatusCode:  http.StatusNoContent,
		RoundTripMS: float64(time.Since(startedAt).Nanoseconds()) / float64(time.Millisecond),
	}, nil
}
