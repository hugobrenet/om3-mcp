package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodeDriversSortsDeduplicatesAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{
		t:     t,
		path:  "/api/node/name/node-a/drivers",
		query: url.Values{},
		payload: `{"kind":"DriverList","items":[
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.zfs"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"container.docker"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}},
			{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.zfs"}}
		]}`,
	}
	service := New(client)

	first, err := service.ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Limit: 2})
	if err != nil {
		t.Fatalf("list first node driver page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 3 || first.Count != 2 || !first.Truncated {
		t.Fatalf("unexpected first page metadata: %#v", first)
	}
	if first.Described || first.Drivers[0].Name != "app.simple" || first.Drivers[0].Group != "" || first.Drivers[1].Name != "container.docker" || first.NextCursor != first.Drivers[1].Name {
		t.Errorf("drivers are not sorted, deduplicated, or paginated: %#v", first)
	}

	second, err := service.ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("list second node driver page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Drivers[0].Name != "fs.zfs" {
		t.Errorf("unexpected second page: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodeDriversSupportsExplicitNodeAndEmptyList(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/drivers", query: url.Values{},
		payload: `{"kind":"DriverList","items":[]}`,
	}
	result, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a"})
	if err != nil {
		t.Fatalf("list empty node drivers: %v", err)
	}
	if result.Node != "node-a" || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Drivers == nil || result.Truncated {
		t.Errorf("unexpected empty driver list: %#v", result)
	}
}

func TestListNodeDriversRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodeDriversOptions{
		"missing node":   {},
		"local alias":    {Node: "_"},
		"node selector":  {Node: "node*"},
		"spaced node":    {Node: " node-a"},
		"invalid limit":  {Node: "node-a", Limit: maxNodeDriverLimit + 1},
		"invalid cursor": {Node: "node-a", Cursor: "bad\ncursor"},
		"long cursor":    {Node: "node-a", Cursor: strings.Repeat("x", maxNodeDriverCursorLength+1)},
		"spaced group":   {Node: "node-a", Group: "fs ext4"},
		"upper feature":  {Node: "node-a", Feature: "Resize"},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodeDrivers(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodeDriversRejectsMalformedDaemonData(t *testing.T) {
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"DriverList","items":[{"kind":"DriversItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}}]}`,
		"unexpected node":      `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-b"},"data":{"name":"app.simple"}}]}`,
		"empty name":           `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":""}}]}`,
		"control in name":      `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"bad\nname"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{
				t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: payload,
			}
			if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodeDriversRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/drivers", query: url.Values{},
		payload: `{"kind":"DriverList","items":[{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"app.simple"}}]}`,
	}
	if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Cursor: "disk.missing"}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}

// The catalog a daemon describes: each driver with its doc, features,
// availability and grants, the vocabulary of the features, and the filters.
const describedNodeDrivers = `{"kind":"DriverList",
	"features":[{"name":"provision","doc":"The provision makes it."},{"name":"resize","doc":"Grown in place."},{"name":"start","doc":"Brings it up."}],
	"items":[
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.ext4","group":"fs","type":"ext4","doc":"Mounts ext4.","features":["provision","start","resize"],"selects":[],"aliasOf":"","available":true,"grant":"root","provisionGrant":""}},
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.flag","group":"fs","type":"flag","doc":"Holds a flag.","features":["provision","start"],"selects":[],"aliasOf":"","available":true,"grant":"","provisionGrant":""}},
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"container.oci","group":"container","type":"oci","doc":"Runs an OCI container.","features":["start"],"selects":["container.docker","container.podman"],"aliasOf":"","available":true,"grant":"","provisionGrant":""}},
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"container.podman","group":"container","type":"podman","doc":"Runs podman.","features":["start"],"selects":[],"aliasOf":"","available":false,"grant":"","provisionGrant":""}},
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"disk.lvm","group":"disk","type":"lvm","doc":"Activates a VG.","features":["provision","start","resize"],"selects":[],"aliasOf":"disk.vg","available":true,"grant":"root","provisionGrant":""}},
	{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"disk.disk","group":"disk","type":"disk","doc":"Allocates a disk.","features":["provision","start"],"selects":[],"aliasOf":"","available":true,"grant":"","provisionGrant":"root"}}
]}`

func TestListNodeDriversGivesTheCatalog(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: describedNodeDrivers}
	result, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Described || len(result.Features) != 3 || result.Features[1].Name != "resize" || result.Total != 6 {
		t.Fatalf("result = %#v", result)
	}
	byName := map[string]NodeDriver{}
	for _, driver := range result.Drivers {
		byName[driver.Name] = driver
	}
	oci, lvm, disk := byName["container.oci"], byName["disk.lvm"], byName["disk.disk"]
	if strings.Join(oci.Selects, " ") != "container.docker container.podman" || oci.Available == nil || !*oci.Available || oci.Grant != "" {
		t.Errorf("container.oci = %#v", oci)
	}
	if lvm.AliasOf != "disk.vg" || lvm.Grant != "root" || lvm.Group != "disk" || lvm.Type != "lvm" || lvm.Doc != "Activates a VG." {
		t.Errorf("disk.lvm = %#v", lvm)
	}
	if disk.ProvisionGrant != "root" || disk.Grant != "" {
		t.Errorf("disk.disk = %#v", disk)
	}
}

func TestListNodeDriversFilters(t *testing.T) {
	no := false
	for name, tc := range map[string]struct {
		options ListNodeDriversOptions
		want    string
	}{
		"group":         {ListNodeDriversOptions{Group: "disk"}, "disk.disk disk.lvm"},
		"feature":       {ListNodeDriversOptions{Feature: "resize"}, "disk.lvm fs.ext4"},
		"unavailable":   {ListNodeDriversOptions{Available: &no}, "container.podman"},
		"group feature": {ListNodeDriversOptions{Group: "fs", Feature: "resize"}, "fs.ext4"},
	} {
		client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: describedNodeDrivers}
		tc.options.Node = "node-a"
		result, err := New(client).ListNodeDrivers(context.Background(), tc.options)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var names []string
		for _, driver := range result.Drivers {
			names = append(names, driver.Name)
		}
		if got := strings.Join(names, " "); got != tc.want || result.Total != len(names) {
			t.Errorf("%s: drivers %q, total %d, want %q", name, got, result.Total, tc.want)
		}
	}
}

// A feature the vocabulary does not hold is refused with the vocabulary; an
// older daemon, names only, takes the group filter only.
func TestListNodeDriversRefusesWhatTheDaemonCannotTell(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: describedNodeDrivers}
	if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Feature: "explode"}); err == nil || !strings.Contains(err.Error(), "provision, resize, start") {
		t.Fatalf("unknown feature: %v", err)
	}
	older := `{"kind":"DriverList","items":[
		{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"fs.ext4"}},
		{"kind":"DriverItem","meta":{"node":"node-a"},"data":{"name":"disk.lvm"}}]}`
	yes := true
	client = &recordingJSONGetter{t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: older}
	if _, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Available: &yes}); err == nil || !strings.Contains(err.Error(), "names only") {
		t.Fatalf("availability on an older daemon: %v", err)
	}
	client = &recordingJSONGetter{t: t, path: "/api/node/name/node-a/drivers", query: url.Values{}, payload: older}
	result, err := New(client).ListNodeDrivers(context.Background(), ListNodeDriversOptions{Node: "node-a", Group: "fs"})
	if err != nil || result.Described || result.Count != 1 || result.Drivers[0].Name != "fs.ext4" || result.Drivers[0].Group != "" {
		t.Fatalf("group on an older daemon: %#v, %v", result, err)
	}
}
