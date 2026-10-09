package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Recorded on a node with multipath iSCSI LUNs and local disks.
const nodeDiskPayload = `{"kind": "DiskList", "items": [
	{"kind":"DiskItem","meta":{"node":"node-a"},"data":{"id":"vdb","devpath":"/dev/vdb","size":21474836480,"vendor":"0x1af4","model":"","type":"disk","regions":[{"id":"vdb","devpath":"/dev/vdb","object":"","group":"","size":21474836480}]}},
	{"kind":"DiskItem","meta":{"node":"node-a"},"data":{"id":"36001405105890c7c2304921aea6236d2","devpath":"/dev/mapper/36001405105890c7c2304921aea6236d2","size":1073741824,"vendor":"LIO-ORG ","model":"c46_disk9","type":"mpath","regions":[{"id":"36001405105890c7c2304921aea6236d2","devpath":"/dev/mapper/36001405105890c7c2304921aea6236d2","object":"prod/svc/db","group":"vg1","size":1073741824}]}},
	{"kind":"DiskItem","meta":{"node":"node-a"},"data":{"id":"360014058c47036759364d489f302f35b","devpath":"/dev/mapper/360014058c47036759364d489f302f35b","size":1073741824,"vendor":"LIO-ORG ","model":"c46_disk6","type":"mpath","regions":[{"id":"360014058c47036759364d489f302f35b","devpath":"/dev/mapper/360014058c47036759364d489f302f35b","object":"","group":"","size":1073741824}]}},
	{"kind":"DiskItem","meta":{"node":"node-a"},"data":{"id":"sr0","devpath":"/dev/sr0","size":385024,"vendor":"QEMU    ","model":"QEMU DVD-ROM","type":"rom","regions":[]}}
]}`

func TestListNodeDisksReportsInventoryAndClaimsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/disk", query: url.Values{}, payload: nodeDiskPayload}
	service := New(client)
	first, err := service.ListNodeDisks(context.Background(), ListNodeDisksOptions{Node: "node-a", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 4 || first.Total != 4 || first.Count != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page %+v", first)
	}
	if first.Disks[0].ID != "vdb" || first.Disks[0].Type != "disk" || first.Disks[1].Type != "mpath" {
		t.Fatalf("disks not sorted by type then id: %+v", first.Disks)
	}
	claimed := first.Disks[1]
	if claimed.ID != "36001405105890c7c2304921aea6236d2" || !claimed.Claimed || claimed.Vendor != "LIO-ORG " || claimed.SizeBytes != 1073741824 ||
		len(claimed.Regions) != 1 || claimed.Regions[0].Object != "prod/svc/db" || claimed.Regions[0].Group != "vg1" {
		t.Fatalf("disk facts or claims not preserved: %+v", claimed)
	}
	second, err := service.ListNodeDisks(context.Background(), ListNodeDisksOptions{Node: "node-a", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.Count != 2 || second.Truncated || second.Disks[1].ID != "sr0" || second.Disks[1].Regions == nil || second.Disks[0].Claimed {
		t.Fatalf("unexpected second page %+v", second)
	}
}

func TestListNodeDisksFilters(t *testing.T) {
	for _, tc := range []struct {
		options ListNodeDisksOptions
		ids     []string
	}{
		{ListNodeDisksOptions{Type: "mpath"}, []string{"36001405105890c7c2304921aea6236d2", "360014058c47036759364d489f302f35b"}},
		{ListNodeDisksOptions{ClaimedOnly: true}, []string{"36001405105890c7c2304921aea6236d2"}},
		{ListNodeDisksOptions{Type: "mpath", UnclaimedOnly: true}, []string{"360014058c47036759364d489f302f35b"}},
	} {
		client := &recordingJSONGetter{t: t, path: "/api/node/name/_/system/disk", query: url.Values{}, payload: nodeDiskPayload}
		disks, err := New(client).ListNodeDisks(context.Background(), tc.options)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, disk := range disks.Disks {
			ids = append(ids, disk.ID)
		}
		if strings.Join(ids, ",") != strings.Join(tc.ids, ",") || disks.Node != "node-a" || disks.ReportedTotal != 4 {
			t.Fatalf("options %+v gave %v (node %q)", tc.options, ids, disks.Node)
		}
	}
}

func TestListNodeDisksRejectsInvalidInputBeforeTheDaemonCall(t *testing.T) {
	for _, options := range []ListNodeDisksOptions{
		{Node: "n*"}, {Node: ".."}, {Type: " mpath"}, {Type: strings.Repeat("t", 65)},
		{ClaimedOnly: true, UnclaimedOnly: true}, {Limit: -1}, {Limit: 201}, {Cursor: "not base64!"},
	} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListNodeDisks(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
}

func TestListNodeDisksRejectsAnotherNodeOrKind(t *testing.T) {
	for _, payload := range []string{
		`{"kind":"DiskList","items":[{"kind":"DiskItem","meta":{"node":"node-b"},"data":{"id":"x","type":"disk"}}]}`,
		`{"kind":"DiskList","items":[{"kind":"Other","meta":{"node":"node-a"},"data":{"id":"x","type":"disk"}}]}`,
		`{"kind":"PackageList","items":[]}`,
	} {
		client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/disk", query: url.Values{}, payload: payload}
		if _, err := New(client).ListNodeDisks(context.Background(), ListNodeDisksOptions{Node: "node-a"}); err == nil {
			t.Fatalf("payload accepted: %s", payload)
		}
	}
}
