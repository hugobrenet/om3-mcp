package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

type fakeJSONGetter struct {
	t       *testing.T
	payload string
}

func (f *fakeJSONGetter) GetJSON(_ context.Context, path string, query url.Values, output any) error {
	f.t.Helper()
	if path != "/api/cluster/status" {
		f.t.Errorf("got path %q, want /api/cluster/status", path)
	}
	if len(query) != 0 {
		f.t.Errorf("got unexpected cluster status query %v, want no parameters", query)
	}
	return json.Unmarshal([]byte(f.payload), output)
}

func TestGetDaemonStatus(t *testing.T) {
	service := New(&fakeJSONGetter{t: t, payload: `{
		"cluster": {
			"config": {
				"id": "cluster-123", "name": "prod", "nodes": ["node-a", "node-b"],
				"quorum": true, "listener": {"addr": "::", "port": 1215}
			},
			"node": {
				"node-a": {
					"status": {
						"agent": "v3.0.0", "api": 1, "compat": 2,
						"is_leader": true, "is_overloaded": false,
						"booted_at": "2026-07-10T17:23:14+09:00"
					},
					"daemon": {
						"pid": 2610, "started_at": "2026-07-10T17:23:35+09:00",
						"daemondata": {"id":"daemondata","state":"future-state","configured_at":"2026-07-10T17:23:30Z","created_at":"2026-07-10T17:23:31Z","updated_at":"2026-07-10T17:23:32Z","queue_size":3},
						"listener": {"id":"listener","state":"running","configured_at":"c1","created_at":"c2","updated_at":"c3","addr":"::","port":"1215","rate_limiter":{"rate":20,"burst":100,"expires":60000000000}},
						"dns": {"id":"dns","state":"running","configured_at":"d1","created_at":"d2","updated_at":"d3","nameservers":["192.0.2.53","2001:db8::53"]},
						"collector": {"id":"collector","state":"disabled","configured_at":"o1","created_at":"o2","updated_at":"o3","url":"https://alice:secret@collector.example:8443/private/token?jwt=hidden#fragment"},
						"scheduler": {"id":"scheduler","state":"running","configured_at":"s1","created_at":"s2","updated_at":"s3","count":2,"max_running":10},
						"runner_imon": {"id":"runner_imon","state":"running","configured_at":"r1","created_at":"r2","updated_at":"r3","max_running":12}
					}
				}
			}
		},
		"daemon": {"nodename": "node-a", "routines": 121}
	}`})

	status, err := service.GetDaemonStatus(context.Background())
	if err != nil {
		t.Fatalf("get daemon status: %v", err)
	}
	if status.Daemon.NodeName != "node-a" {
		t.Errorf("got nodename %q, want node-a", status.Daemon.NodeName)
	}
	if status.Daemon.PID != 2610 {
		t.Errorf("got PID %d, want 2610", status.Daemon.PID)
	}
	if status.Cluster.ID != "cluster-123" {
		t.Errorf("got cluster ID %q, want cluster-123", status.Cluster.ID)
	}
	if !status.Cluster.QuorumEnabled {
		t.Error("expected cluster quorum feature to be enabled")
	}
	if status.Node.AgentVersion != "v3.0.0" {
		t.Errorf("got agent version %q, want v3.0.0", status.Node.AgentVersion)
	}
	if status.ListenerConfig.Port != 1215 {
		t.Errorf("got listener port %d, want 1215", status.ListenerConfig.Port)
	}
	if status.Subsystems.DaemonData == nil || status.Subsystems.DaemonData.State != "future-state" || status.Subsystems.DaemonData.QueueSize != 3 {
		t.Errorf("got daemondata %#v, want exact unknown state and queue size", status.Subsystems.DaemonData)
	}
	if status.Subsystems.Listener == nil || status.Subsystems.Listener.RateLimiter.ExpiresNS != 60000000000 {
		t.Errorf("got listener %#v, want exact runtime listener facts", status.Subsystems.Listener)
	}
	if status.Subsystems.DNS == nil || status.Subsystems.DNS.NameserversTotal != 2 || status.Subsystems.DNS.NameserversTruncated {
		t.Errorf("got DNS %#v, want two untruncated nameservers", status.Subsystems.DNS)
	}
	endpoint := status.Subsystems.Collector.Endpoint
	if !endpoint.Valid || endpoint.Scheme != "https" || endpoint.Host != "collector.example:8443" || !endpoint.UserInfoRedacted || !endpoint.PathRedacted || !endpoint.QueryRedacted || !endpoint.FragmentRedacted {
		t.Errorf("got collector endpoint %#v, want credential-safe projection", endpoint)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal daemon status: %v", err)
	}
	for _, secret := range []string{"alice", "secret", "private/token", "jwt=hidden", "#fragment"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("daemon status leaks collector URL component %q: %s", secret, encoded)
		}
	}
}

func TestDaemonSubsystemsPreserveMissingDataAndBoundNameservers(t *testing.T) {
	nameservers := make([]string, maxDaemonDNSNameservers+1)
	for i := range nameservers {
		nameservers[i] = "192.0.2.53"
	}
	result := daemonSubsystems(clusterNodeDaemon{DNS: &daemonDNSStatus{Nameservers: nameservers}})
	if result.DaemonData != nil || result.Listener != nil || result.Collector != nil || result.Scheduler != nil || result.RunnerIMon != nil {
		t.Errorf("absent subsystems must remain null: %#v", result)
	}
	if result.DNS == nil || result.DNS.NameserversTotal != maxDaemonDNSNameservers+1 || len(result.DNS.Nameservers) != maxDaemonDNSNameservers || !result.DNS.NameserversTruncated {
		t.Errorf("got unbounded DNS nameservers: %#v", result.DNS)
	}
}

func TestDaemonCollectorEndpointRejectsMalformedAndBoundsComponents(t *testing.T) {
	malformed := daemonCollectorEndpoint("://secret")
	if !malformed.Configured || malformed.Valid || malformed.Host != "" {
		t.Errorf("got malformed endpoint %#v", malformed)
	}
	bounded := daemonCollectorEndpoint("https://" + strings.Repeat("a", maxDaemonEndpointComponentRunes+1) + ".example")
	if !bounded.Valid || !bounded.ComponentsTruncated || len([]rune(bounded.Host)) != maxDaemonEndpointComponentRunes {
		t.Errorf("got unbounded endpoint %#v", bounded)
	}
}
