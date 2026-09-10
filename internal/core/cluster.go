package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const maxClusterHealthProblemObjects = 100

type ClusterHealth struct {
	Healthy                 bool                       `json:"healthy" jsonschema:"whether all evaluated cluster health checks pass"`
	Cluster                 ClusterHealthStatus        `json:"cluster" jsonschema:"cluster-wide health checks"`
	NodeSummary             ClusterNodeHealthSummary   `json:"node_summary" jsonschema:"summary of evaluated node health"`
	Nodes                   []ClusterNodeHealth        `json:"nodes" jsonschema:"health details for configured and reported nodes"`
	ObjectSummary           ClusterObjectHealthSummary `json:"object_summary" jsonschema:"summary of actor object availability"`
	ProblemObjects          []ClusterObjectHealth      `json:"problem_objects" jsonschema:"actor objects with health issues, sorted by path and limited to 100 entries"`
	ProblemObjectsTruncated bool                       `json:"problem_objects_truncated" jsonschema:"whether more than 100 problem objects were found"`
}

type ClusterHealthStatus struct {
	ID           string   `json:"id" jsonschema:"the OpenSVC cluster identifier"`
	Name         string   `json:"name" jsonschema:"the OpenSVC cluster name"`
	IsCompatible bool     `json:"is_compatible" jsonschema:"whether cluster nodes are mutually compatible"`
	IsFrozen     bool     `json:"is_frozen" jsonschema:"whether the cluster is frozen"`
	LeaderNodes  []string `json:"leader_nodes" jsonschema:"reported cluster leader node names"`
	Issues       []string `json:"issues" jsonschema:"cluster-wide health issues"`
}

type ClusterNodeHealthSummary struct {
	Total      int `json:"total" jsonschema:"number of configured or reported nodes evaluated"`
	Healthy    int `json:"healthy" jsonschema:"number of evaluated nodes with no health issues"`
	Missing    int `json:"missing" jsonschema:"number of configured nodes with no reported status data"`
	Frozen     int `json:"frozen" jsonschema:"number of reported nodes considered frozen"`
	Overloaded int `json:"overloaded" jsonschema:"number of reported nodes indicating overload"`
	NonIdle    int `json:"non_idle" jsonschema:"number of reported nodes whose non-empty monitor state is not idle"`
}

type ClusterNodeHealth struct {
	Name         string                   `json:"name" jsonschema:"the OpenSVC node name"`
	Reported     bool                     `json:"reported" jsonschema:"whether cluster status contains data for this node"`
	Healthy      bool                     `json:"healthy" jsonschema:"whether this node has no evaluated health issues"`
	MonitorState string                   `json:"monitor_state" jsonschema:"the monitor state reported by the node, or an empty string when unavailable"`
	IsLeader     bool                     `json:"is_leader" jsonschema:"whether the node reports itself as cluster leader"`
	IsFrozen     bool                     `json:"is_frozen" jsonschema:"whether the node frozen timestamp is non-zero or invalid"`
	IsOverloaded bool                     `json:"is_overloaded" jsonschema:"whether the node reports overload"`
	Issues       []ClusterNodeHealthIssue `json:"issues" jsonschema:"deterministic structured health issues identified for this node"`
}

type ClusterNodeHealthIssue struct {
	Code               string                               `json:"code" jsonschema:"stable machine-readable issue code"`
	Message            string                               `json:"message" jsonschema:"concise human-readable explanation of the issue"`
	Evidence           *ClusterNodeHealthIssueEvidence      `json:"evidence,omitempty" jsonschema:"bounded OpenSVC values that prove the issue when available"`
	Policy             *ClusterNodeHealthPolicy             `json:"policy,omitempty" jsonschema:"OpenSVC policy responsible for evaluating the issue when applicable"`
	RemediationOptions []ClusterNodeHealthRemediationOption `json:"remediation_options,omitempty" jsonschema:"conditional remediation candidates; the MCP neither selects nor applies them"`
}

