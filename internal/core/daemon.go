package core

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type JSONGetter interface {
	GetJSON(context.Context, string, url.Values, any) error
}

type JSONPoster interface {
	PostJSON(context.Context, string, url.Values, any, any) error
}

type SSEGetter interface {
	GetSSE(context.Context, string, url.Values, func(string, string, []byte) error) error
}

type StreamGetter interface {
	GetStream(context.Context, string, url.Values, func([]byte) error) error
}

type FileGetter interface {
	GetFile(context.Context, string, url.Values) ([]byte, error)
}

type Service struct {
	client JSONGetter
	now    func() time.Time
}

func New(client JSONGetter) *Service {
	return &Service{client: client, now: time.Now}
}

type DaemonIdentity struct {
	Provenance Provenance            `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Daemon     DaemonProcessIdentity `json:"daemon" jsonschema:"identity of the local OpenSVC daemon process"`
	Cluster    ClusterIdentity       `json:"cluster" jsonschema:"identity of the OpenSVC cluster"`
	Node       NodeIdentity          `json:"node" jsonschema:"identity and role of the local OpenSVC node"`
	Listener   ListenerIdentity      `json:"listener" jsonschema:"OpenSVC daemon listener configuration"`
}

type DaemonProcessIdentity struct {
	NodeName  string `json:"nodename" jsonschema:"the OpenSVC daemon node name"`
	PID       int    `json:"pid" jsonschema:"the OpenSVC daemon process identifier"`
	StartedAt string `json:"started_at" jsonschema:"the OpenSVC daemon start timestamp"`
	Routines  int    `json:"routines" jsonschema:"the number of daemon goroutines"`
}

type ClusterIdentity struct {
	ID            string   `json:"id" jsonschema:"the OpenSVC cluster identifier"`
	Name          string   `json:"name" jsonschema:"the OpenSVC cluster name"`
	Nodes         []string `json:"nodes" jsonschema:"the configured OpenSVC cluster node names"`
	QuorumEnabled bool     `json:"quorum_enabled" jsonschema:"whether the OpenSVC cluster quorum feature is enabled; this does not report whether quorum is currently attained"`
}

type NodeIdentity struct {
	AgentVersion string `json:"agent_version" jsonschema:"the OpenSVC agent version reported by the local node"`
	APIVersion   int    `json:"api_version" jsonschema:"the OpenSVC daemon API compatibility version"`
	Compat       int    `json:"compat_version" jsonschema:"the OpenSVC daemon compatibility version"`
	IsLeader     bool   `json:"is_leader" jsonschema:"whether the local node is cluster leader"`
	IsOverloaded bool   `json:"is_overloaded" jsonschema:"whether the local node is overloaded"`
	BootedAt     string `json:"booted_at" jsonschema:"the local node boot timestamp"`
}

type ListenerIdentity struct {
	Address string `json:"address" jsonschema:"the configured OpenSVC daemon listener address"`
	Port    int    `json:"port" jsonschema:"the configured OpenSVC daemon listener port"`
}

type clusterNodeConfig struct {
	MinAvailMemPct  int      `json:"min_avail_mem_pct"`
	MinAvailSwapPct int      `json:"min_avail_swap_pct"`
	Issues          []string `json:"issues"`
}

type clusterNodeStats struct {
	Load15M      float64 `json:"load_15m"`
	MemAvailPct  int     `json:"mem_avail"`
	MemTotalMB   uint64  `json:"mem_total"`
	Score        int     `json:"score"`
	SwapAvailPct int     `json:"swap_avail"`
	SwapTotalMB  uint64  `json:"swap_total"`
}

type clusterHeartbeat struct {
	LastMessage   clusterHeartbeatLastMessage   `json:"last_message"`
	LastMessages  []clusterHeartbeatLastMessage `json:"last_messages"`
	SecretVersion clusterHeartbeatSecretVersion `json:"secret_version"`
	UpdatedAt     string                        `json:"updated_at"`
	Streams       []clusterHeartbeatStream      `json:"streams"`
}

type clusterHeartbeatLastMessage struct {
	From        string `json:"from"`
	PatchLength int    `json:"patch_length"`
	Type        string `json:"type"`
}

type clusterHeartbeatSecretVersion struct {
	Main      uint64 `json:"main"`
	Alternate uint64 `json:"alt"`
}

type clusterHeartbeatStream struct {
	ID           string                          `json:"id"`
	Type         string                          `json:"type"`
	State        string                          `json:"state"`
	ConfiguredAt string                          `json:"configured_at"`
	CreatedAt    string                          `json:"created_at"`
	UpdatedAt    string                          `json:"updated_at"`
	Alerts       []clusterHeartbeatAlert         `json:"alerts"`
	Peers        map[string]clusterHeartbeatPeer `json:"peers"`
}

type clusterHeartbeatAlert struct {
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type clusterHeartbeatPeer struct {
	Description   string `json:"desc"`
	IsBeating     bool   `json:"is_beating"`
	ChangedAt     string `json:"changed_at"`
	LastBeatingAt string `json:"last_beating_at"`
}

type clusterNodeArbitrator struct {
	URL    string `json:"url"`
	Status string `json:"status"`
	Weight int    `json:"weight"`
}

type clusterStatusResponse struct {
	Cluster struct {
		Config struct {
			ID       string   `json:"id"`
			Name     string   `json:"name"`
			Nodes    []string `json:"nodes"`
			Quorum   bool     `json:"quorum"`
			Issues   []string `json:"issues"`
			Listener struct {
				Address string `json:"addr"`
				Port    int    `json:"port"`
			} `json:"listener"`
		} `json:"config"`
		Status struct {
			IsCompatible bool `json:"is_compat"`
			IsFrozen     bool `json:"is_frozen"`
		} `json:"status"`
		Node map[string]struct {
			Config *clusterNodeConfig `json:"config"`
			Stats  *clusterNodeStats  `json:"stats"`
			Status struct {
				Agent        string                           `json:"agent"`
				API          int                              `json:"api"`
				Compat       int                              `json:"compat"`
				Arbitrators  map[string]clusterNodeArbitrator `json:"arbitrators"`
				Generation   map[string]uint64                `json:"gen"`
				IsLeader     bool                             `json:"is_leader"`
				IsOverloaded bool                             `json:"is_overloaded"`
				BootedAt     string                           `json:"booted_at"`
				FrozenAt     string                           `json:"frozen_at"`
				LeftAt       string                           `json:"left_at"`
				RejoinedAt   string                           `json:"rejoined_at"`
			} `json:"status"`
			Monitor struct {
				State                 string `json:"state"`
				StateUpdatedAt        string `json:"state_updated_at"`
				GlobalExpect          string `json:"global_expect"`
				GlobalExpectUpdatedAt string `json:"global_expect_updated_at"`
				LocalExpect           string `json:"local_expect"`
				LocalExpectUpdatedAt  string `json:"local_expect_updated_at"`
				OrchestrationID       string `json:"orchestration_id"`
				OrchestrationIsDone   bool   `json:"orchestration_is_done"`
				SessionID             string `json:"session_id"`
				UpdatedAt             string `json:"updated_at"`
			} `json:"monitor"`
			Daemon struct {
				PID       int               `json:"pid"`
				StartedAt string            `json:"started_at"`
				Heartbeat *clusterHeartbeat `json:"heartbeat"`
			} `json:"daemon"`
		} `json:"node"`
		Object map[string]struct {
			Availability     *string  `json:"avail"`
			Overall          string   `json:"overall"`
			Provisioned      string   `json:"provisioned"`
			Frozen           string   `json:"frozen"`
			PlacementState   string   `json:"placement_state"`
			PlacementPolicy  string   `json:"placement_policy"`
			Orchestrate      string   `json:"orchestrate"`
			Topology         string   `json:"topology"`
			Priority         int      `json:"priority"`
			UpInstancesCount int      `json:"up_instances_count"`
			Scope            []string `json:"scope"`
			UpdatedAt        string   `json:"updated_at"`
		} `json:"object"`
	} `json:"cluster"`
	Daemon struct {
		NodeName string `json:"nodename"`
		Routines int    `json:"routines"`
	} `json:"daemon"`
}

func (s *Service) getClusterStatus(ctx context.Context) (clusterStatusResponse, error) {
	var status clusterStatusResponse
	err := s.client.GetJSON(ctx, "/api/cluster/status", nil, &status)
	if err != nil {
		return clusterStatusResponse{}, fmt.Errorf("get cluster status: %w", err)
	}
	return status, nil
}

func (s *Service) GetDaemonIdentity(ctx context.Context) (DaemonIdentity, error) {
	status, err := s.getClusterStatus(ctx)
	if err != nil {
		return DaemonIdentity{}, fmt.Errorf("get daemon identity: %w", err)
	}

	nodeName := status.Daemon.NodeName
	if nodeName == "" {
		return DaemonIdentity{}, fmt.Errorf("cluster status has no daemon nodename")
	}
	node, ok := status.Cluster.Node[nodeName]
	if !ok {
		return DaemonIdentity{}, fmt.Errorf("cluster status has no data for local node %q", nodeName)
	}
	if node.Status.Agent == "" {
		return DaemonIdentity{}, fmt.Errorf("cluster status has no agent version for local node %q", nodeName)
	}

	return DaemonIdentity{
		Provenance: s.newProvenance(),
		Daemon:     DaemonProcessIdentity{NodeName: nodeName, PID: node.Daemon.PID, StartedAt: node.Daemon.StartedAt, Routines: status.Daemon.Routines},
		Cluster:    ClusterIdentity{ID: status.Cluster.Config.ID, Name: status.Cluster.Config.Name, Nodes: status.Cluster.Config.Nodes, QuorumEnabled: status.Cluster.Config.Quorum},
		Node:       NodeIdentity{AgentVersion: node.Status.Agent, APIVersion: node.Status.API, Compat: node.Status.Compat, IsLeader: node.Status.IsLeader, IsOverloaded: node.Status.IsOverloaded, BootedAt: node.Status.BootedAt},
		Listener:   ListenerIdentity{Address: status.Cluster.Config.Listener.Address, Port: status.Cluster.Config.Listener.Port},
	}, nil
}
