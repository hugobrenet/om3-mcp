package core

import (
	"context"
	"fmt"
	"sort"
)

const (
	defaultClusterStatusPageLimit = 100
	maxClusterStatusPageLimit     = 200
	maxClusterStatusCursorLength  = 1024
	maxClusterStatusIssues        = 50
	maxClusterStatusGenerations   = 200
	maxClusterStatusArbitrators   = 50
	maxClusterStatusHBMessages    = 100
	maxClusterStatusHBStreams     = 50
	maxClusterStatusHBAlerts      = 50
	maxClusterStatusHBPeers       = 100
	maxClusterStatusTextRunes     = 1024
)

type GetClusterStatusOptions struct {
	NodeLimit    int
	NodeCursor   string
	ObjectLimit  int
	ObjectCursor string
}

type ClusterStatusSnapshot struct {
	Provenance Provenance              `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Cluster    ClusterStatusFacts      `json:"cluster" jsonschema:"cluster identity, configuration flags, and daemon-reported cluster status"`
	Nodes      ClusterStatusNodePage   `json:"nodes" jsonschema:"bounded page of configured or reported nodes with factual status data"`
	Objects    ClusterStatusObjectPage `json:"objects" jsonschema:"bounded page and exact-value summaries of visible actor objects"`
}

type ClusterStatusFacts struct {
	ID                    string   `json:"id" jsonschema:"the OpenSVC cluster identifier"`
	Name                  string   `json:"name" jsonschema:"the OpenSVC cluster name"`
	QuorumEnabled         bool     `json:"quorum_enabled" jsonschema:"whether quorum is enabled in the cluster configuration"`
	IsCompatible          bool     `json:"is_compatible" jsonschema:"the compatibility flag reported by OpenSVC"`
	IsFrozen              bool     `json:"is_frozen" jsonschema:"the cluster frozen flag reported by OpenSVC"`
	ConfigIssuesTotal     int      `json:"config_issues_total" jsonschema:"number of cluster configuration issues reported by OpenSVC before limiting"`
	ConfigIssues          []string `json:"config_issues" jsonschema:"cluster configuration issues reported by OpenSVC, limited to 50"`
	ConfigIssuesTruncated bool     `json:"config_issues_truncated" jsonschema:"whether cluster configuration issues were omitted after the 50-entry limit"`
}

type ClusterStatusNodePage struct {
	ConfiguredTotal int                 `json:"configured_total" jsonschema:"number of distinct node names in the cluster configuration"`
	ReportedTotal   int                 `json:"reported_total" jsonschema:"number of node names with status data in the daemon response"`
	Total           int                 `json:"total" jsonschema:"number of distinct configured or reported node names"`
	Count           int                 `json:"count" jsonschema:"number of node records returned in this page"`
	Items           []ClusterStatusNode `json:"items" jsonschema:"node records sorted by exact node name"`
	NextCursor      string              `json:"next_cursor,omitempty" jsonschema:"the exact node name to pass as node_cursor for the next page"`
	Truncated       bool                `json:"truncated" jsonschema:"whether more node records remain after this page"`
}

type ClusterStatusNode struct {
	Name       string                      `json:"name" jsonschema:"the exact OpenSVC node name"`
	Configured bool                        `json:"configured" jsonschema:"whether the node name is present in the cluster configuration"`
	Reported   bool                        `json:"reported" jsonschema:"whether the daemon response contains status data for this node"`
	Status     *ClusterStatusNodeReported  `json:"status" jsonschema:"status fields reported by OpenSVC, or null when status data is absent"`
	Monitor    *ClusterStatusNodeMonitor   `json:"monitor" jsonschema:"monitor fields reported by OpenSVC, or null when status data is absent"`
	Stats      *NodeCapacityStats          `json:"stats" jsonschema:"published node capacity statistics, or null when unavailable"`
	Policy     *ClusterStatusNodePolicy    `json:"policy" jsonschema:"selected node configuration facts relevant to capacity, or null when unavailable"`
	Daemon     *ClusterStatusNodeDaemon    `json:"daemon" jsonschema:"daemon process facts, or null when node status is absent"`
	Heartbeat  *ClusterStatusNodeHeartbeat `json:"heartbeat" jsonschema:"bounded heartbeat facts reported by OpenSVC, or null when unavailable"`
}

type ClusterStatusNodeReported struct {
	AgentVersion string                      `json:"agent_version" jsonschema:"the OpenSVC agent version reported by this node"`
	APIVersion   int                         `json:"api_version" jsonschema:"the daemon API compatibility version reported by this node"`
	Compat       int                         `json:"compat_version" jsonschema:"the daemon compatibility version reported by this node"`
	IsLeader     bool                        `json:"is_leader" jsonschema:"whether this node reports itself as cluster leader"`
	IsOverloaded bool                        `json:"is_overloaded" jsonschema:"the overload flag reported by OpenSVC without MCP interpretation"`
	BootedAt     string                      `json:"booted_at" jsonschema:"the node boot timestamp reported by OpenSVC"`
	FrozenAt     string                      `json:"frozen_at" jsonschema:"the node freeze timestamp reported by OpenSVC"`
	LeftAt       string                      `json:"left_at" jsonschema:"the last node leave timestamp reported by OpenSVC"`
	RejoinedAt   string                      `json:"rejoined_at" jsonschema:"the last node rejoin timestamp reported by OpenSVC"`
	Generations  ClusterStatusGenerationList `json:"generations" jsonschema:"bounded generation vector reported by OpenSVC"`
	Arbitrators  ClusterStatusArbitratorList `json:"arbitrators" jsonschema:"bounded arbitrator statuses reported by OpenSVC"`
}

type ClusterStatusGenerationList struct {
	Total     int                       `json:"total" jsonschema:"number of generation entries before limiting"`
	Count     int                       `json:"count" jsonschema:"number of generation entries returned"`
	Items     []ClusterStatusGeneration `json:"items" jsonschema:"generation entries sorted by node name"`
	Truncated bool                      `json:"truncated" jsonschema:"whether generation entries were omitted after the 200-entry limit"`
}

type ClusterStatusGeneration struct {
	Node  string `json:"node" jsonschema:"the node name associated with this generation value"`
	Value uint64 `json:"value" jsonschema:"the exact generation value reported by OpenSVC"`
}

type ClusterStatusArbitratorList struct {
	Total     int                       `json:"total" jsonschema:"number of arbitrator entries before limiting"`
	Count     int                       `json:"count" jsonschema:"number of arbitrator entries returned"`
	Items     []ClusterStatusArbitrator `json:"items" jsonschema:"arbitrator entries sorted by name"`
	Truncated bool                      `json:"truncated" jsonschema:"whether arbitrator entries were omitted after the 50-entry limit"`
}

type ClusterStatusArbitrator struct {
	Name   string `json:"name" jsonschema:"the arbitrator name reported by OpenSVC"`
	URL    string `json:"url" jsonschema:"the arbitrator URL reported by OpenSVC"`
	Status string `json:"status" jsonschema:"the exact arbitrator status reported by OpenSVC"`
	Weight int    `json:"weight" jsonschema:"the arbitrator vote weight reported by OpenSVC"`
}

type ClusterStatusNodeMonitor struct {
	State                 string `json:"state" jsonschema:"the exact node monitor state reported by OpenSVC"`
	StateUpdatedAt        string `json:"state_updated_at" jsonschema:"the monitor state update timestamp reported by OpenSVC"`
	GlobalExpect          string `json:"global_expect" jsonschema:"the exact global target state reported by OpenSVC"`
	GlobalExpectUpdatedAt string `json:"global_expect_updated_at" jsonschema:"the global target update timestamp reported by OpenSVC"`
	LocalExpect           string `json:"local_expect" jsonschema:"the exact local target state reported by OpenSVC"`
	LocalExpectUpdatedAt  string `json:"local_expect_updated_at" jsonschema:"the local target update timestamp reported by OpenSVC"`
	OrchestrationID       string `json:"orchestration_id" jsonschema:"the orchestration identifier reported by OpenSVC"`
	OrchestrationIsDone   bool   `json:"orchestration_is_done" jsonschema:"the orchestration completion flag reported by OpenSVC"`
	SessionID             string `json:"session_id" jsonschema:"the monitor session identifier reported by OpenSVC"`
	UpdatedAt             string `json:"updated_at" jsonschema:"the monitor publication timestamp reported by OpenSVC"`
}

type ClusterStatusNodePolicy struct {
	MinAvailMemPct  int      `json:"min_avail_mem_pct" jsonschema:"the configured minimum available physical memory percentage; zero disables the daemon check"`
	MinAvailSwapPct int      `json:"min_avail_swap_pct" jsonschema:"the configured minimum available swap percentage; zero disables the daemon check"`
	IssuesTotal     int      `json:"issues_total" jsonschema:"number of node configuration issues reported by OpenSVC before limiting"`
	Issues          []string `json:"issues" jsonschema:"node configuration issues reported by OpenSVC, limited to 50"`
	IssuesTruncated bool     `json:"issues_truncated" jsonschema:"whether node configuration issues were omitted after the 50-entry limit"`
}

type ClusterStatusNodeDaemon struct {
	PID       int    `json:"pid" jsonschema:"the OpenSVC daemon process identifier reported for this node"`
	StartedAt string `json:"started_at" jsonschema:"the daemon start timestamp reported for this node"`
}

type ClusterStatusNodeHeartbeat struct {
	UpdatedAt     string                            `json:"updated_at" jsonschema:"the heartbeat publication timestamp reported by OpenSVC"`
	LastMessage   ClusterStatusHeartbeatLastMessage `json:"last_message" jsonschema:"the last heartbeat message reported by OpenSVC"`
	LastMessages  ClusterStatusHeartbeatMessageList `json:"last_messages" jsonschema:"bounded recent heartbeat messages reported by OpenSVC"`
	SecretVersion ClusterStatusHeartbeatSecret      `json:"secret_version" jsonschema:"heartbeat secret version numbers; no secret material is returned"`
	Streams       ClusterStatusHeartbeatStreamList  `json:"streams" jsonschema:"bounded heartbeat streams reported by OpenSVC without health classification"`
}

type ClusterStatusHeartbeatLastMessage struct {
	From        string `json:"from" jsonschema:"the node name carried by the heartbeat message"`
	PatchLength int    `json:"patch_length" jsonschema:"the patch queue length reported by OpenSVC"`
	Type        string `json:"type" jsonschema:"the exact heartbeat message type reported by OpenSVC"`
}

type ClusterStatusHeartbeatMessageList struct {
	Total     int                                 `json:"total" jsonschema:"number of heartbeat messages before limiting"`
	Count     int                                 `json:"count" jsonschema:"number of heartbeat messages returned"`
	Items     []ClusterStatusHeartbeatLastMessage `json:"items" jsonschema:"heartbeat messages in daemon-provided order"`
	Truncated bool                                `json:"truncated" jsonschema:"whether heartbeat messages were omitted after the 100-entry limit"`
}

type ClusterStatusHeartbeatSecret struct {
	Main      uint64 `json:"main" jsonschema:"the main heartbeat secret version number"`
	Alternate uint64 `json:"alternate" jsonschema:"the alternate heartbeat secret version number"`
}

type ClusterStatusHeartbeatStreamList struct {
	Total     int                            `json:"total" jsonschema:"number of heartbeat streams before limiting"`
	Count     int                            `json:"count" jsonschema:"number of heartbeat streams returned"`
	Items     []ClusterStatusHeartbeatStream `json:"items" jsonschema:"heartbeat streams sorted by exact stream identifier"`
	Truncated bool                           `json:"truncated" jsonschema:"whether heartbeat streams were omitted after the 50-entry limit"`
}

type ClusterStatusHeartbeatStream struct {
	ID           string                          `json:"id" jsonschema:"the exact heartbeat stream identifier"`
	Type         string                          `json:"type" jsonschema:"the exact heartbeat transport type reported by OpenSVC"`
	State        string                          `json:"state" jsonschema:"the exact heartbeat stream state reported by OpenSVC"`
	ConfiguredAt string                          `json:"configured_at" jsonschema:"the stream configuration timestamp reported by OpenSVC"`
	CreatedAt    string                          `json:"created_at" jsonschema:"the stream creation timestamp reported by OpenSVC"`
	UpdatedAt    string                          `json:"updated_at" jsonschema:"the stream update timestamp reported by OpenSVC"`
	Alerts       ClusterStatusHeartbeatAlertList `json:"alerts" jsonschema:"bounded alerts reported for this stream"`
	Peers        ClusterStatusHeartbeatPeerList  `json:"peers" jsonschema:"bounded peer links reported for this stream"`
}

type ClusterStatusHeartbeatAlertList struct {
	Total     int                           `json:"total" jsonschema:"number of stream alerts before limiting"`
	Count     int                           `json:"count" jsonschema:"number of stream alerts returned"`
	Items     []ClusterStatusHeartbeatAlert `json:"items" jsonschema:"stream alerts in daemon-provided order"`
	Truncated bool                          `json:"truncated" jsonschema:"whether alerts were omitted after the 50-entry limit"`
}

type ClusterStatusHeartbeatAlert struct {
	Severity         string `json:"severity" jsonschema:"the exact alert severity reported by OpenSVC"`
	Message          string `json:"message" jsonschema:"the bounded alert message reported by OpenSVC"`
	MessageTruncated bool   `json:"message_truncated" jsonschema:"whether the alert message was shortened after 1024 Unicode characters"`
}

type ClusterStatusHeartbeatPeerList struct {
	Total     int                          `json:"total" jsonschema:"number of stream peer links before limiting"`
	Count     int                          `json:"count" jsonschema:"number of stream peer links returned"`
	Items     []ClusterStatusHeartbeatPeer `json:"items" jsonschema:"peer links sorted by exact peer node name"`
	Truncated bool                         `json:"truncated" jsonschema:"whether peer links were omitted after the 100-entry limit"`
}

type ClusterStatusHeartbeatPeer struct {
	Name                 string `json:"name" jsonschema:"the exact peer node name"`
	Description          string `json:"description" jsonschema:"the bounded heartbeat link description reported by OpenSVC"`
	DescriptionTruncated bool   `json:"description_truncated" jsonschema:"whether the description was shortened after 1024 Unicode characters"`
	IsBeating            bool   `json:"is_beating" jsonschema:"the peer link beating flag reported by OpenSVC"`
	ChangedAt            string `json:"changed_at" jsonschema:"the timestamp when is_beating last changed, as reported by OpenSVC"`
	LastBeatingAt        string `json:"last_beating_at" jsonschema:"the last beating timestamp reported by OpenSVC"`
}

type ClusterStatusObjectPage struct {
	ReportedTotal int                            `json:"reported_total" jsonschema:"number of visible entries in the daemon object map, including non-actors"`
	ActorTotal    int                            `json:"actor_total" jsonschema:"number of visible objects with a daemon-reported availability field"`
	Count         int                            `json:"count" jsonschema:"number of actor object records returned in this page"`
	Items         []ClusterStatusObject          `json:"items" jsonschema:"visible actor objects sorted by canonical path"`
	StateCounts   ClusterStatusObjectStateCounts `json:"state_counts" jsonschema:"counts grouped by exact daemon-reported values across all visible actor objects"`
	NextCursor    string                         `json:"next_cursor,omitempty" jsonschema:"the exact object path to pass as object_cursor for the next page"`
	Truncated     bool                           `json:"truncated" jsonschema:"whether more actor object records remain after this page"`
}

type ClusterStatusObject struct {
	Path             string   `json:"path" jsonschema:"the canonical OpenSVC object path"`
	Availability     string   `json:"availability" jsonschema:"the exact aggregate availability reported by OpenSVC"`
	Overall          string   `json:"overall" jsonschema:"the exact aggregate overall status reported by OpenSVC"`
	Provisioned      string   `json:"provisioned" jsonschema:"the exact aggregate provisioned state reported by OpenSVC"`
	Frozen           string   `json:"frozen" jsonschema:"the exact aggregate freeze state reported by OpenSVC"`
	PlacementState   string   `json:"placement_state" jsonschema:"the exact aggregate placement state reported by OpenSVC"`
	PlacementPolicy  string   `json:"placement_policy" jsonschema:"the placement policy reported by OpenSVC"`
	Orchestrate      string   `json:"orchestrate" jsonschema:"the orchestration mode reported by OpenSVC"`
	Topology         string   `json:"topology" jsonschema:"the topology reported by OpenSVC"`
	Priority         int      `json:"priority" jsonschema:"the orchestration priority reported by OpenSVC"`
	UpInstancesCount int      `json:"up_instances_count" jsonschema:"the aggregate number of up instances reported by OpenSVC"`
	Scope            []string `json:"scope" jsonschema:"the sorted node names in the object scope"`
	UpdatedAt        string   `json:"updated_at" jsonschema:"the last object status update timestamp reported by OpenSVC"`
}

type ClusterStatusObjectStateCounts struct {
	Availability   []ClusterStatusValueCount `json:"availability" jsonschema:"counts by exact availability value"`
	Overall        []ClusterStatusValueCount `json:"overall" jsonschema:"counts by exact overall value"`
	Provisioned    []ClusterStatusValueCount `json:"provisioned" jsonschema:"counts by exact provisioned value"`
	Frozen         []ClusterStatusValueCount `json:"frozen" jsonschema:"counts by exact freeze value"`
	PlacementState []ClusterStatusValueCount `json:"placement_state" jsonschema:"counts by exact placement state value"`
}

type ClusterStatusValueCount struct {
	Value string `json:"value" jsonschema:"the exact value reported by OpenSVC, including an empty string"`
	Count int    `json:"count" jsonschema:"the number of visible actor objects reporting this exact value"`
}

func (s *Service) GetClusterStatus(ctx context.Context, options GetClusterStatusOptions) (ClusterStatusSnapshot, error) {
	nodeLimit, err := clusterStatusPageLimit(options.NodeLimit, "node")
	if err != nil {
		return ClusterStatusSnapshot{}, err
	}
	objectLimit, err := clusterStatusPageLimit(options.ObjectLimit, "object")
	if err != nil {
		return ClusterStatusSnapshot{}, err
	}
	if len(options.NodeCursor) > maxClusterStatusCursorLength {
		return ClusterStatusSnapshot{}, fmt.Errorf("node cursor exceeds %d characters", maxClusterStatusCursorLength)
	}
	if len(options.ObjectCursor) > maxClusterStatusCursorLength {
		return ClusterStatusSnapshot{}, fmt.Errorf("object cursor exceeds %d characters", maxClusterStatusCursorLength)
	}

	status, err := s.getClusterStatus(ctx)
	if err != nil {
		return ClusterStatusSnapshot{}, fmt.Errorf("get cluster status: %w", err)
	}
	result := clusterStatusSnapshot(status, options.NodeCursor, nodeLimit, options.ObjectCursor, objectLimit)
	result.Provenance = s.newProvenance()
	return result, nil
}

func clusterStatusPageLimit(value int, domain string) (int, error) {
	if value == 0 {
		return defaultClusterStatusPageLimit, nil
	}
	if value < 1 || value > maxClusterStatusPageLimit {
		return 0, fmt.Errorf("cluster status %s limit must be between 1 and %d", domain, maxClusterStatusPageLimit)
	}
	return value, nil
}

func clusterStatusSnapshot(status clusterStatusResponse, nodeCursor string, nodeLimit int, objectCursor string, objectLimit int) ClusterStatusSnapshot {
	clusterIssues, clusterIssuesTruncated := boundedStrings(status.Cluster.Config.Issues, maxClusterStatusIssues)
	result := ClusterStatusSnapshot{
		Cluster: ClusterStatusFacts{
			ID: status.Cluster.Config.ID, Name: status.Cluster.Config.Name,
			QuorumEnabled: status.Cluster.Config.Quorum, IsCompatible: status.Cluster.Status.IsCompatible,
			IsFrozen: status.Cluster.Status.IsFrozen, ConfigIssuesTotal: len(status.Cluster.Config.Issues),
			ConfigIssues: clusterIssues, ConfigIssuesTruncated: clusterIssuesTruncated,
		},
	}
	result.Nodes = clusterStatusNodes(status, nodeCursor, nodeLimit)
	result.Objects = clusterStatusObjects(status, objectCursor, objectLimit)
	return result
}

func clusterStatusNodes(status clusterStatusResponse, cursor string, limit int) ClusterStatusNodePage {
	configured := make(map[string]bool, len(status.Cluster.Config.Nodes))
	allNames := make(map[string]struct{}, len(status.Cluster.Config.Nodes)+len(status.Cluster.Node))
	for _, name := range status.Cluster.Config.Nodes {
		configured[name] = true
		allNames[name] = struct{}{}
	}
	for name := range status.Cluster.Node {
		allNames[name] = struct{}{}
	}
	names := sortedSetKeys(allNames)
	start := sort.Search(len(names), func(i int) bool { return names[i] > cursor })
	end := min(start+limit, len(names))
	items := make([]ClusterStatusNode, 0, end-start)
	for _, name := range names[start:end] {
		node, reported := status.Cluster.Node[name]
		item := ClusterStatusNode{Name: name, Configured: configured[name], Reported: reported}
		if reported {
			item.Status = clusterStatusNodeReported(node.Status.Agent, node.Status.API, node.Status.Compat, node.Status.IsLeader, node.Status.IsOverloaded, node.Status.BootedAt, node.Status.FrozenAt, node.Status.LeftAt, node.Status.RejoinedAt, node.Status.Generation, node.Status.Arbitrators)
			item.Monitor = &ClusterStatusNodeMonitor{
				State: node.Monitor.State, StateUpdatedAt: node.Monitor.StateUpdatedAt,
				GlobalExpect: node.Monitor.GlobalExpect, GlobalExpectUpdatedAt: node.Monitor.GlobalExpectUpdatedAt,
				LocalExpect: node.Monitor.LocalExpect, LocalExpectUpdatedAt: node.Monitor.LocalExpectUpdatedAt,
				OrchestrationID: node.Monitor.OrchestrationID, OrchestrationIsDone: node.Monitor.OrchestrationIsDone,
				SessionID: node.Monitor.SessionID, UpdatedAt: node.Monitor.UpdatedAt,
			}
			item.Daemon = &ClusterStatusNodeDaemon{PID: node.Daemon.PID, StartedAt: node.Daemon.StartedAt}
			if node.Stats != nil {
				item.Stats = &NodeCapacityStats{Load15M: node.Stats.Load15M, MemAvailPct: node.Stats.MemAvailPct, MemTotalMB: node.Stats.MemTotalMB, Score: node.Stats.Score, SwapAvailPct: node.Stats.SwapAvailPct, SwapTotalMB: node.Stats.SwapTotalMB}
			}
			if node.Config != nil {
				issues, truncated := boundedStrings(node.Config.Issues, maxClusterStatusIssues)
				item.Policy = &ClusterStatusNodePolicy{MinAvailMemPct: node.Config.MinAvailMemPct, MinAvailSwapPct: node.Config.MinAvailSwapPct, IssuesTotal: len(node.Config.Issues), Issues: issues, IssuesTruncated: truncated}
			}
			if node.Daemon.Heartbeat != nil {
				item.Heartbeat = clusterStatusHeartbeat(node.Daemon.Heartbeat)
			}
		}
		items = append(items, item)
	}
	page := ClusterStatusNodePage{ConfiguredTotal: len(configured), ReportedTotal: len(status.Cluster.Node), Total: len(names), Count: len(items), Items: items, Truncated: end < len(names)}
	if page.Truncated {
		page.NextCursor = items[len(items)-1].Name
	}
	return page
}

func clusterStatusNodeReported(agent string, api int, compat int, leader bool, overloaded bool, bootedAt string, frozenAt string, leftAt string, rejoinedAt string, generations map[string]uint64, arbitrators map[string]clusterNodeArbitrator) *ClusterStatusNodeReported {
	return &ClusterStatusNodeReported{
		AgentVersion: agent, APIVersion: api, Compat: compat, IsLeader: leader, IsOverloaded: overloaded,
		BootedAt: bootedAt, FrozenAt: frozenAt, LeftAt: leftAt, RejoinedAt: rejoinedAt,
		Generations: clusterStatusGenerations(generations), Arbitrators: clusterStatusArbitrators(arbitrators),
	}
}

func clusterStatusGenerations(values map[string]uint64) ClusterStatusGenerationList {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	end := min(len(names), maxClusterStatusGenerations)
	items := make([]ClusterStatusGeneration, 0, end)
	for _, name := range names[:end] {
		items = append(items, ClusterStatusGeneration{Node: name, Value: values[name]})
	}
	return ClusterStatusGenerationList{Total: len(names), Count: len(items), Items: items, Truncated: end < len(names)}
}

func clusterStatusArbitrators(values map[string]clusterNodeArbitrator) ClusterStatusArbitratorList {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	end := min(len(names), maxClusterStatusArbitrators)
	items := make([]ClusterStatusArbitrator, 0, end)
	for _, name := range names[:end] {
		value := values[name]
		items = append(items, ClusterStatusArbitrator{Name: name, URL: value.URL, Status: value.Status, Weight: value.Weight})
	}
	return ClusterStatusArbitratorList{Total: len(names), Count: len(items), Items: items, Truncated: end < len(names)}
}

func clusterStatusHeartbeat(value *clusterHeartbeat) *ClusterStatusNodeHeartbeat {
	messagesEnd := min(len(value.LastMessages), maxClusterStatusHBMessages)
	messages := make([]ClusterStatusHeartbeatLastMessage, 0, messagesEnd)
	for _, message := range value.LastMessages[:messagesEnd] {
		messages = append(messages, clusterStatusHeartbeatMessage(message))
	}
	streams := append([]clusterHeartbeatStream(nil), value.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streams[i].ID < streams[j].ID })
	streamsEnd := min(len(streams), maxClusterStatusHBStreams)
	streamItems := make([]ClusterStatusHeartbeatStream, 0, streamsEnd)
	for _, stream := range streams[:streamsEnd] {
		streamItems = append(streamItems, clusterStatusHeartbeatStreamFacts(stream))
	}
	return &ClusterStatusNodeHeartbeat{
		UpdatedAt: value.UpdatedAt, LastMessage: clusterStatusHeartbeatMessage(value.LastMessage),
		LastMessages:  ClusterStatusHeartbeatMessageList{Total: len(value.LastMessages), Count: len(messages), Items: messages, Truncated: messagesEnd < len(value.LastMessages)},
		SecretVersion: ClusterStatusHeartbeatSecret{Main: value.SecretVersion.Main, Alternate: value.SecretVersion.Alternate},
		Streams:       ClusterStatusHeartbeatStreamList{Total: len(streams), Count: len(streamItems), Items: streamItems, Truncated: streamsEnd < len(streams)},
	}
}

func clusterStatusHeartbeatMessage(value clusterHeartbeatLastMessage) ClusterStatusHeartbeatLastMessage {
	return ClusterStatusHeartbeatLastMessage{From: value.From, PatchLength: value.PatchLength, Type: value.Type}
}

func clusterStatusHeartbeatStreamFacts(stream clusterHeartbeatStream) ClusterStatusHeartbeatStream {
	alertsEnd := min(len(stream.Alerts), maxClusterStatusHBAlerts)
	alerts := make([]ClusterStatusHeartbeatAlert, 0, alertsEnd)
	for _, alert := range stream.Alerts[:alertsEnd] {
		message, truncated := boundedRunes(alert.Message, maxClusterStatusTextRunes)
		alerts = append(alerts, ClusterStatusHeartbeatAlert{Severity: alert.Severity, Message: message, MessageTruncated: truncated})
	}
	peerNames := make([]string, 0, len(stream.Peers))
	for name := range stream.Peers {
		peerNames = append(peerNames, name)
	}
	sort.Strings(peerNames)
	peersEnd := min(len(peerNames), maxClusterStatusHBPeers)
	peers := make([]ClusterStatusHeartbeatPeer, 0, peersEnd)
	for _, name := range peerNames[:peersEnd] {
		peer := stream.Peers[name]
		description, truncated := boundedRunes(peer.Description, maxClusterStatusTextRunes)
		peers = append(peers, ClusterStatusHeartbeatPeer{Name: name, Description: description, DescriptionTruncated: truncated, IsBeating: peer.IsBeating, ChangedAt: peer.ChangedAt, LastBeatingAt: peer.LastBeatingAt})
	}
	return ClusterStatusHeartbeatStream{
		ID: stream.ID, Type: stream.Type, State: stream.State, ConfiguredAt: stream.ConfiguredAt, CreatedAt: stream.CreatedAt, UpdatedAt: stream.UpdatedAt,
		Alerts: ClusterStatusHeartbeatAlertList{Total: len(stream.Alerts), Count: len(alerts), Items: alerts, Truncated: alertsEnd < len(stream.Alerts)},
		Peers:  ClusterStatusHeartbeatPeerList{Total: len(peerNames), Count: len(peers), Items: peers, Truncated: peersEnd < len(peerNames)},
	}
}

func clusterStatusObjects(status clusterStatusResponse, cursor string, limit int) ClusterStatusObjectPage {
	items := make([]ClusterStatusObject, 0, len(status.Cluster.Object))
	availability := make(map[string]int)
	overall := make(map[string]int)
	provisioned := make(map[string]int)
	frozen := make(map[string]int)
	placement := make(map[string]int)
	for path, object := range status.Cluster.Object {
		if object.Availability == nil {
			continue
		}
		scope := append([]string{}, object.Scope...)
		sort.Strings(scope)
		items = append(items, ClusterStatusObject{Path: path, Availability: *object.Availability, Overall: object.Overall, Provisioned: object.Provisioned, Frozen: object.Frozen, PlacementState: object.PlacementState, PlacementPolicy: object.PlacementPolicy, Orchestrate: object.Orchestrate, Topology: object.Topology, Priority: object.Priority, UpInstancesCount: object.UpInstancesCount, Scope: scope, UpdatedAt: object.UpdatedAt})
		availability[*object.Availability]++
		overall[object.Overall]++
		provisioned[object.Provisioned]++
		frozen[object.Frozen]++
		placement[object.PlacementState]++
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	start := sort.Search(len(items), func(i int) bool { return items[i].Path > cursor })
	end := min(start+limit, len(items))
	pageItems := append([]ClusterStatusObject{}, items[start:end]...)
	page := ClusterStatusObjectPage{
		ReportedTotal: len(status.Cluster.Object), ActorTotal: len(items), Count: len(pageItems), Items: pageItems,
		StateCounts: ClusterStatusObjectStateCounts{Availability: exactValueCounts(availability), Overall: exactValueCounts(overall), Provisioned: exactValueCounts(provisioned), Frozen: exactValueCounts(frozen), PlacementState: exactValueCounts(placement)},
		Truncated:   end < len(items),
	}
	if page.Truncated {
		page.NextCursor = pageItems[len(pageItems)-1].Path
	}
	return page
}

func exactValueCounts(values map[string]int) []ClusterStatusValueCount {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	result := make([]ClusterStatusValueCount, 0, len(keys))
	for _, value := range keys {
		result = append(result, ClusterStatusValueCount{Value: value, Count: values[value]})
	}
	return result
}

func boundedStrings(values []string, limit int) ([]string, bool) {
	end := min(len(values), limit)
	result := append([]string{}, values[:end]...)
	return result, end < len(values)
}

func boundedRunes(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]), true
}

func sortedSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
