package core

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

// OpenSVC refreshes heartbeat status at most every 60 seconds. Allow time for
// cluster propagation and clock skew before treating a published view as old.
const maxClusterHeartbeatAge = 3 * time.Minute
const maxClusterHeartbeatFutureSkew = 30 * time.Second
const maxClusterHeartbeatIssues = 50
const maxClusterHeartbeatIssueValueRunes = 255

type ClusterNodeHeartbeatHealth struct {
	State           string                  `json:"state" jsonschema:"healthy, degraded, unknown, or not_applicable; describes the published heartbeat view, not node reachability"`
	UpdatedAt       string                  `json:"updated_at" jsonschema:"OpenSVC heartbeat publication time, or empty when missing or invalid"`
	StreamsTotal    int                     `json:"streams_total" jsonschema:"number of heartbeat streams in the published node view"`
	StreamsRunning  int                     `json:"streams_running" jsonschema:"number of streams reporting running"`
	LinksTotal      int                     `json:"links_total" jsonschema:"number of reported stream and peer links"`
	LinksBeating    int                     `json:"links_beating" jsonschema:"number of reported links with is_beating true"`
	LinksNotBeating int                     `json:"links_not_beating" jsonschema:"number of reported links with is_beating false"`
	RXPeersBeating  int                     `json:"rx_peers_beating" jsonschema:"configured peers with at least one beating RX link"`
	RXPeersStale    int                     `json:"rx_peers_stale" jsonschema:"configured peers with RX links but none beating, from this node's view"`
	IssuesTotal     int                     `json:"issues_total" jsonschema:"total heartbeat issues found before limiting the issues list"`
	Issues          []ClusterHeartbeatIssue `json:"issues" jsonschema:"heartbeat issues with stream and peer evidence, limited to 50"`
	IssuesTruncated bool                    `json:"issues_truncated" jsonschema:"whether heartbeat issues were omitted after the 50-entry limit"`
}

type ClusterHeartbeatIssue struct {
	Code          string `json:"code" jsonschema:"stable machine-readable heartbeat issue code"`
	Message       string `json:"message" jsonschema:"concise explanation of the observed heartbeat condition"`
	StreamID      string `json:"stream_id,omitempty" jsonschema:"heartbeat stream identifier when relevant"`
	Peer          string `json:"peer,omitempty" jsonschema:"peer node name when relevant"`
	StreamState   string `json:"stream_state,omitempty" jsonschema:"reported stream state when relevant"`
	ChangedAt     string `json:"changed_at,omitempty" jsonschema:"time when this peer's is_beating value last changed"`
	LastBeatingAt string `json:"last_beating_at,omitempty" jsonschema:"time when this peer was last observed beating"`
}

func (h *ClusterNodeHeartbeatHealth) addIssue(issue ClusterHeartbeatIssue) {
	h.IssuesTotal++
	if len(h.Issues) < maxClusterHeartbeatIssues {
		h.Issues = append(h.Issues, issue)
	} else {
		h.IssuesTruncated = true
	}
}

func clusterHeartbeatHealth(heartbeat *clusterHeartbeat, nodeName string, configuredNodes []string, now time.Time) ClusterNodeHeartbeatHealth {
	health := ClusterNodeHeartbeatHealth{State: "unknown", Issues: []ClusterHeartbeatIssue{}}
	if heartbeat == nil {
		if len(configuredNodes) == 1 {
			health.State = "not_applicable"
			return health
		}
		health.addIssue(ClusterHeartbeatIssue{Code: "heartbeat_status_missing", Message: "node has no published heartbeat status"})
		return health
	}

	health.StreamsTotal = len(heartbeat.Streams)
	if len(configuredNodes) == 1 && len(heartbeat.Streams) == 0 {
		health.State = "not_applicable"
		return health
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, heartbeat.UpdatedAt)
	if err != nil || updatedAt.IsZero() {
		health.addIssue(ClusterHeartbeatIssue{Code: "heartbeat_timestamp_missing", Message: "heartbeat publication time is missing or invalid"})
		return health
	}
	health.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if now.Sub(updatedAt) > maxClusterHeartbeatAge {
		health.addIssue(ClusterHeartbeatIssue{Code: "heartbeat_status_stale", Message: "heartbeat status was published more than three minutes ago"})
		return health
	}
	if updatedAt.Sub(now) > maxClusterHeartbeatFutureSkew {
		health.addIssue(ClusterHeartbeatIssue{Code: "heartbeat_timestamp_in_future", Message: "heartbeat publication time is more than thirty seconds in the future"})
		return health
	}

	streams := append([]clusterHeartbeatStream(nil), heartbeat.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streams[i].ID < streams[j].ID })
	rxSeen := make(map[string]bool)
	rxBeating := make(map[string]bool)
	degraded := false
	for _, stream := range streams {
		if stream.State == "running" {
			health.StreamsRunning++
		} else {
			degraded = true
			health.addIssue(ClusterHeartbeatIssue{
				Code:        "heartbeat_stream_not_running",
				Message:     "heartbeat stream is not running",
				StreamID:    boundedHeartbeatIssueValue(stream.ID),
				StreamState: boundedHeartbeatIssueValue(stream.State),
			})
		}
		peers := make([]string, 0, len(stream.Peers))
		for peerName := range stream.Peers {
			peers = append(peers, peerName)
		}
		sort.Strings(peers)
		for _, peerName := range peers {
			peer := stream.Peers[peerName]
			health.LinksTotal++
			if strings.HasSuffix(stream.ID, ".rx") {
				rxSeen[peerName] = true
			}
			if peer.IsBeating {
				health.LinksBeating++
				if strings.HasSuffix(stream.ID, ".rx") {
					rxBeating[peerName] = true
				}
				continue
			}
			degraded = true
			health.LinksNotBeating++
			health.addIssue(ClusterHeartbeatIssue{
				Code:          "heartbeat_peer_not_beating",
				Message:       "heartbeat stream peer is not beating",
				StreamID:      boundedHeartbeatIssueValue(stream.ID),
				Peer:          boundedHeartbeatIssueValue(peerName),
				ChangedAt:     validHeartbeatTime(peer.ChangedAt),
				LastBeatingAt: validHeartbeatTime(peer.LastBeatingAt),
			})
		}
	}

	missingRX := false
	for _, peerName := range configuredNodes {
		if peerName == nodeName {
			continue
		}
		if !rxSeen[peerName] {
			missingRX = true
			health.addIssue(ClusterHeartbeatIssue{
				Code:    "heartbeat_rx_peer_missing",
				Message: "configured peer has no reported RX heartbeat link",
				Peer:    boundedHeartbeatIssueValue(peerName),
			})
		} else if rxBeating[peerName] {
			health.RXPeersBeating++
		} else {
			degraded = true
			health.RXPeersStale++
			health.addIssue(ClusterHeartbeatIssue{
				Code:    "heartbeat_peer_stale",
				Message: "no RX heartbeat link is beating for this configured peer",
				Peer:    boundedHeartbeatIssueValue(peerName),
			})
		}
	}
	switch {
	case degraded:
		health.State = "degraded"
	case missingRX:
		health.State = "unknown"
	default:
		health.State = "healthy"
	}
	return health
}

func validHeartbeatTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func boundedHeartbeatIssueValue(value string) string {
	var b strings.Builder
	count := 0
	for _, r := range value {
		if count >= maxClusterHeartbeatIssueValueRunes {
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
