package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const (
	maxStoragePools              = 512
	maxStoragePoolCapabilities   = 32
	maxStoragePoolErrors         = 10
	maxStoragePoolErrorRunes     = 512
	maxStoragePoolTextRunes      = 1024
	maxStoragePoolNameCharacters = 255
)

type ListStoragePoolsOptions struct {
	Pool    string
	Node    string
	PerNode bool
}

type StoragePoolList struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	PoolFilter string        `json:"pool_filter,omitempty" jsonschema:"the optional exact pool name used for the daemon request"`
	NodeFilter string        `json:"node_filter,omitempty" jsonschema:"the optional exact node name used for the daemon request"`
	PerNode    bool          `json:"per_node" jsonschema:"whether the daemon returned one record per node and pool rather than one cluster record per pool"`
	Total      int           `json:"total" jsonschema:"the number of pool records returned by the daemon"`
	Count      int           `json:"count" jsonschema:"the number of pool records included in this result"`
	Pools      []StoragePool `json:"pools" jsonschema:"the pool records sorted by node then pool name"`
	Truncated  bool          `json:"truncated" jsonschema:"whether pool records were omitted after the 512-record limit"`
}

type StoragePool struct {
	Name                  string          `json:"name" jsonschema:"the pool name"`
	Node                  string          `json:"node,omitempty" jsonschema:"the node of this record; empty in the cluster view, which merges the nodes"`
	Type                  string          `json:"type" jsonschema:"the pool driver type reported by OpenSVC"`
	Head                  string          `json:"head" jsonschema:"the storage location the pool driver reports, such as a directory or a device"`
	Capabilities          []string        `json:"capabilities" jsonschema:"the volume access modes and features the pool supports"`
	CapabilitiesTruncated bool            `json:"capabilities_truncated" jsonschema:"whether capabilities were omitted after 32 entries"`
	Shared                bool            `json:"shared" jsonschema:"whether every node sees the same storage behind the pool"`
	Physical              StorageCapacity `json:"physical" jsonschema:"the storage behind the pool; the cluster view of a non-shared pool sums the nodes"`
	Logical               StorageCapacity `json:"logical" jsonschema:"the capacity the pool hands out to volumes, counting a volume once however many nodes hold a copy; free_bytes is what volumes can still claim. The cluster view of a non-shared pool keeps the three figures of the node with the least room left"`
	VolumeCount           int             `json:"volume_count" jsonschema:"the number of volumes the pool serves"`
	Errors                []string        `json:"errors" jsonschema:"the pool errors reported by OpenSVC, each limited to 512 characters; the cluster view may hold the errors of one node only, use per_node for every node"`
	ErrorsTruncated       bool            `json:"errors_truncated" jsonschema:"whether errors were omitted after 10 entries or shortened"`
	UpdatedAt             string          `json:"updated_at" jsonschema:"the daemon timestamp of this pool usage"`
}

type StorageCapacity struct {
	SizeBytes int64 `json:"size_bytes" jsonschema:"the size in bytes"`
	UsedBytes int64 `json:"used_bytes" jsonschema:"the used bytes"`
	FreeBytes int64 `json:"free_bytes" jsonschema:"the free bytes"`
}

type daemonPoolList struct {
	Items []daemonPool `json:"items"`
}

type daemonPool struct {
	Name         string   `json:"name"`
	Node         string   `json:"node"`
	Type         string   `json:"type"`
	Head         string   `json:"head"`
	Capabilities []string `json:"capabilities"`
	Shared       bool     `json:"shared"`
	Size         int64    `json:"size"`
	Used         int64    `json:"used"`
	Free         int64    `json:"free"`
	LogicalSize  int64    `json:"logical_size"`
	LogicalUsed  int64    `json:"logical_used"`
	LogicalFree  int64    `json:"logical_free"`
	VolumeCount  int      `json:"volume_count"`
	Errors       []string `json:"errors"`
	UpdatedAt    string   `json:"updated_at"`
}

