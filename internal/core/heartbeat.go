package core

import "sort"

const (
	maxHeartbeatMessages  = 100
	maxHeartbeatStreams   = 50
	maxHeartbeatAlerts    = 50
	maxHeartbeatPeers     = 100
	maxHeartbeatTextRunes = 1024
)

// HeartbeatFacts is a bounded factual projection of the heartbeat section
// published by OpenSVC. It deliberately does not classify health or freshness.
type HeartbeatFacts struct {
	UpdatedAt     string               `json:"updated_at" jsonschema:"the heartbeat publication timestamp reported by OpenSVC"`
	LastMessage   HeartbeatLastMessage `json:"last_message" jsonschema:"the last heartbeat message reported by OpenSVC"`
	LastMessages  HeartbeatMessageList `json:"last_messages" jsonschema:"bounded recent heartbeat messages reported by OpenSVC"`
	SecretVersion HeartbeatSecret      `json:"secret_version" jsonschema:"heartbeat secret version numbers; no secret material is returned"`
	Streams       HeartbeatStreamList  `json:"streams" jsonschema:"bounded heartbeat streams reported by OpenSVC without health classification"`
}

type HeartbeatLastMessage struct {
	From        string `json:"from" jsonschema:"the node name carried by the heartbeat message"`
	PatchLength int    `json:"patch_length" jsonschema:"the patch queue length reported by OpenSVC"`
	Type        string `json:"type" jsonschema:"the exact heartbeat message type reported by OpenSVC"`
}

type HeartbeatMessageList struct {
	Total     int                    `json:"total" jsonschema:"number of heartbeat messages before limiting"`
	Count     int                    `json:"count" jsonschema:"number of heartbeat messages returned"`
	Items     []HeartbeatLastMessage `json:"items" jsonschema:"heartbeat messages in daemon-provided order"`
	Truncated bool                   `json:"truncated" jsonschema:"whether heartbeat messages were omitted after the 100-entry limit"`
}

type HeartbeatSecret struct {
	Main      uint64 `json:"main" jsonschema:"the main heartbeat secret version number"`
	Alternate uint64 `json:"alternate" jsonschema:"the alternate heartbeat secret version number"`
}

type HeartbeatStreamList struct {
	Total     int               `json:"total" jsonschema:"number of heartbeat streams before limiting"`
	Count     int               `json:"count" jsonschema:"number of heartbeat streams returned"`
	Items     []HeartbeatStream `json:"items" jsonschema:"heartbeat streams sorted by exact stream identifier"`
	Truncated bool              `json:"truncated" jsonschema:"whether heartbeat streams were omitted after the 50-entry limit"`
}

type HeartbeatStream struct {
	ID           string             `json:"id" jsonschema:"the exact heartbeat stream identifier"`
	Type         string             `json:"type" jsonschema:"the exact heartbeat transport type reported by OpenSVC"`
	State        string             `json:"state" jsonschema:"the exact heartbeat stream state reported by OpenSVC"`
	ConfiguredAt string             `json:"configured_at" jsonschema:"the stream configuration timestamp reported by OpenSVC"`
	CreatedAt    string             `json:"created_at" jsonschema:"the stream creation timestamp reported by OpenSVC"`
	UpdatedAt    string             `json:"updated_at" jsonschema:"the stream update timestamp reported by OpenSVC"`
	Alerts       HeartbeatAlertList `json:"alerts" jsonschema:"bounded alerts reported for this stream"`
	Peers        HeartbeatPeerList  `json:"peers" jsonschema:"bounded peer links reported for this stream"`
}

type HeartbeatAlertList struct {
	Total     int              `json:"total" jsonschema:"number of stream alerts before limiting"`
	Count     int              `json:"count" jsonschema:"number of stream alerts returned"`
	Items     []HeartbeatAlert `json:"items" jsonschema:"stream alerts in daemon-provided order"`
	Truncated bool             `json:"truncated" jsonschema:"whether alerts were omitted after the 50-entry limit"`
}

type HeartbeatAlert struct {
	Severity         string `json:"severity" jsonschema:"the exact alert severity reported by OpenSVC"`
	Message          string `json:"message" jsonschema:"the bounded alert message reported by OpenSVC"`
	MessageTruncated bool   `json:"message_truncated" jsonschema:"whether the alert message was shortened after 1024 Unicode characters"`
}