type ClusterNodeHealthIssueEvidence struct {
	MemoryTotalMB         *uint64 `json:"memory_total_mb,omitempty" jsonschema:"total physical memory in megabytes"`
	MemoryAvailablePct    *int    `json:"memory_available_pct,omitempty" jsonschema:"available physical memory as a percentage"`
	MinimumMemoryAvailPct *int    `json:"minimum_memory_available_pct,omitempty" jsonschema:"configured minimum available physical memory percentage"`
	SwapTotalMB           *uint64 `json:"swap_total_mb,omitempty" jsonschema:"total swap capacity in megabytes"`
	SwapAvailablePct      *int    `json:"swap_available_pct,omitempty" jsonschema:"available swap as a percentage"`
	MinimumSwapAvailPct   *int    `json:"minimum_swap_available_pct,omitempty" jsonschema:"configured minimum available swap percentage"`
}

type ClusterNodeHealthPolicy struct {
	ConfigKey           string `json:"config_key" jsonschema:"fully qualified OpenSVC configuration key"`
	CurrentValue        int    `json:"current_value" jsonschema:"current configured policy value"`
	Unit                string `json:"unit" jsonschema:"unit of the configured policy value"`
	OverloadedWhen      string `json:"overloaded_when" jsonschema:"deterministic condition used by OpenSVC to report overload"`
	DisabledWhenValueIs *int   `json:"disabled_when_value_is,omitempty" jsonschema:"configuration value that disables this policy check when supported"`
}

type ClusterNodeHealthRemediationOption struct {
	ID            string                                `json:"id" jsonschema:"stable machine-readable remediation option identifier"`
	AppliesWhen   string                                `json:"applies_when" jsonschema:"operator intent or condition under which this option is appropriate"`
	Description   string                                `json:"description" jsonschema:"read-only explanation of the remediation option"`
	Configuration *ClusterNodeHealthConfigurationChange `json:"configuration,omitempty" jsonschema:"exact configuration candidate when it can be derived without guessing"`
}

type ClusterNodeHealthConfigurationChange struct {
	Key   string `json:"key" jsonschema:"fully qualified OpenSVC configuration key"`
	Value int    `json:"value" jsonschema:"candidate integer configuration value"`
}

type ClusterObjectHealthSummary struct {
	Total         int `json:"total" jsonschema:"number of visible actor objects evaluated"`
	Up            int `json:"up" jsonschema:"number of actor objects with availability up or stdby up"`
	Down          int `json:"down" jsonschema:"number of actor objects with availability down or stdby down"`
	Warn          int `json:"warn" jsonschema:"number of actor objects with availability warn"`
	NotApplicable int `json:"not_applicable" jsonschema:"number of actor objects with availability n/a"`
	Other         int `json:"other" jsonschema:"number of actor objects with any other availability value"`
	Problems      int `json:"problems" jsonschema:"number of actor objects failing at least one health check"`
}

type ClusterObjectHealth struct {
	Path             string   `json:"path" jsonschema:"the canonical OpenSVC object path"`
	Availability     string   `json:"availability" jsonschema:"the object availability reported by OpenSVC"`
	Overall          string   `json:"overall" jsonschema:"the object overall status reported by OpenSVC"`
	Provisioned      string   `json:"provisioned" jsonschema:"the object provisioned state reported by OpenSVC"`
	Frozen           string   `json:"frozen" jsonschema:"the object freeze state reported by OpenSVC"`
	PlacementState   string   `json:"placement_state" jsonschema:"the object placement state reported by OpenSVC"`
	UpInstancesCount int      `json:"up_instances_count" jsonschema:"the number of up object instances reported by OpenSVC"`
	Scope            []string `json:"scope" jsonschema:"the node names in the object scope"`
	Issues           []string `json:"issues" jsonschema:"deterministic health issues identified for this object"`
}

