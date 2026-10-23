package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"time"
)

const (
	maxSyncSnapshots       = 1000
	maxSyncSnapshotMembers = 100
	maxSyncSnapshotText    = 1024
)

// syncRID is a sync resource selector: the sync group, or one sync resource.
var syncRID = regexp.MustCompile(`^sync(#[A-Za-z0-9_.-]+)?$`)

type ListInstanceSyncSnapshotsOptions struct {
	Path string
	Node string
	// RID is sync, every sync resource, or one of them; empty, sync.
	RID string
}

type SyncSnapshot struct {
	RID string `json:"rid" jsonschema:"the sync resource holding the snapshot"`
	// ID is the session of the sync update that took the snapshot.
	ID        string   `json:"id" jsonschema:"the identifier of the snapshot, the session of the sync update that took it; a restore, not offered, would take it"`
	CreatedAt string   `json:"created_at" jsonschema:"when the snapshot was taken"`
	Node      string   `json:"node" jsonschema:"the node that took the snapshot"`
	Members   []string `json:"members" jsonschema:"what the snapshot holds, such as fs#1 or config"`
	Location  string   `json:"location" jsonschema:"where the snapshot is stored, as the sync resource names it; untrusted text"`
}

type InstanceSyncSnapshotList struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Node       string                 `json:"node" jsonschema:"the node whose instance was read"`
	RIDFilter  string                 `json:"rid_filter" jsonschema:"the sync resources read: sync for all of them"`
	Total      int                    `json:"total" jsonschema:"the number of snapshots the daemon listed"`
	Count      int                    `json:"count" jsonschema:"the number of snapshots returned"`
	Snapshots  []SyncSnapshot         `json:"snapshots" jsonschema:"the snapshots, the most recent first; empty for a sync resource that keeps none: only sync.store lists snapshots"`
	Truncated  bool                   `json:"truncated" jsonschema:"whether snapshots were left out after 1000"`
}

type daemonSyncSnapshotList struct {
	Items []struct {
		Meta struct {
			Node   string `json:"node"`
			Object string `json:"object"`
		} `json:"meta"`
		Data struct {
			RID       string   `json:"rid"`
			ID        string   `json:"id"`
			CreatedAt string   `json:"created_at"`
			Node      string   `json:"node"`
			Members   []string `json:"members"`
			Location  string   `json:"location"`
		} `json:"data"`
	} `json:"items"`
}

// ListInstanceSyncSnapshots lists the snapshots the sync resources of the
// instance of one object on one node hold: the restorable points a
// sync.store keeps; the other sync drivers list none.
func (s *Service) ListInstanceSyncSnapshots(ctx context.Context, options ListInstanceSyncSnapshotsOptions) (InstanceSyncSnapshotList, error) {
	reference, err := validateInstanceTarget(options.Path, options.Node, "list sync snapshots", []string{"svc", "vol"})
	if err != nil {
		return InstanceSyncSnapshotList{}, err
	}
	rid := options.RID
	if rid == "" {
		rid = "sync"
	}
	if !syncRID.MatchString(rid) {
		return InstanceSyncSnapshotList{}, fmt.Errorf("rid must be sync, every sync resource, or one exact sync resource id, such as sync#1")
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/instance/path/%s/%s/%s/sync/snapshots", options.Node, reference.Namespace, reference.Kind, reference.Name)
	var response daemonSyncSnapshotList
	if err := s.client.GetJSON(ctx, endpoint, url.Values{"rid": {rid}}, &response); err != nil {
		return InstanceSyncSnapshotList{}, fmt.Errorf("list sync snapshots of %s on %s: %w", reference.Path, options.Node, err)
	}
	snapshots := make([]SyncSnapshot, 0, len(response.Items))
	for _, item := range response.Items {
		if item.Meta.Object != reference.Path {
			return InstanceSyncSnapshotList{}, fmt.Errorf("list sync snapshots of %s: the daemon returned a snapshot of %q", reference.Path, item.Meta.Object)
		}
		members := item.Data.Members
		if len(members) > maxSyncSnapshotMembers {
			members = members[:maxSyncSnapshotMembers]
		}
		bounded := make([]string, 0, len(members))
		for _, member := range members {
			text, _ := cutUTF8(member, maxSyncSnapshotText)
			bounded = append(bounded, text)
		}
		location, _ := cutUTF8(item.Data.Location, maxSyncSnapshotText)
		snapshots = append(snapshots, SyncSnapshot{
			RID: item.Data.RID, ID: item.Data.ID, CreatedAt: item.Data.CreatedAt, Node: item.Data.Node, Members: bounded, Location: location,
		})
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, snapshots[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, snapshots[j].CreatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.After(right)
		}
		if snapshots[i].RID != snapshots[j].RID {
			return snapshots[i].RID < snapshots[j].RID
		}
		return snapshots[i].ID < snapshots[j].ID
	})
	result := InstanceSyncSnapshotList{Object: reference, Node: options.Node, RIDFilter: rid, Total: len(snapshots), Snapshots: snapshots}
	if len(snapshots) > maxSyncSnapshots {
		result.Snapshots, result.Truncated = snapshots[:maxSyncSnapshots], true
	}
	result.Count = len(result.Snapshots)
	result.Provenance = s.newProvenance()
	return result, nil
}