// ListStoragePools reads the pool usage the daemon reports: one record per
// pool for the cluster, or one per node and pool. It reports facts only.
func (s *Service) ListStoragePools(ctx context.Context, options ListStoragePoolsOptions) (StoragePoolList, error) {
	pool, err := validatePoolName(options.Pool)
	if err != nil {
		return StoragePoolList{}, err
	}
	node := strings.TrimSpace(options.Node)
	if node != "" && options.PerNode {
		return StoragePoolList{}, fmt.Errorf("node and per_node are mutually exclusive")
	}
	if node != "" && !validExactNodeName(node) {
		return StoragePoolList{}, fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}

	query := url.Values{}
	if pool != "" {
		query.Set("name", pool)
	}
	switch {
	case node != "":
		query.Set("node", node)
	case options.PerNode:
		query.Set("node", "*")
	}
	var response daemonPoolList
	if err := s.client.GetJSON(ctx, "/api/pool", query, &response); err != nil {
		return StoragePoolList{}, fmt.Errorf("list storage pools: %w", err)
	}

	pools := make([]StoragePool, 0, min(len(response.Items), maxStoragePools))
	for _, item := range response.Items {
		pools = append(pools, projectStoragePool(item))
	}
	sort.SliceStable(pools, func(i, j int) bool {
		if pools[i].Node != pools[j].Node {
			return pools[i].Node < pools[j].Node
		}
		return pools[i].Name < pools[j].Name
	})
	total := len(pools)
	if total > maxStoragePools {
		pools = pools[:maxStoragePools]
	}
	return StoragePoolList{
		Provenance: s.newProvenance(),
		PoolFilter: pool,
		NodeFilter: node,
		PerNode:    node != "" || options.PerNode,
		Total:      total,
		Count:      len(pools),
		Pools:      pools,
		Truncated:  total > len(pools),
	}, nil
}

func projectStoragePool(item daemonPool) StoragePool {
	capabilities, capabilitiesTruncated := boundedStrings(item.Capabilities, maxStoragePoolCapabilities)
	for i, capability := range capabilities {
		capabilities[i] = boundedPoolText(capability)
	}
	errors, errorsTruncated := boundedStrings(item.Errors, maxStoragePoolErrors)
	for i, message := range errors {
		var shortened bool
		errors[i], shortened = boundedRunes(message, maxStoragePoolErrorRunes)
		errorsTruncated = errorsTruncated || shortened
	}
	return StoragePool{
		Name:                  boundedPoolText(item.Name),
		Node:                  boundedPoolText(item.Node),
		Type:                  boundedPoolText(item.Type),
		Head:                  boundedPoolText(item.Head),
		Capabilities:          capabilities,
		CapabilitiesTruncated: capabilitiesTruncated,
		Shared:                item.Shared,
		Physical:              StorageCapacity{SizeBytes: item.Size, UsedBytes: item.Used, FreeBytes: item.Free},
		Logical:               StorageCapacity{SizeBytes: item.LogicalSize, UsedBytes: item.LogicalUsed, FreeBytes: item.LogicalFree},
		VolumeCount:           item.VolumeCount,
		Errors:                errors,
		ErrorsTruncated:       errorsTruncated,
		UpdatedAt:             boundedPoolText(item.UpdatedAt),
	}
}

// boundedPoolText keeps a daemon text field within the pool text limit.
func boundedPoolText(value string) string {
	bounded, _ := boundedRunes(value, maxStoragePoolTextRunes)
	return bounded
}

// validatePoolName accepts an empty name or one exact pool name: the daemon
// matches the name as given, so wildcards and separators are refused.
func validatePoolName(value string) (string, error) {
	pool := strings.TrimSpace(value)
	if pool == "" {
		return "", nil
	}
	if pool != value || len(pool) > maxStoragePoolNameCharacters || strings.ContainsAny(pool, "*?[],=/\\") ||
		strings.ContainsFunc(pool, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("pool must be one exact OpenSVC pool name of at most %d characters", maxStoragePoolNameCharacters)
	}
	return pool, nil
}