func (s *Service) GetClusterHealth(ctx context.Context) (ClusterHealth, error) {
	status, err := s.getClusterStatus(ctx)
	if err != nil {
		return ClusterHealth{}, fmt.Errorf("get cluster health: %w", err)
	}
	return clusterHealthFromStatus(status), nil
}

func clusterHealthFromStatus(status clusterStatusResponse) ClusterHealth {
	health := ClusterHealth{
		Cluster: ClusterHealthStatus{
			ID:           status.Cluster.Config.ID,
			Name:         status.Cluster.Config.Name,
			IsCompatible: status.Cluster.Status.IsCompatible,
			IsFrozen:     status.Cluster.Status.IsFrozen,
			LeaderNodes:  []string{},
			Issues:       []string{},
		},
		Nodes:          []ClusterNodeHealth{},
		ProblemObjects: []ClusterObjectHealth{},
	}

	if !health.Cluster.IsCompatible {
		health.Cluster.Issues = append(health.Cluster.Issues, "cluster nodes are not compatible")
	}
	if health.Cluster.IsFrozen {
		health.Cluster.Issues = append(health.Cluster.Issues, "cluster is frozen")
	}

	nodeNames := make(map[string]struct{}, len(status.Cluster.Config.Nodes)+len(status.Cluster.Node))
	for _, name := range status.Cluster.Config.Nodes {
		nodeNames[name] = struct{}{}
	}
	for name, node := range status.Cluster.Node {
		nodeNames[name] = struct{}{}
		if node.Status.IsLeader {
			health.Cluster.LeaderNodes = append(health.Cluster.LeaderNodes, name)
		}
	}
	sort.Strings(health.Cluster.LeaderNodes)
	if len(health.Cluster.LeaderNodes) == 0 {
		health.Cluster.Issues = append(health.Cluster.Issues, "cluster has no reported leader")
	} else if len(health.Cluster.LeaderNodes) > 1 {
		health.Cluster.Issues = append(health.Cluster.Issues, "cluster has multiple reported leaders")
	}

	sortedNodeNames := sortedKeys(nodeNames)
	health.NodeSummary.Total = len(sortedNodeNames)
	for _, name := range sortedNodeNames {
		node, reported := status.Cluster.Node[name]
		nodeHealth := ClusterNodeHealth{Name: name, Reported: reported, Issues: []ClusterNodeHealthIssue{}}
		if !reported {
			nodeHealth.Issues = append(nodeHealth.Issues, nodeIssue("node_status_missing", "configured node has no status data"))
			health.NodeSummary.Missing++
		} else {
			nodeHealth.MonitorState = node.Monitor.State
			nodeHealth.IsLeader = node.Status.IsLeader
			nodeHealth.IsFrozen = isNonZeroTimestamp(node.Status.FrozenAt)
			nodeHealth.IsOverloaded = node.Status.IsOverloaded
			if node.Status.Agent == "" {
				nodeHealth.Issues = append(nodeHealth.Issues, nodeIssue("agent_version_missing", "node has no reported agent version"))
			}
			if strings.TrimSpace(node.Monitor.State) == "" {
				nodeHealth.Issues = append(nodeHealth.Issues, nodeIssue("monitor_state_missing", "node has no monitor state"))
			} else if normalizedState(node.Monitor.State) != "idle" {
				nodeHealth.Issues = append(nodeHealth.Issues, nodeIssue("monitor_state_not_idle", fmt.Sprintf("node monitor state is %q", node.Monitor.State)))
				health.NodeSummary.NonIdle++
			}
			if nodeHealth.IsFrozen {
				nodeHealth.Issues = append(nodeHealth.Issues, nodeIssue("node_frozen", "node is frozen"))
				health.NodeSummary.Frozen++
			}
			if nodeHealth.IsOverloaded {
				nodeHealth.Issues = append(nodeHealth.Issues, overloadIssues(node.Config, node.Stats)...)
				health.NodeSummary.Overloaded++
			}
		}
		nodeHealth.Healthy = len(nodeHealth.Issues) == 0
		if nodeHealth.Healthy {
			health.NodeSummary.Healthy++
		}
		health.Nodes = append(health.Nodes, nodeHealth)
	}

	objectPaths := make([]string, 0, len(status.Cluster.Object))
	for path, object := range status.Cluster.Object {
		if object.Availability != nil {
			objectPaths = append(objectPaths, path)
		}
	}
	sort.Strings(objectPaths)
	for _, path := range objectPaths {
		object := status.Cluster.Object[path]
		availability := normalizedState(*object.Availability)
		health.ObjectSummary.Total++
		switch availability {
		case "up", "stdby up":
			health.ObjectSummary.Up++
		case "down", "stdby down":
			health.ObjectSummary.Down++
		case "warn":
			health.ObjectSummary.Warn++
		case "n/a":
			health.ObjectSummary.NotApplicable++
		default:
			health.ObjectSummary.Other++
		}

		objectHealth := ClusterObjectHealth{
			Path:             path,
			Availability:     *object.Availability,
			Overall:          object.Overall,
			Provisioned:      object.Provisioned,
			Frozen:           object.Frozen,
			PlacementState:   object.PlacementState,
			UpInstancesCount: object.UpInstancesCount,
			Scope:            append([]string(nil), object.Scope...),
			Issues:           []string{},
		}
		if availability != "up" && availability != "stdby up" && availability != "n/a" {
			objectHealth.Issues = append(objectHealth.Issues, fmt.Sprintf("availability is %q", displayState(*object.Availability)))
		}
		if isProblemState(object.Overall, "down", "warn", "undef", "stdby down") {
			objectHealth.Issues = append(objectHealth.Issues, fmt.Sprintf("overall status is %q", displayState(object.Overall)))
		}
		if state := normalizedState(object.PlacementState); state != "" && state != "optimal" && state != "n/a" {
			objectHealth.Issues = append(objectHealth.Issues, fmt.Sprintf("placement state is %q", object.PlacementState))
		}
		if state := normalizedState(object.Frozen); state != "" && state != "unfrozen" {
			objectHealth.Issues = append(objectHealth.Issues, fmt.Sprintf("freeze state is %q", object.Frozen))
		}
		if isProblemState(object.Provisioned, "false", "mixed", "undef") {
			objectHealth.Issues = append(objectHealth.Issues, fmt.Sprintf("provisioned state is %q", object.Provisioned))
		}
		if len(objectHealth.Issues) > 0 {
			health.ObjectSummary.Problems++
			if len(health.ProblemObjects) < maxClusterHealthProblemObjects {
				health.ProblemObjects = append(health.ProblemObjects, objectHealth)
			} else {
				health.ProblemObjectsTruncated = true
			}
		}
	}

	health.Healthy = len(health.Cluster.Issues) == 0 &&
		health.NodeSummary.Healthy == health.NodeSummary.Total &&
		health.ObjectSummary.Problems == 0
	return health
}

