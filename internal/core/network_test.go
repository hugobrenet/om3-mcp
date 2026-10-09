package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Recorded from a two-node cluster, /api/network, plus an IPv6 range.
const networkPayload = `{"kind":"NetworkList","items":[
	{"free":65536,"name":"default","network":"10.22.0.0/16","size":65536,"type":"bridge","used":0},
	{"free":18446744073709551614,"name":"backend6","network":"2001:db8::/64","size":18446744073709551616,"type":"lo","used":2},
	{"free":1,"name":"lo","network":"127.0.0.1/32","size":1,"type":"lo","used":0,"errors":["overlaps default"]}
]}`

func TestListNetworksReportsDaemonUsage(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/network", query: url.Values{}, payload: networkPayload}
	networks, err := New(client).ListNetworks(context.Background(), ListNetworksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if networks.Total != 3 || networks.Count != 3 || networks.Truncated || networks.Provenance.Source == "" {
		t.Fatalf("unexpected list %+v", networks)
	}
	ipv6 := networks.Networks[0]
	if ipv6.Name != "backend6" || ipv6.Type != "lo" || ipv6.Network != "2001:db8::/64" || ipv6.Used != 2 ||
		ipv6.SizeAddresses != "18446744073709551616" || ipv6.FreeAddresses != "18446744073709551614" || ipv6.Errors == nil {
		t.Fatalf("IPv6 range not preserved or not sorted first: %+v", networks.Networks)
	}
	if lo := networks.Networks[2]; lo.Name != "lo" || len(lo.Errors) != 1 || lo.Errors[0] != "overlaps default" {
		t.Fatalf("errors not preserved: %+v", lo)
	}
}

func TestListNetworksForwardsTheNameAndAcceptsStringCounts(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/network", query: url.Values{"name": {"default"}},
		payload: `{"kind":"NetworkList","items":[{"free":"65534","name":"default","network":"10.22.0.0/16","size":"65536","type":"bridge","used":"2"}]}`,
	}
	networks, err := New(client).ListNetworks(context.Background(), ListNetworksOptions{Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if networks.NameFilter != "default" || networks.Networks[0].Used != 2 || networks.Networks[0].FreeAddresses != "65534" {
		t.Fatalf("unexpected list %+v", networks)
	}
}

func TestListNetworksKeepsAnUnknownSizeEmpty(t *testing.T) {
	client := &recordingJSONGetter{
		t: t, path: "/api/network", query: url.Values{},
		payload: `{"kind":"NetworkList","items":[{"name":"broken","network":"not-a-cidr","type":"bridge","used":0}]}`,
	}
	networks, err := New(client).ListNetworks(context.Background(), ListNetworksOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if network := networks.Networks[0]; network.SizeAddresses != "" || network.FreeAddresses != "" {
		t.Fatalf("unknown size was invented: %+v", network)
	}
}

func TestListNetworksRejectsInvalidInputAndData(t *testing.T) {
	for _, name := range []string{"n*", " default", "a,b", "a/b", strings.Repeat("n", 256)} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListNetworks(context.Background(), ListNetworksOptions{Name: name}); err == nil || client.calls != 0 {
			t.Fatalf("name %q was accepted or reached the daemon", name)
		}
	}
	for _, payload := range []string{
		`{"kind":"PoolList","items":[]}`,
		`{"kind":"NetworkList","items":[{"name":"n","used":-1}]}`,
		`{"kind":"NetworkList","items":[{"name":"n","used":1.5}]}`,
		`{"kind":"NetworkList","items":[{"name":"n","used":0,"size":"` + strings.Repeat("9", 65) + `"}]}`,
		`{"kind":"NetworkList","items":[{"name":"n","used":18446744073709551616}]}`,
	} {
		client := &recordingJSONGetter{t: t, path: "/api/network", query: url.Values{}, payload: payload}
		if _, err := New(client).ListNetworks(context.Background(), ListNetworksOptions{}); err == nil {
			t.Fatalf("payload %s was accepted", payload)
		}
	}
}
