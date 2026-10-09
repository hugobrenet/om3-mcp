package core

import (
	"context"
	"net/url"
	"testing"
)

const poolVolumePayload = `{"kind": "PoolVolumeList", "items": [
	{"path":"test/vol/b","pool":"default","size":1073741824,"is_orphan":true,"children":[]},
	{"path":"test/vol/a","pool":"default","size":104857600,"is_orphan":false,"children":["test/svc/app"],"charges":{"shm":4096,"fast":8192}},
	{"path":"test/vol/c","pool":"shm","size":2048,"is_orphan":false,"children":["test/svc/web","test/svc/api"]}
]}`

func TestListPoolVolumesReportsUsersAndChargesAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/pool/volume", query: url.Values{}, payload: poolVolumePayload}
	service := New(client)
	first, err := service.ListPoolVolumes(context.Background(), ListPoolVolumesOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || first.Count != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page %+v", first)
	}
	a := first.Volumes[0]
	if a.Path != "test/vol/a" || a.Pool != "default" || a.SizeBytes != 104857600 || a.IsOrphan ||
		len(a.Children) != 1 || a.Children[0] != "test/svc/app" ||
		len(a.Charges) != 2 || a.Charges[0] != (PoolCharge{Pool: "fast", Bytes: 8192}) || a.Charges[1] != (PoolCharge{Pool: "shm", Bytes: 4096}) {
		t.Fatalf("volume facts not preserved or not sorted: %+v", a)
	}
	if first.Volumes[1].Path != "test/vol/b" || !first.Volumes[1].IsOrphan || first.Volumes[1].Charges == nil {
		t.Fatalf("volumes not sorted by pool then path, or missing charges not empty: %+v", first.Volumes)
	}
	second, err := service.ListPoolVolumes(context.Background(), ListPoolVolumesOptions{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.Count != 1 || second.Truncated || second.Volumes[0].Path != "test/vol/c" || len(second.Volumes[0].Children) != 2 {
		t.Fatalf("unexpected second page %+v", second)
	}
}

func TestListPoolVolumesFiltersPoolAndOrphans(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/pool/volume", query: url.Values{"name": {"default"}}, payload: poolVolumePayload}
	volumes, err := New(client).ListPoolVolumes(context.Background(), ListPoolVolumesOptions{Pool: "default", OrphansOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if volumes.PoolFilter != "default" || !volumes.OrphansOnly || volumes.Total != 1 || volumes.Volumes[0].Path != "test/vol/b" {
		t.Fatalf("filters not applied %+v", volumes)
	}
}

func TestListPoolVolumesRejectsInvalidInputBeforeTheDaemonCall(t *testing.T) {
	for _, options := range []ListPoolVolumesOptions{
		{Pool: "p*"}, {Limit: -1}, {Limit: maxListPoolVolumesLimit + 1}, {Cursor: "not base64!"},
	} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListPoolVolumes(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
}

func TestListPoolVolumesWithoutVolumes(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/pool/volume", query: url.Values{}, payload: `{"kind":"PoolVolumeList","items":[]}`}
	volumes, err := New(client).ListPoolVolumes(context.Background(), ListPoolVolumesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if volumes.Total != 0 || volumes.Volumes == nil || volumes.Truncated {
		t.Fatalf("unexpected empty list %+v", volumes)
	}
}