func nodeIssue(code, message string) ClusterNodeHealthIssue {
	return ClusterNodeHealthIssue{Code: code, Message: message}
}

func overloadIssues(config *clusterNodeConfig, stats *clusterNodeStats) []ClusterNodeHealthIssue {
	issues := []ClusterNodeHealthIssue{}
	if config == nil || stats == nil {
		return append(issues, nodeIssue("overload_cause_undetermined", "node reports overload but configuration or statistics are unavailable"))
	}

	disabledValue := 0
	if config.MinAvailMemPct > 0 && stats.MemAvailPct < config.MinAvailMemPct {
		issues = append(issues, ClusterNodeHealthIssue{
			Code:    "memory_below_threshold",
			Message: "available physical memory is below the configured threshold",
			Evidence: &ClusterNodeHealthIssueEvidence{
				MemoryTotalMB:         pointer(stats.MemTotalMB),
				MemoryAvailablePct:    pointer(stats.MemAvailPct),
				MinimumMemoryAvailPct: pointer(config.MinAvailMemPct),
			},
			Policy: &ClusterNodeHealthPolicy{
				ConfigKey:           "node.min_avail_mem_pct",
				CurrentValue:        config.MinAvailMemPct,
				Unit:                "percent",
				OverloadedWhen:      "memory_available_pct < minimum_memory_available_pct",
				DisabledWhenValueIs: &disabledValue,
			},
			RemediationOptions: []ClusterNodeHealthRemediationOption{
				{
					ID:          "relieve_memory_pressure",
					AppliesWhen: "the threshold reflects the intended policy and memory pressure is temporary or abnormal",
					Description: "reduce memory consumption or add physical memory until availability is above the configured threshold",
				},
				{
					ID:          "review_memory_threshold",
					AppliesWhen: "the configured threshold does not reflect the intended memory policy",
					Description: "review node.min_avail_mem_pct and choose an operator-approved threshold",
				},
			},
		})
	}

	if config.MinAvailSwapPct > 0 && stats.SwapAvailPct < config.MinAvailSwapPct {
		issue := ClusterNodeHealthIssue{
			Code:    "swap_below_threshold",
			Message: "available swap is below the configured threshold",
			Evidence: &ClusterNodeHealthIssueEvidence{
				SwapTotalMB:         pointer(stats.SwapTotalMB),
				SwapAvailablePct:    pointer(stats.SwapAvailPct),
				MinimumSwapAvailPct: pointer(config.MinAvailSwapPct),
			},
			Policy: &ClusterNodeHealthPolicy{
				ConfigKey:           "node.min_avail_swap_pct",
				CurrentValue:        config.MinAvailSwapPct,
				Unit:                "percent",
				OverloadedWhen:      "swap_available_pct < minimum_swap_available_pct",
				DisabledWhenValueIs: &disabledValue,
			},
		}
		if stats.SwapTotalMB == 0 {
			issue.Message = "node has no swap while a minimum available swap threshold is enabled"
			issue.RemediationOptions = []ClusterNodeHealthRemediationOption{
				{
					ID:          "provide_swap",
					AppliesWhen: "swap is expected on this node",
					Description: "configure swap capacity so OpenSVC can evaluate available swap against the threshold",
				},
				{
					ID:          "disable_swap_threshold",
					AppliesWhen: "the absence of swap is intentional",
					Description: "disable the OpenSVC available swap check for this node",
					Configuration: &ClusterNodeHealthConfigurationChange{
						Key:   "node.min_avail_swap_pct",
						Value: 0,
					},
				},
			}
		} else {
			issue.RemediationOptions = []ClusterNodeHealthRemediationOption{
				{
					ID:          "restore_swap_availability",
					AppliesWhen: "the threshold reflects the intended policy",
					Description: "reduce swap consumption or add swap capacity until availability is above the configured threshold",
				},
				{
					ID:          "review_swap_threshold",
					AppliesWhen: "the configured threshold does not reflect the intended swap policy",
					Description: "review node.min_avail_swap_pct and choose an operator-approved threshold",
				},
			}
		}
		issues = append(issues, issue)
	}

	if len(issues) == 0 {
		issues = append(issues, nodeIssue("overload_cause_undetermined", "node reports overload but the published thresholds and statistics do not explain it"))
	}
	return issues
}

func pointer[T any](value T) *T {
	return &value
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizedState(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func displayState(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func isProblemState(value string, problems ...string) bool {
	state := normalizedState(value)
	for _, problem := range problems {
		if state == problem {
			return true
		}
	}
	return false
}

func isNonZeroTimestamp(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return true
	}
	return !timestamp.IsZero()
}
