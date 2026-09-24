package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"
)

const (
	maxDaemonClusterNodes           = 200
	maxDaemonDNSNameservers         = 32
	maxDaemonEndpointComponentRunes = 512
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

type DaemonStatus struct {
	Provenance     Provenance           `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Daemon         DaemonProcessStatus  `json:"daemon" jsonschema:"identity and process facts for the local OpenSVC daemon"`
	Cluster        DaemonClusterContext `json:"cluster" jsonschema:"bounded identity and configuration context for the OpenSVC cluster"`
	Node           DaemonNodeContext    `json:"node" jsonschema:"identity compatibility and role facts for the local OpenSVC node"`
	ListenerConfig DaemonListenerConfig `json:"listener_config" jsonschema:"configured OpenSVC daemon listener address and port"`
	Subsystems     DaemonSubsystems     `json:"subsystems" jsonschema:"exact states timestamps and selected operating facts reported for local daemon subsystems"`
}

type DaemonProcessStatus struct {
	NodeName  string `json:"nodename" jsonschema:"the OpenSVC daemon node name"`
	PID       int    `json:"pid" jsonschema:"the OpenSVC daemon process identifier"`
	StartedAt string `json:"started_at" jsonschema:"the OpenSVC daemon start timestamp"`
	Routines  int    `json:"routines" jsonschema:"the number of daemon goroutines"`
}

type DaemonClusterContext struct {
	ID             string   `json:"id" jsonschema:"the OpenSVC cluster identifier"`
	Name           string   `json:"name" jsonschema:"the OpenSVC cluster name"`
	NodesTotal     int      `json:"nodes_total" jsonschema:"the number of configured OpenSVC cluster node names before limiting"`
	Nodes          []string `json:"nodes" jsonschema:"configured OpenSVC cluster node names sorted and limited to 200"`
	NodesTruncated bool     `json:"nodes_truncated" jsonschema:"whether configured node names were omitted after the 200-entry limit"`
	QuorumEnabled  bool     `json:"quorum_enabled" jsonschema:"whether the OpenSVC cluster quorum feature is enabled; this does not report whether quorum is currently attained"`
}

type DaemonNodeContext struct {
	AgentVersion string `json:"agent_version" jsonschema:"the OpenSVC agent version reported by the local node"`
	APIVersion   int    `json:"api_version" jsonschema:"the OpenSVC daemon API compatibility version"`
	Compat       int    `json:"compat_version" jsonschema:"the OpenSVC daemon compatibility version"`
	IsLeader     bool   `json:"is_leader" jsonschema:"whether the local node is cluster leader"`
	IsOverloaded bool   `json:"is_overloaded" jsonschema:"whether the local node is overloaded"`
	BootedAt     string `json:"booted_at" jsonschema:"the local node boot timestamp"`
}

type DaemonListenerConfig struct {
	Address string `json:"address" jsonschema:"the configured OpenSVC daemon listener address"`
	Port    int    `json:"port" jsonschema:"the configured OpenSVC daemon listener port"`
}

type DaemonSubsystems struct {
	DaemonData *DaemonDataSubsystem       `json:"daemon_data" jsonschema:"daemon data subsystem facts, or null when absent from the daemon response"`
	Listener   *DaemonListenerSubsystem   `json:"listener" jsonschema:"runtime listener subsystem facts, or null when absent from the daemon response"`
	DNS        *DaemonDNSSubsystem        `json:"dns" jsonschema:"DNS subsystem facts, or null when absent from the daemon response"`
	Collector  *DaemonCollectorSubsystem  `json:"collector" jsonschema:"collector subsystem facts with a credential-safe endpoint projection, or null when absent from the daemon response"`
	Scheduler  *DaemonSchedulerSubsystem  `json:"scheduler" jsonschema:"scheduler subsystem facts, or null when absent from the daemon response"`
	RunnerIMon *DaemonRunnerIMonSubsystem `json:"runner_imon" jsonschema:"instance-monitor runner subsystem facts, or null when absent from the daemon response"`
}

type DaemonDataSubsystem struct {
	ID           string `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State        string `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt string `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt    string `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt    string `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	QueueSize    int    `json:"queue_size" jsonschema:"the daemon data queue size reported by OpenSVC"`
}

type DaemonListenerSubsystem struct {
	ID           string                    `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State        string                    `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt string                    `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt    string                    `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt    string                    `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	Address      string                    `json:"address" jsonschema:"the runtime listener bind address reported by OpenSVC"`
	Port         string                    `json:"port" jsonschema:"the runtime listener port reported by OpenSVC"`
	RateLimiter  DaemonListenerRateLimiter `json:"rate_limiter" jsonschema:"runtime listener rate limiter settings reported by OpenSVC"`
}

type DaemonListenerRateLimiter struct {
	Rate      float64 `json:"rate" jsonschema:"the request rate limit reported by OpenSVC"`
	Burst     int     `json:"burst" jsonschema:"the request burst limit reported by OpenSVC"`
	ExpiresNS int64   `json:"expires_ns" jsonschema:"the rate limiter entry expiry duration in nanoseconds reported by OpenSVC"`
}

type DaemonDNSSubsystem struct {
	ID                   string   `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State                string   `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt         string   `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt            string   `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt            string   `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	NameserversTotal     int      `json:"nameservers_total" jsonschema:"the number of nameservers reported by OpenSVC before limiting"`
	Nameservers          []string `json:"nameservers" jsonschema:"nameservers reported by OpenSVC limited to 32 entries"`
	NameserversTruncated bool     `json:"nameservers_truncated" jsonschema:"whether nameservers were omitted after the 32-entry limit"`
}

type DaemonCollectorSubsystem struct {
	ID           string                  `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State        string                  `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt string                  `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt    string                  `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt    string                  `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	Endpoint     DaemonCollectorEndpoint `json:"endpoint" jsonschema:"credential-safe projection of the collector URL reported by OpenSVC"`
}

type DaemonCollectorEndpoint struct {
	Configured          bool   `json:"configured" jsonschema:"whether OpenSVC reported a non-empty collector URL"`
	Valid               bool   `json:"valid" jsonschema:"whether the reported collector URL has a scheme and host"`
	Scheme              string `json:"scheme" jsonschema:"the bounded collector URL scheme without credentials"`
	Host                string `json:"host" jsonschema:"the bounded collector URL host and optional port without credentials"`
	UserInfoRedacted    bool   `json:"userinfo_redacted" jsonschema:"whether URL user information was present and omitted"`
	PathRedacted        bool   `json:"path_redacted" jsonschema:"whether a non-root URL path was present and omitted"`
	QueryRedacted       bool   `json:"query_redacted" jsonschema:"whether a URL query was present and omitted"`
	FragmentRedacted    bool   `json:"fragment_redacted" jsonschema:"whether a URL fragment was present and omitted"`
	ComponentsTruncated bool   `json:"components_truncated" jsonschema:"whether the scheme or host exceeded the 512-rune output limit"`
}

type DaemonSchedulerSubsystem struct {
	ID           string `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State        string `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt string `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt    string `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt    string `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	Count        int    `json:"count" jsonschema:"the number of running scheduler jobs reported by OpenSVC"`
	MaxRunning   int    `json:"max_running" jsonschema:"the scheduler concurrency limit reported by OpenSVC"`
}

type DaemonRunnerIMonSubsystem struct {
	ID           string `json:"id" jsonschema:"the subsystem identifier reported by OpenSVC"`
	State        string `json:"state" jsonschema:"the exact subsystem state reported by OpenSVC"`
	ConfiguredAt string `json:"configured_at" jsonschema:"the subsystem configuration timestamp reported by OpenSVC"`
	CreatedAt    string `json:"created_at" jsonschema:"the subsystem creation timestamp reported by OpenSVC"`
	UpdatedAt    string `json:"updated_at" jsonschema:"the subsystem update timestamp reported by OpenSVC"`
	MaxRunning   int    `json:"max_running" jsonschema:"the instance-monitor runner concurrency limit reported by OpenSVC"`
}

type daemonSubsystemStatus struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	ConfiguredAt string `json:"configured_at"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type daemonDataStatus struct {
	daemonSubsystemStatus
	QueueSize int `json:"queue_size"`
}

type daemonListenerStatus struct {
	daemonSubsystemStatus
	Address     string `json:"addr"`
	Port        string `json:"port"`
	RateLimiter struct {
		Rate    float64 `json:"rate"`
		Burst   int     `json:"burst"`
		Expires int64   `json:"expires"`
	} `json:"rate_limiter"`
}

type daemonDNSStatus struct {
	daemonSubsystemStatus
	Nameservers []string `json:"nameservers"`
}

type daemonCollectorStatus struct {
	daemonSubsystemStatus
	URL string `json:"url"`
}

type daemonSchedulerStatus struct {
	daemonSubsystemStatus
	Count      int `json:"count"`
	MaxRunning int `json:"max_running"`
}

type daemonRunnerIMonStatus struct {
	daemonSubsystemStatus
	MaxRunning int `json:"max_running"`
}

type clusterNodeDaemon struct {
	PID        int                     `json:"pid"`
	StartedAt  string                  `json:"started_at"`
	DaemonData *daemonDataStatus       `json:"daemondata"`
	Listener   *daemonListenerStatus   `json:"listener"`
	DNS        *daemonDNSStatus        `json:"dns"`
	Collector  *daemonCollectorStatus  `json:"collector"`
	Scheduler  *daemonSchedulerStatus  `json:"scheduler"`
	RunnerIMon *daemonRunnerIMonStatus `json:"runner_imon"`
	Heartbeat  *clusterHeartbeat       `json:"heartbeat"`
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
			Daemon clusterNodeDaemon `json:"daemon"`
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

func (s *Service) GetDaemonStatus(ctx context.Context) (DaemonStatus, error) {
	status, err := s.getClusterStatus(ctx)
	if err != nil {
		return DaemonStatus{}, fmt.Errorf("get daemon status: %w", err)
	}

	nodeName := status.Daemon.NodeName
	if nodeName == "" {
		return DaemonStatus{}, fmt.Errorf("cluster status has no daemon nodename")
	}
	node, ok := status.Cluster.Node[nodeName]
	if !ok {
		return DaemonStatus{}, fmt.Errorf("cluster status has no data for local node %q", nodeName)
	}
	if node.Status.Agent == "" {
		return DaemonStatus{}, fmt.Errorf("cluster status has no agent version for local node %q", nodeName)
	}

	nodes := append([]string{}, status.Cluster.Config.Nodes...)
	sort.Strings(nodes)
	nodes, nodesTruncated := boundedStrings(nodes, maxDaemonClusterNodes)

	return DaemonStatus{
		Provenance: s.newProvenance(),
		Daemon:     DaemonProcessStatus{NodeName: nodeName, PID: node.Daemon.PID, StartedAt: node.Daemon.StartedAt, Routines: status.Daemon.Routines},
		Cluster: DaemonClusterContext{
			ID: status.Cluster.Config.ID, Name: status.Cluster.Config.Name,
			NodesTotal: len(status.Cluster.Config.Nodes), Nodes: nodes, NodesTruncated: nodesTruncated,
			QuorumEnabled: status.Cluster.Config.Quorum,
		},
		Node: DaemonNodeContext{
			AgentVersion: node.Status.Agent, APIVersion: node.Status.API, Compat: node.Status.Compat,
			IsLeader: node.Status.IsLeader, IsOverloaded: node.Status.IsOverloaded, BootedAt: node.Status.BootedAt,
		},
		ListenerConfig: DaemonListenerConfig{Address: status.Cluster.Config.Listener.Address, Port: status.Cluster.Config.Listener.Port},
		Subsystems:     daemonSubsystems(node.Daemon),
	}, nil
}

func daemonSubsystems(raw clusterNodeDaemon) DaemonSubsystems {
	result := DaemonSubsystems{}
	if value := raw.DaemonData; value != nil {
		result.DaemonData = &DaemonDataSubsystem{ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, QueueSize: value.QueueSize}
	}
	if value := raw.Listener; value != nil {
		result.Listener = &DaemonListenerSubsystem{
			ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
			Address: value.Address, Port: value.Port,
			RateLimiter: DaemonListenerRateLimiter{Rate: value.RateLimiter.Rate, Burst: value.RateLimiter.Burst, ExpiresNS: value.RateLimiter.Expires},
		}
	}
	if value := raw.DNS; value != nil {
		nameservers, truncated := boundedStrings(value.Nameservers, maxDaemonDNSNameservers)
		result.DNS = &DaemonDNSSubsystem{
			ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
			NameserversTotal: len(value.Nameservers), Nameservers: nameservers, NameserversTruncated: truncated,
		}
	}
	if value := raw.Collector; value != nil {
		result.Collector = &DaemonCollectorSubsystem{
			ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
			Endpoint: daemonCollectorEndpoint(value.URL),
		}
	}
	if value := raw.Scheduler; value != nil {
		result.Scheduler = &DaemonSchedulerSubsystem{ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, Count: value.Count, MaxRunning: value.MaxRunning}
	}
	if value := raw.RunnerIMon; value != nil {
		result.RunnerIMon = &DaemonRunnerIMonSubsystem{ID: value.ID, State: value.State, ConfiguredAt: value.ConfiguredAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, MaxRunning: value.MaxRunning}
	}
	return result
}

func daemonCollectorEndpoint(raw string) DaemonCollectorEndpoint {
	result := DaemonCollectorEndpoint{Configured: raw != ""}
	if raw == "" {
		return result
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return result
	}
	result.Valid = parsed.Scheme != "" && parsed.Host != ""
	result.Scheme, result.ComponentsTruncated = boundedRunes(parsed.Scheme, maxDaemonEndpointComponentRunes)
	var hostTruncated bool
	result.Host, hostTruncated = boundedRunes(parsed.Host, maxDaemonEndpointComponentRunes)
	result.ComponentsTruncated = result.ComponentsTruncated || hostTruncated
	result.UserInfoRedacted = parsed.User != nil
	result.PathRedacted = parsed.EscapedPath() != "" && parsed.EscapedPath() != "/"
	result.QueryRedacted = parsed.RawQuery != ""
	result.FragmentRedacted = parsed.Fragment != ""
	return result
}
