package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestListNodePackagesFiltersSortsAndPaginatesDuplicates(t *testing.T) {
	payload := `{"kind":"PackageList","items":[
		{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"zlib1g","version":"1:1.3.dfsg+really1.3.1-1+b1","arch":"amd64","type":"deb","installedat":"2026-09-01T10:00:00+02:00","sig":""}},
		{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"opensvc-server","version":"3.0.0~rc30","arch":"amd64","type":"deb","installedat":"2026-09-10T18:45:04.148567787+02:00","sig":""}},
		{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"opensvc-agent","version":"3.0.0","arch":"amd64","type":"rpm","installedat":"2026-09-10T18:00:00Z","sig":"key-id"}},
		{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"opensvc-client","version":"3.0.0~rc30","arch":"amd64","type":"deb","installedat":"2026-09-10T10:58:38.650154166+02:00","sig":""}},
		{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"opensvc-server","version":"3.0.0~rc30","arch":"amd64","type":"deb","installedat":"2026-09-10T18:45:04.148567787+02:00","sig":""}}
	]}`
	client := &recordingJSONGetter{t: t, path: "/api/node/name/_/system/package", query: url.Values{}, payload: payload}
	service := New(client)
	options := ListNodePackagesOptions{
		Names: []string{"curl"}, NamePrefixes: []string{"opensvc-"}, Types: []string{"deb"},
		Architectures: []string{"amd64"}, Limit: 2,
	}

	first, err := service.ListNodePackages(context.Background(), options)
	if err != nil {
		t.Fatalf("list first node package page: %v", err)
	}
	if first.Node != "node-a" || first.ReportedTotal != 5 || first.Total != 3 || first.Count != 2 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	if first.Packages[0].Name != "opensvc-client" || first.Packages[1].Name != "opensvc-server" {
		t.Errorf("packages were not sorted as expected: %#v", first.Packages)
	}

	options.Cursor = first.NextCursor
	second, err := service.ListNodePackages(context.Background(), options)
	if err != nil {
		t.Fatalf("list second node package page: %v", err)
	}
	if second.Count != 1 || second.Truncated || second.NextCursor != "" || second.Packages[0] != first.Packages[1] {
		t.Errorf("duplicate package entry was not preserved: %#v", second)
	}
	if client.calls != 2 {
		t.Errorf("got %d daemon calls, want 2", client.calls)
	}
}

func TestListNodePackagesCombinesExactNamesAndPrefixesWithOR(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/node-a/system/package", query: url.Values{},
		payload: `{"kind":"PackageList","items":[
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":"8.14.1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}},
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"opensvc-server","version":"3.0.0","arch":"amd64","type":"deb","installedat":"2026-09-10T18:45:04Z","sig":""}},
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"jq","version":"1.7.1","arch":"amd64","type":"deb","installedat":"2026-09-23T17:01:19Z","sig":""}}
		]}`,
	}
	result, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{
		Node: "node-a", Names: []string{"curl"}, NamePrefixes: []string{"opensvc-"},
	})
	if err != nil {
		t.Fatalf("filter package names: %v", err)
	}
	if result.Total != 2 || result.Packages[0].Name != "curl" || result.Packages[1].Name != "opensvc-server" {
		t.Errorf("unexpected name filter result: %#v", result)
	}
}

func TestListNodePackagesPreservesEmptySourceFacts(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/package", query: url.Values{},
		payload: `{"kind":"PackageList","items":[
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"core","version":"16-2.61.4 rev 17200","arch":"","type":"snap","installedat":"0001-01-01T00:00:00Z","sig":""}},
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"com.example.tool","version":"","arch":"amd64","type":"","installedat":"0001-01-01T00:00:00Z","sig":""}}
		]}`,
	}
	result, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{})
	if err != nil {
		t.Fatalf("list package with empty optional facts: %v", err)
	}
	if result.Packages[0].Name != "com.example.tool" || result.Packages[0].Version != "" || result.Packages[0].Type != "" ||
		result.Packages[1].Name != "core" || result.Packages[1].Architecture != "" || result.Packages[1].InstalledAt != "0001-01-01T00:00:00Z" || result.Packages[1].Signature != "" {
		t.Errorf("source package facts were not preserved: %#v", result.Packages)
	}
	withoutType, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{Types: []string{""}})
	if err != nil || withoutType.Total != 1 || withoutType.Packages[0].Name != "com.example.tool" {
		t.Errorf("empty type filter was not preserved: result=%#v error=%v", withoutType, err)
	}
	withoutArchitecture, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{Architectures: []string{""}})
	if err != nil || withoutArchitecture.Total != 1 || withoutArchitecture.Packages[0].Name != "core" {
		t.Errorf("empty architecture filter was not preserved: result=%#v error=%v", withoutArchitecture, err)
	}
}