type HeartbeatPeerList struct {
	Total     int             `json:"total" jsonschema:"number of stream peer links before limiting"`
	Count     int             `json:"count" jsonschema:"number of stream peer links returned"`
	Items     []HeartbeatPeer `json:"items" jsonschema:"peer links sorted by exact peer node name"`
	Truncated bool            `json:"truncated" jsonschema:"whether peer links were omitted after the 100-entry limit"`
}

type HeartbeatPeer struct {
	Name                 string `json:"name" jsonschema:"the exact peer node name"`
	Description          string `json:"description" jsonschema:"the bounded heartbeat link description reported by OpenSVC"`
	DescriptionTruncated bool   `json:"description_truncated" jsonschema:"whether the description was shortened after 1024 Unicode characters"`
	IsBeating            bool   `json:"is_beating" jsonschema:"the peer link beating flag reported by OpenSVC"`
	ChangedAt            string `json:"changed_at" jsonschema:"the timestamp when is_beating last changed, as reported by OpenSVC"`
	LastBeatingAt        string `json:"last_beating_at" jsonschema:"the last beating timestamp reported by OpenSVC"`
}

func heartbeatFacts(value *clusterHeartbeat) *HeartbeatFacts {
	if value == nil {
		return nil
	}
	messagesEnd := min(len(value.LastMessages), maxHeartbeatMessages)
	messages := make([]HeartbeatLastMessage, 0, messagesEnd)
	for _, message := range value.LastMessages[:messagesEnd] {
		messages = append(messages, heartbeatMessage(message))
	}
	streams := append([]clusterHeartbeatStream(nil), value.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streams[i].ID < streams[j].ID })
	streamsEnd := min(len(streams), maxHeartbeatStreams)
	streamItems := make([]HeartbeatStream, 0, streamsEnd)
	for _, stream := range streams[:streamsEnd] {
		streamItems = append(streamItems, heartbeatStreamFacts(stream))
	}
	return &HeartbeatFacts{
		UpdatedAt: value.UpdatedAt, LastMessage: heartbeatMessage(value.LastMessage),
		LastMessages:  HeartbeatMessageList{Total: len(value.LastMessages), Count: len(messages), Items: messages, Truncated: messagesEnd < len(value.LastMessages)},
		SecretVersion: HeartbeatSecret{Main: value.SecretVersion.Main, Alternate: value.SecretVersion.Alternate},
		Streams:       HeartbeatStreamList{Total: len(streams), Count: len(streamItems), Items: streamItems, Truncated: streamsEnd < len(streams)},
	}
}

func heartbeatMessage(value clusterHeartbeatLastMessage) HeartbeatLastMessage {
	return HeartbeatLastMessage{From: value.From, PatchLength: value.PatchLength, Type: value.Type}
}

func heartbeatStreamFacts(stream clusterHeartbeatStream) HeartbeatStream {
	alertsEnd := min(len(stream.Alerts), maxHeartbeatAlerts)
	alerts := make([]HeartbeatAlert, 0, alertsEnd)
	for _, alert := range stream.Alerts[:alertsEnd] {
		message, truncated := boundedRunes(alert.Message, maxHeartbeatTextRunes)
		alerts = append(alerts, HeartbeatAlert{Severity: alert.Severity, Message: message, MessageTruncated: truncated})
	}
	peerNames := make([]string, 0, len(stream.Peers))
	for name := range stream.Peers {
		peerNames = append(peerNames, name)
	}
	sort.Strings(peerNames)
	peersEnd := min(len(peerNames), maxHeartbeatPeers)
	peers := make([]HeartbeatPeer, 0, peersEnd)
	for _, name := range peerNames[:peersEnd] {
		peer := stream.Peers[name]
		description, truncated := boundedRunes(peer.Description, maxHeartbeatTextRunes)
		peers = append(peers, HeartbeatPeer{Name: name, Description: description, DescriptionTruncated: truncated, IsBeating: peer.IsBeating, ChangedAt: peer.ChangedAt, LastBeatingAt: peer.LastBeatingAt})
	}
	return HeartbeatStream{
		ID: stream.ID, Type: stream.Type, State: stream.State, ConfiguredAt: stream.ConfiguredAt, CreatedAt: stream.CreatedAt, UpdatedAt: stream.UpdatedAt,
		Alerts: HeartbeatAlertList{Total: len(stream.Alerts), Count: len(alerts), Items: alerts, Truncated: alertsEnd < len(stream.Alerts)},
		Peers:  HeartbeatPeerList{Total: len(peerNames), Count: len(peers), Items: peers, Truncated: peersEnd < len(peerNames)},
	}
}
