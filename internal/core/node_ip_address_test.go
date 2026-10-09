package core

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// Shaped after the dev5 cache: unicast, link-local and multicast addresses.
const nodeIPAddressPayload = `{"kind":"IPAddressList","items":[
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"2001:db8::100","flagdeprecated":false,"intf":"br-vrack","mac":"be:37:6b:30:0f:4c","mask":"64","type":"ipv6"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"2001:db8::11","flagdeprecated":true,"intf":"br-vrack","mac":"be:37:6b:30:0f:4c","mask":"64","type":"ipv6"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"fe80::bc37:6bff:fe30:f4c","flagdeprecated":false,"intf":"br-vrack","mac":"be:37:6b:30:0f:4c","mask":"64","type":"ipv6"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"ff02::1","flagdeprecated":false,"intf":"br-vrack","mac":"be:37:6b:30:0f:4c","mask":"","type":"ipv6"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"10.46.0.11","flagdeprecated":false,"intf":"br-prd","mac":"86:dd:78:d5:cf:1a","mask":"24","type":"ipv4"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"224.0.0.1","flagdeprecated":false,"intf":"br-prd","mac":"86:dd:78:d5:cf:1a","mask":"","type":"ipv4"}},
	{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"127.0.0.1","flagdeprecated":false,"intf":"lo","mac":"00:00:00:00:00:00","mask":"8","type":"ipv4"}}
]}`

func nodeIPKeys(list NodeIPAddressList) string {
	keys := make([]string, 0, len(list.Addresses))
	for _, address := range list.Addresses {
		keys = append(keys, address.Interface+" "+address.Address)
	}
	return strings.Join(keys, ",")
}

func TestListNodeIPAddressesFiltersAndSorts(t *testing.T) {
	for _, tc := range []struct {
		options ListNodeIPAddressesOptions
		keys    string
	}{
		{ListNodeIPAddressesOptions{UnicastOnly: true}, "br-prd 10.46.0.11,br-vrack 2001:db8::11,br-vrack 2001:db8::100,lo 127.0.0.1"},
		{ListNodeIPAddressesOptions{}, "br-prd 10.46.0.11,br-prd 224.0.0.1,br-vrack 2001:db8::11,br-vrack 2001:db8::100,br-vrack fe80::bc37:6bff:fe30:f4c,br-vrack ff02::1,lo 127.0.0.1"},
		{ListNodeIPAddressesOptions{UnicastOnly: true, Family: "IPv4"}, "br-prd 10.46.0.11,lo 127.0.0.1"},
		{ListNodeIPAddressesOptions{UnicastOnly: true, Interface: "br-vrack"}, "br-vrack 2001:db8::11,br-vrack 2001:db8::100"},
	} {
		tc.options.Node = "node-a"
		client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/ipaddress", query: url.Values{}, payload: nodeIPAddressPayload}
		list, err := New(client).ListNodeIPAddresses(context.Background(), tc.options)
		if err != nil {
			t.Fatal(err)
		}
		if got := nodeIPKeys(list); got != tc.keys || list.ReportedTotal != 7 || list.Node != "node-a" || list.UnicastOnly != tc.options.UnicastOnly {
			t.Fatalf("options %+v gave %s (%+v)", tc.options, got, list)
		}
	}
}

func TestListNodeIPAddressesPreservesTheCachedFacts(t *testing.T) {
	client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/ipaddress", query: url.Values{}, payload: nodeIPAddressPayload}
	list, err := New(client).ListNodeIPAddresses(context.Background(), ListNodeIPAddressesOptions{Node: "node-a", Interface: "br-vrack", UnicastOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if list.Addresses[0] != (NodeIPAddress{Interface: "br-vrack", Address: "2001:db8::11", PrefixLen: "64", Family: "ipv6", MAC: "be:37:6b:30:0f:4c", Deprecated: true}) {
		t.Fatalf("cached facts not preserved: %+v", list.Addresses[0])
	}
}

func TestListNodeIPAddressesRejectsInvalidInputAndData(t *testing.T) {
	for _, options := range []ListNodeIPAddressesOptions{
		{}, {Node: "_"}, {Node: "node-a", Interface: "br*"}, {Node: "node-a", Interface: " lo"}, {Node: "node-a", Family: "ipx"},
	} {
		client := &recordingJSONGetter{t: t}
		if _, err := New(client).ListNodeIPAddresses(context.Background(), options); err == nil || client.calls != 0 {
			t.Fatalf("options %+v were accepted or reached the daemon", options)
		}
	}
	for _, payload := range []string{
		`{"kind":"PoolList","items":[]}`,
		`{"kind":"IPAddressList","items":[{"kind":"IPAddressItem","meta":{"node":"node-b"},"data":{"address":"10.0.0.1","intf":"lo","type":"ipv4"}}]}`,
		`{"kind":"IPAddressList","items":[{"kind":"Other","meta":{"node":"node-a"},"data":{"address":"10.0.0.1","intf":"lo","type":"ipv4"}}]}`,
		`{"kind":"IPAddressList","items":[{"kind":"IPAddressItem","meta":{"node":"node-a"},"data":{"address":"not-an-ip","intf":"lo","type":"ipv4"}}]}`,
	} {
		client := &recordingJSONGetter{t: t, path: "/api/node/name/node-a/system/ipaddress", query: url.Values{}, payload: payload}
		if _, err := New(client).ListNodeIPAddresses(context.Background(), ListNodeIPAddressesOptions{Node: "node-a"}); err == nil {
			t.Fatalf("payload %s was accepted", payload)
		}
	}
}