func TestListNodePackagesReturnsEmptyNonNilList(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/_/system/package", query: url.Values{}, payload: `{"kind":"PackageList","items":[]}`}
	result, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{})
	if err != nil {
		t.Fatalf("list empty node packages: %v", err)
	}
	if result.Node != localDaemonNodeAlias || result.ReportedTotal != 0 || result.Total != 0 || result.Count != 0 || result.Packages == nil || result.Truncated {
		t.Errorf("unexpected empty package list: %#v", result)
	}
}

func TestListNodePackagesRejectsInvalidInputsBeforeCallingDaemon(t *testing.T) {
	tests := map[string]ListNodePackagesOptions{
		"node selector":        {Node: "node*"},
		"empty name":           {Names: []string{""}},
		"spaced prefix":        {NamePrefixes: []string{" opensvc"}},
		"too many types":       {Types: make([]string, maxNodePackageFilters+1)},
		"control architecture": {Architectures: []string{"amd64\n"}},
		"invalid limit":        {Limit: maxNodePackageLimit + 1},
		"invalid cursor":       {Cursor: "bad\ncursor"},
		"long cursor":          {Cursor: strings.Repeat("x", maxNodePackageCursorRunes+1)},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t}
			if _, err := New(client).ListNodePackages(context.Background(), options); err == nil {
				t.Fatal("expected validation error")
			}
			if client.calls != 0 {
				t.Errorf("daemon was called %d times", client.calls)
			}
		})
	}
}

func TestListNodePackagesRejectsMalformedDaemonData(t *testing.T) {
	validData := `{"name":"curl","version":"8.14.1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}`
	tests := map[string]string{
		"unexpected list kind": `{"kind":"OtherList","items":[]}`,
		"unexpected item kind": `{"kind":"PackageList","items":[{"kind":"OtherItem","meta":{"node":"node-a"},"data":` + validData + `}]}`,
		"unexpected node":      `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-b"},"data":` + validData + `}]}`,
		"empty name":           `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"","version":"1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}}]}`,
		"spaced version":       `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":" 1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}}]}`,
		"invalid timestamp":    `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":"1","arch":"amd64","type":"deb","installedat":"yesterday","sig":""}}]}`,
		"control signature":    `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":"1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":"bad\nsignature"}}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/package", query: url.Values{}, payload: payload}
			if _, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{Node: "node-a"}); err == nil {
				t.Fatal("expected malformed daemon response error")
			}
		})
	}
}

func TestListNodePackagesRejectsInconsistentLocalNodeData(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/package", query: url.Values{},
		payload: `{"kind":"PackageList","items":[
			{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":"1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}},
			{"kind":"PackageItem","meta":{"node":"node-b"},"data":{"name":"jq","version":"1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}}
		]}`,
	}
	if _, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{}); err == nil || !strings.Contains(err.Error(), "inconsistent node") {
		t.Fatalf("got inconsistent local node error %v", err)
	}
}

func TestListNodePackagesRejectsStaleCursor(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/node/name/_/system/package", query: url.Values{},
		payload: `{"kind":"PackageList","items":[{"kind":"PackageItem","meta":{"node":"node-a"},"data":{"name":"curl","version":"1","arch":"amd64","type":"deb","installedat":"2026-09-13T06:17:45Z","sig":""}}]}`,
	}
	if _, err := New(client).ListNodePackages(context.Background(), ListNodePackagesOptions{Cursor: "missing.0"}); err == nil || !strings.Contains(err.Error(), "no longer present") {
		t.Fatalf("got stale cursor error %v", err)
	}
}
