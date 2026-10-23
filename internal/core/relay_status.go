package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	maxRelayStatuses  = 1000
	maxRelayFieldText = 512
)

type RelayStatus struct {
	Relay       string `json:"relay,omitempty" jsonschema:"for a local read, the node relaying the slot; for a remote read, the URL of the relay, its credentials left out"`
	ClusterID   string `json:"cluster_id" jsonschema:"the cluster of the node writing the slot"`
	ClusterName string `json:"cluster_name" jsonschema:"the name of that cluster"`
	Nodename    string `json:"nodename" jsonschema:"the node writing the slot"`
	NodeAddr    string `json:"node_addr" jsonschema:"the address the node writes from"`
	MsgLen      int    `json:"msg_len" jsonschema:"the size of the last heartbeat message of the slot, in bytes; the message itself is never returned"`
	UpdatedAt   string `json:"updated_at" jsonschema:"when the node last wrote the slot: an old time on a relay alive is the sign of a node no longer beating through it"`
	Username    string `json:"username" jsonschema:"the account the node writes the slot with"`
	Status      string `json:"status,omitempty" jsonschema:"for a remote read, why the relay could not be read; empty when it was"`
}

type RelayStatusList struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Remote     bool          `json:"remote" jsonschema:"false, the slots the daemon answering relays for nodes of other clusters; true, the relays this cluster is a client of, read through them"`
	Total      int           `json:"total" jsonschema:"the number of slots the daemon returned"`
	Count      int           `json:"count" jsonschema:"the number of slots returned"`
	Slots      []RelayStatus `json:"slots" jsonschema:"the relay slots, by cluster, node and address"`
	Truncated  bool          `json:"truncated" jsonschema:"whether slots were left out after 1000"`
}

type daemonRelayStatusList struct {
	Items []struct {
		Relay       string `json:"relay"`
		ClusterID   string `json:"cluster_id"`
		ClusterName string `json:"cluster_name"`
		Nodename    string `json:"nodename"`
		NodeAddr    string `json:"node_addr"`
		MsgLen      int    `json:"msg_len"`
		UpdatedAt   string `json:"updated_at"`
		Username    string `json:"username"`
		Status      string `json:"status"`
	} `json:"items"`
}

// GetRelayStatus reads the heartbeat relay slots, as the daemon the MCP talks
// to serves them: the slots it relays for the nodes of other clusters, or,
// remote, the slots of the relays the heartbeats of this cluster go through,
// which the daemon reads from each relay with the credentials of its
// heartbeat. The messages themselves are never read.
func (s *Service) GetRelayStatus(ctx context.Context, remote bool) (RelayStatusList, error) {
	query := url.Values{}
	if remote {
		query.Set("remote", "true")
	}
	var response daemonRelayStatusList
	if err := s.client.GetJSON(ctx, "/api/relay/status", query, &response); err != nil {
		return RelayStatusList{}, fmt.Errorf("get relay status: %w", err)
	}
	slots := make([]RelayStatus, 0, len(response.Items))
	for _, item := range response.Items {
		slot := RelayStatus{
			Relay: relayURLWithoutCredentials(item.Relay), ClusterID: item.ClusterID, MsgLen: item.MsgLen, UpdatedAt: item.UpdatedAt,
		}
		slot.ClusterName, _ = cutUTF8(item.ClusterName, maxRelayFieldText)
		slot.Nodename, _ = cutUTF8(item.Nodename, maxRelayFieldText)
		slot.NodeAddr, _ = cutUTF8(item.NodeAddr, maxRelayFieldText)
		slot.Username, _ = cutUTF8(item.Username, maxRelayFieldText)
		slot.Status, _ = cutUTF8(item.Status, maxRelayFieldText)
		slots = append(slots, slot)
	}
	sort.SliceStable(slots, func(i, j int) bool {
		a, b := slots[i], slots[j]
		if a.Relay != b.Relay {
			return a.Relay < b.Relay
		}
		if a.ClusterID != b.ClusterID {
			return a.ClusterID < b.ClusterID
		}
		if a.Nodename != b.Nodename {
			return a.Nodename < b.Nodename
		}
		return a.NodeAddr < b.NodeAddr
	})
	result := RelayStatusList{Remote: remote, Total: len(slots), Slots: slots}
	if len(slots) > maxRelayStatuses {
		result.Slots, result.Truncated = slots[:maxRelayStatuses], true
	}
	result.Count = len(result.Slots)
	result.Provenance = s.newProvenance()
	return result, nil
}

// relayURLWithoutCredentials leaves the user information out of a relay URL,
// which a heartbeat configuration may hold.
func relayURLWithoutCredentials(relay string) string {
	parsed, err := url.Parse(relay)
	if err != nil {
		// What it cannot parse, it cuts before a user information.
		if at := strings.LastIndex(relay, "@"); at >= 0 {
			if scheme := strings.Index(relay, "://"); scheme >= 0 && scheme < at {
				relay = relay[:scheme+3] + relay[at+1:]
			}
		}
		text, _ := cutUTF8(relay, maxRelayFieldText)
		return text
	}
	if parsed.User == nil {
		text, _ := cutUTF8(relay, maxRelayFieldText)
		return text
	}
	parsed.User = nil
	text, _ := cutUTF8(parsed.String(), maxRelayFieldText)
	return text
}
