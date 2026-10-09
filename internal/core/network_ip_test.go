package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// A failover address reported by the instances of both nodes, an address
// shared by two objects, and two addresses sorting numerically.
const networkIPPayload = `{"kind":"NetworkIPList","items":[
	{"ip":"10.22.0.10","node":"node-a","path":"lab/svc/web","rid":"ip#0","network":{"name":"default","type":"bridge","network":"10.22.0.0/16"}},
	{"ip":"2001:db8::100","node":"node-b","path":"system/svc/vip","rid":"ip#0","network":{"name":"backend6","type":"lo","network":"2001:db8::/64"}},
	{"ip":"10.22.0.9","node":"node-a","path":"lab/svc/db","rid":"ip#1","network":{"name":"default","type":"bridge","network":"10.22.0.0/16"}},
	{"ip":"2001:db8::100","node":"node-a","path":"system/svc/vip","rid":"ip#0","network":{"name":"backend6","type":"lo","network":"2001:db8::/64"}},
	{"ip":"10.22.0.10","node":"node-b","path":"lab/svc/api","rid":"ip#0","network":{"name":"default","type":"bridge","network":"10.22.0.0/16"}}
]}`

func networkIPKeys(list NetworkIPList) string {
	keys := make([]string, 0, len(list.IPs))
	for _, ip := range list.IPs {
		keys = append(keys, ip.IP+"@"+ip.Node+":"+ip.Path)
	}
	return strings.Join(keys, ",")
}

func TestListNetworkIPsSortsCountsAndPaginates(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/network/ip", query: url.Values{}, payload: networkIPPayload}
	service := New(client)
	first, err := service.ListNetworkIPs(context.Background(), ListNetworkIPsOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if first.ReportedTotal != 5 || first.Total != 5 || first.Count != 3 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("unexpected first page %+v", first)
	}
	if got := networkIPKeys(first); got != "2001:db8::100@node-a:system/svc/vip,2001:db8::100@node-b:system/svc/vip,10.22.0.9@node-a:lab/svc/db" {
		t.Fatalf("unexpected order %s", got)
	}
	if first.IPs[0].OtherResources != 0 || first.IPs[0].Network != (NetworkReference{Name: "backend6", Type: "lo", Network: "2001:db8::/64"}) {
		t.Fatalf("a failover address is not shared: %+v", first.IPs[0])
	}
	second, err := service.ListNetworkIPs(context.Background(), ListNetworkIPsOptions{Limit: 3, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if got := networkIPKeys(second); got != "10.22.0.10@node-b:lab/svc/api,10.22.0.10@node-a:lab/svc/web" || second.Truncated {
		t.Fatalf("unexpected second page %s %+v", got, second)
	}
	if second.IPs[0].OtherResources != 1 || second.IPs[1].OtherResources != 1 {
		t.Fatalf("shared address not counted: %+v", second.IPs)
	}
}

func TestListNetworkIPsFilters(t *testing.T) {
	for _, tc := range []struct {
		options ListNetworkIPsOptions
		query   url.Values
		keys    string
	}{
		{ListNetworkIPsOptions{SharedOnly: true}, url.Values{}, "10.22.0.10@node-b:lab/svc/api,10.22.0.10@node-a:lab/svc/web"},
		{ListNetworkIPsOptions{Node: "node-b"}, url.Values{}, "2001:db8::100@node-b:system/svc/vip,10.22.0.10@node-b:lab/svc/api"},
		{ListNetworkIPsOptions{Path: "system/svc/vip"}, url.Values{}, "2001:db8::100@node-a:system/svc/vip,2001:db8::100@node-b:system/svc/vip"},
		// The daemon applies the network name; the recorded payload is not filtered.
		{ListNetworkIPsOptions{Network: "default", Node: "node-a"}, url.Values{"name": {"default"}}, "2001:db8::100@node-a:system/svc/vip,10.22.0.9@node-a:lab/svc/db,10.22.0.10@node-a:lab/svc/web"},
	} {
		client := &recordingJSONGetter{t: t, path: "/api/network/ip", query: tc.query, payload: networkIPPayload}
		list, err := New(client).ListNetworkIPs(context.Background(), tc.options)
		if err != nil {
			t.Fatal(err)
		}
		if got := networkIPKeys(list); got != tc.keys || list.ReportedTotal != 5 {
			t.Fatalf("options %+v gave %s", tc.options, got)
		}
	}
}

func TestListNetworkIPsMatchesTheRootNamespaceWhateverTheSpelling(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/network/ip", query: url.Values{},
		payload: `{"kind":"NetworkIPList","items":[{"ip":"10.22.0.5","node":"node-a","path":"web","rid":"ip#0","network":{"name":"default","type":"bridge","network":"10.22.0.0/16"}}]}`,
	}
	list, err := New(client).ListNetworkIPs(context.Background(), ListNetworkIPsOptions{Path: "root/svc/web"})
	if err != nil || list.Count != 1 {
		t.Fatalf("got %+v, %v", list, err)
	}
}

func TestListNetworkIPsAcceptsANullList(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/network/ip", query: url.Values{}, payload: `{"kind":"NetworkIPList","items":null}`}
	list, err := New(client).ListNetworkIPs(context.Background(), ListNetworkIPsOptions{})
	if err != nil || list.IPs == nil || list.Total != 0 || list.Truncated {
		t.Fatalf("got %+v, %v", list, err)
	}
}

func TestListNetworkIPsRejectsInvalidInputBeforeTheDaemonCall(t *testing.T) {
	for _, options := range []ListNetworkIPsOptions{
		{Network: "n*"}, {Node: "_"}, {Node: "node-*"}, {Path: "a/b/c/d"}, {Path: "test/svc/*"}, {Path: "a,b"}, {Limit: -1}, {Limit: 201}, {Cursor: "not base64!"},
	} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListNetworkIPs(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
}

func TestListNetworkIPsRejectsMalformedDaemonData(t *testing.T) {
	for _, payload := range []string{
		`{"kind":"NetworkList","items":[]}`,
		`{"kind":"NetworkIPList","items":[{"ip":"<nil>","node":"node-a","path":"web","rid":"ip#0"}]}`,
		`{"kind":"NetworkIPList","items":[{"ip":"10.0.0.1","node":"node/a","path":"web","rid":"ip#0"}]}`,
		`{"kind":"NetworkIPList","items":[{"ip":"10.0.0.1","node":"node-a","path":"","rid":"ip#0"}]}`,
	} {
		client := &recordingJSONGetter{t: t, path: "/api/network/ip", query: url.Values{}, payload: payload}
		if _, err := New(client).ListNetworkIPs(context.Background(), ListNetworkIPsOptions{}); err == nil {
			t.Fatalf("payload %s was accepted", payload)
		}
	}
}
