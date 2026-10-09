package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
)

const (
	defaultListPoolVolumesLimit = 100
	maxListPoolVolumesLimit     = 200
	maxPoolVolumeChildren       = 32
	maxPoolVolumeCharges        = 32
)

type ListPoolVolumesOptions struct {
	Pool        string
	OrphansOnly bool
	Limit       int
	Cursor      string
}

type PoolVolumeList struct {
	Provenance  Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	PoolFilter  string       `json:"pool_filter,omitempty" jsonschema:"the optional exact pool name used for the daemon request"`
	OrphansOnly bool         `json:"orphans_only" jsonschema:"whether only volumes no object uses are listed"`
	Total       int          `json:"total" jsonschema:"the number of volumes matching the filters"`
	Count       int          `json:"count" jsonschema:"the number of volumes returned in this page"`
	Volumes     []PoolVolume `json:"volumes" jsonschema:"the volumes sorted by pool then volume path"`
	NextCursor  string       `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass to retrieve the next page"`
	Truncated   bool         `json:"truncated" jsonschema:"whether more matching volumes remain after this page"`
}

type PoolVolume struct {
	Path              string       `json:"path" jsonschema:"the OpenSVC volume object path"`
	Pool              string       `json:"pool" jsonschema:"the pool that served the volume"`
	SizeBytes         int64        `json:"size_bytes" jsonschema:"the size the pool served the volume with"`
	IsOrphan          bool         `json:"is_orphan" jsonschema:"whether no object uses the volume"`
	Children          []string     `json:"children" jsonschema:"the object paths using the volume, at most 32"`
	ChildrenTruncated bool         `json:"children_truncated" jsonschema:"whether children were omitted after 32 entries"`
	Charges           []PoolCharge `json:"charges" jsonschema:"what the volume takes of pools other than the one that served it, sorted by pool"`
	ChargesTruncated  bool         `json:"charges_truncated" jsonschema:"whether charges were omitted after 32 entries"`
}

type PoolCharge struct {
	Pool  string `json:"pool" jsonschema:"the other pool the volume takes storage from"`
	Bytes int64  `json:"bytes" jsonschema:"the bytes the volume takes of that pool"`
}

type daemonPoolVolumeList struct {
	Items []daemonPoolVolume `json:"items"`
}

type daemonPoolVolume struct {
	Path     string           `json:"path"`
	Pool     string           `json:"pool"`
	Size     int64            `json:"size"`
	IsOrphan bool             `json:"is_orphan"`
	Children []string         `json:"children"`
	Charges  map[string]int64 `json:"charges"`
}

// ListPoolVolumes reads the volumes the pools serve, the objects using them
// and the storage they take of other pools. It reports facts only.
func (s *Service) ListPoolVolumes(ctx context.Context, options ListPoolVolumesOptions) (PoolVolumeList, error) {
	pool, err := validatePoolName(options.Pool)
	if err != nil {
		return PoolVolumeList{}, err
	}
	limit := options.Limit
	if limit == 0 {
		limit = defaultListPoolVolumesLimit
	}
	if limit < 1 || limit > maxListPoolVolumesLimit {
		return PoolVolumeList{}, fmt.Errorf("pool volume list limit must be between 1 and %d", maxListPoolVolumesLimit)
	}
	cursorKey, err := decodePoolVolumeCursor(options.Cursor)
	if err != nil {
		return PoolVolumeList{}, err
	}

	query := url.Values{}
	if pool != "" {
		query.Set("name", pool)
	}
	var response daemonPoolVolumeList
	if err := s.client.GetJSON(ctx, "/api/pool/volume", query, &response); err != nil {
		return PoolVolumeList{}, fmt.Errorf("list pool volumes: %w", err)
	}

	volumes := make([]PoolVolume, 0, len(response.Items))
	for _, item := range response.Items {
		if options.OrphansOnly && !item.IsOrphan {
			continue
		}
		volumes = append(volumes, projectPoolVolume(item))
	}
	sort.Slice(volumes, func(i, j int) bool {
		return poolVolumeSortKey(volumes[i]) < poolVolumeSortKey(volumes[j])
	})
	start := sort.Search(len(volumes), func(i int) bool {
		return poolVolumeSortKey(volumes[i]) > cursorKey
	})
	end := min(start+limit, len(volumes))
	page := append([]PoolVolume{}, volumes[start:end]...)
	result := PoolVolumeList{
		PoolFilter:  pool,
		OrphansOnly: options.OrphansOnly,
		Total:       len(volumes),
		Count:       len(page),
		Volumes:     page,
		Truncated:   end < len(volumes),
	}
	if result.Truncated {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(poolVolumeSortKey(page[len(page)-1])))
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

func projectPoolVolume(item daemonPoolVolume) PoolVolume {
	children, childrenTruncated := boundedStrings(item.Children, maxPoolVolumeChildren)
	for i, child := range children {
		children[i] = boundedPoolText(child)
	}
	pools := make([]string, 0, len(item.Charges))
	for name := range item.Charges {
		pools = append(pools, name)
	}
	sort.Strings(pools)
	charges := make([]PoolCharge, 0, min(len(pools), maxPoolVolumeCharges))
	for _, name := range pools[:min(len(pools), maxPoolVolumeCharges)] {
		charges = append(charges, PoolCharge{Pool: boundedPoolText(name), Bytes: item.Charges[name]})
	}
	return PoolVolume{
		Path:              boundedPoolText(item.Path),
		Pool:              boundedPoolText(item.Pool),
		SizeBytes:         item.Size,
		IsOrphan:          item.IsOrphan,
		Children:          children,
		ChildrenTruncated: childrenTruncated,
		Charges:           charges,
		ChargesTruncated:  len(pools) > len(charges),
	}
}

func poolVolumeSortKey(volume PoolVolume) string {
	return volume.Pool + "\x00" + volume.Path
}

func decodePoolVolumeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) > 1024 {
		return "", fmt.Errorf("pool volume cursor exceeds 1024 characters")
	}
	value, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("invalid pool volume cursor")
	}
	return string(value), nil
}
