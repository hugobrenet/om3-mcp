package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Recorded from a two-node cluster, /api/pool?node=*.
const perNodePoolPayload = `{"kind": "PoolList", "items": [
	{"capabilities":["blk","file","roo","rox","rwo","rwx","volatile"],"free":1779224576,"head":"/dev/shm","logical_free":1779224576,"logical_size":1779224576,"logical_used":0,"name":"shm","node":"node-b","shared":false,"size":1779224576,"type":"shm","updated_at":"2026-10-09T11:24:07+02:00","used":0,"volume_count":0},
	{"capabilities":["blk","file","roo","rox","rwo","rwx"],"free":37791457280,"head":"/var/lib/opensvc/pool/directory","logical_free":37791457280,"logical_size":49292255232,"logical_used":9073401856,"name":"default","node":"node-a","shared":false,"size":49292255232,"type":"directory","updated_at":"2026-10-09T11:24:07+02:00","used":9073401856,"volume_count":2,"errors":["head is read-only"]},
	{"capabilities":["blk","file","roo","rox","rwo","rwx"],"free":37878534144,"head":"/var/lib/opensvc/pool/directory","logical_free":37878534144,"logical_size":49292255232,"logical_used":8986324992,"name":"default","node":"node-b","shared":false,"size":49292255232,"type":"directory","updated_at":"2026-10-09T11:24:07+02:00","used":8986324992,"volume_count":2}
]}`

func TestListStoragePoolsReportsDaemonUsagePerNode(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/pool", query: url.Values{"node": {"*"}}, payload: perNodePoolPayload}
	pools, err := New(client).ListStoragePools(context.Background(), ListStoragePoolsOptions{PerNode: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pools.PerNode || pools.Total != 3 || pools.Count != 3 || pools.Truncated || pools.Provenance.Source == "" {
		t.Fatalf("unexpected list %+v", pools)
	}
	first := pools.Pools[0]
	if first.Name != "default" || first.Node != "node-a" || first.Type != "directory" || first.Head != "/var/lib/opensvc/pool/directory" {
		t.Fatalf("pools are not sorted by node then name: %+v", pools.Pools)
	}
	if first.Physical != (StorageCapacity{SizeBytes: 49292255232, UsedBytes: 9073401856, FreeBytes: 37791457280}) ||
		first.Logical != (StorageCapacity{SizeBytes: 49292255232, UsedBytes: 9073401856, FreeBytes: 37791457280}) ||
		first.VolumeCount != 2 || len(first.Errors) != 1 || first.Errors[0] != "head is read-only" || first.ErrorsTruncated {
		t.Fatalf("daemon usage not preserved: %+v", first)
	}
	if pools.Pools[2].Name != "shm" || pools.Pools[2].Errors == nil || len(pools.Pools[2].Capabilities) != 7 {
		t.Fatalf("missing errors must be an empty list: %+v", pools.Pools[2])
	}
}

func TestListStoragePoolsSelectsTheClusterViewOrOneNode(t *testing.T) {
	for _, tc := range []struct {
		options ListStoragePoolsOptions
		query   url.Values
		perNode bool
	}{
		{ListStoragePoolsOptions{}, url.Values{}, false},
		{ListStoragePoolsOptions{Pool: "default"}, url.Values{"name": {"default"}}, false},
		{ListStoragePoolsOptions{Node: "node-a"}, url.Values{"node": {"node-a"}}, true},
		{ListStoragePoolsOptions{Pool: "shm", PerNode: true}, url.Values{"name": {"shm"}, "node": {"*"}}, true},
	} {
		client := &recordingJSONGetter{t: t, path: "/api/pool", query: tc.query, payload: `{"kind":"PoolList","items":[]}`}
		pools, err := New(client).ListStoragePools(context.Background(), tc.options)
		if err != nil {
			t.Fatal(err)
		}
		if pools.PerNode != tc.perNode || pools.Pools == nil || pools.Count != 0 || client.calls != 1 {
			t.Fatalf("options %+v gave %+v", tc.options, pools)
		}
	}
}

func TestListStoragePoolsRejectsSelectorsBeforeTheDaemonCall(t *testing.T) {
	for _, options := range []ListStoragePoolsOptions{
		{Node: "node-a", PerNode: true},
		{Node: "n*"}, {Node: "a,b"}, {Node: "label=value"}, {Node: ".."},
		{Pool: "p*"}, {Pool: "a,b"}, {Pool: " padded"}, {Pool: "a b"}, {Pool: strings.Repeat("p", 256)},
	} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListStoragePools(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
}

func TestListStoragePoolsBoundsTheDaemonText(t *testing.T) {
	errors := `"` + strings.Repeat("e", maxStoragePoolErrorRunes+1) + `"`
	for range maxStoragePoolErrors {
		errors += `,"x"`
	}
	payload := `{"kind":"PoolList","items":[{"name":"p","node":"","type":"t","head":"h","capabilities":[],"errors":[` + errors + `],"updated_at":"now"}]}`
	client := &recordingJSONGetter{t: t, path: "/api/pool", query: url.Values{}, payload: payload}
	pools, err := New(client).ListStoragePools(context.Background(), ListStoragePoolsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pool := pools.Pools[0]
	if len(pool.Errors) != maxStoragePoolErrors || len([]rune(pool.Errors[0])) != maxStoragePoolErrorRunes || !pool.ErrorsTruncated {
		t.Fatalf("errors not bounded: %d entries, first of %d runes", len(pool.Errors), len([]rune(pool.Errors[0])))
	}
}
