package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

// Exercise failures through the real MCP HTTP transport. Only the SSO and
// daemons are simulated; no lab configuration or availability is changed.
func TestOAuthExchangeFailureIsolation(t *testing.T) {
	p := testutil.NewOAuthProvider(t)
	resource := "https://mcp.example.test/mcp"
	p.Metadata["token_endpoint"] = p.Server.URL + "/token"
	incoming := p.Token(t, resource, "alice", nil)
	outgoing := map[string]string{
		"daemon-a": p.Token(t, "daemon-a", "alice", nil),
		"daemon-b": p.Token(t, "daemon-b", "alice", nil),
	}
	var exchanges atomic.Int32
	var ssoUnavailable atomic.Bool
	p.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("subject_token") != incoming {
			t.Error("exchange received a different caller token")
		}
		if ssoUnavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "private-SSO-failure-details")
			return
		}
		raw, ok := outgoing[r.Form.Get("audience")]
		if !ok {
			t.Error("exchange requested an unconfigured audience")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": raw, "token_type": "Bearer",
			"issued_token_type": "urn:ietf:params:oauth:token-type:access_token", "expires_in": 120,
		})
	}
	var calls [2]atomic.Int32
	var fault atomic.Int32 // affects only cluster b: 1 = 503, 2 = timeout, 3 = redirect
	release := make(chan struct{})
	var redirectURL string
	daemons := make([]testutil.Daemon, 0, 2)
	clusters := make(map[string]any)
	for i, id := range []string{"a", "b"} {
		d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls[i].Add(1)
			if r.URL.Path != "/api/cluster/status" || r.Header.Get("Authorization") != "Bearer "+outgoing["daemon-"+id] {
				t.Error("request reached an unexpected endpoint or used another cluster's token")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if id == "b" {
				switch fault.Load() {
				case 1:
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case 2:
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				case 3:
					http.Redirect(w, r, redirectURL+"/api/cluster/status", http.StatusTemporaryRedirect)
					return
				}
			}
			fmt.Fprintf(w, `{"cluster":{"config":{"id":%q,"name":%q,"nodes":["shared-node"]},"node":{"shared-node":{"status":{"agent":%q}}}}}`, "cluster-"+id, id, "healthy-"+id)
		}))
		daemons = append(daemons, d)
		// Equal display names and equal node names must not collapse distinct IDs.
		clusters[id] = map[string]any{"name": "same-name", "cluster_id": "cluster-" + id, "endpoint": d.Server.URL,
			"auth":            map[string]string{"profile": "test-sso", "audience": "daemon-" + id},
			"request_timeout": "1s", "tls": map[string]string{"ca_file": d.CAFile}}
	}
	redirectURL = daemons[0].Server.URL
	t.Cleanup(func() { close(release) })
	catalog, err := clusterconfig.Load(testutil.WriteYAML(t, map[string]any{"clusters": clusters}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := auth.LoadExchangeProfiles(testutil.WriteExchangeProfiles(t, p, "client_secret_basic"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := newMCPHandler(config.Config{Clusters: catalog, Exchange: profiles,
		OAuth: auth.OAuthConfig{ResourceURL: resource, Issuer: p.Issuer, CAFile: p.CAFile}})
	if err != nil {
		t.Fatal(err)
	}
	server := testutil.NewDaemon(t, h)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "failure-test", Version: "1"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{Endpoint: server.Server.URL + "/mcp", DisableStandaloneSSE: true,
			HTTPClient: &http.Client{Transport: mcpBearerTransport{base: server.Server.Client().Transport, token: incoming}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	type outcome struct {
		result *mcp.CallToolResult
		err    error
	}
	call := func(name string, args map[string]any) outcome {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		return outcome{result, err}
	}
	check := func(t *testing.T, got outcome, wantError bool, text string) {
		t.Helper()
		if got.err != nil || got.result == nil {
			t.Fatalf("MCP transport failed instead of returning a tool result: %v", got.err)
		}
		data, err := json.Marshal(got.result)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{incoming, outgoing["daemon-a"], outgoing["daemon-b"], "test-secret&+", "private-SSO-failure-details"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("credential or SSO failure details appeared in tool output")
			}
		}
		if got.result.IsError != wantError || !strings.Contains(string(data), text) {
			t.Fatalf("unexpected tool result: %s", data)
		}
	}
	args := func(id, node string) map[string]any { return map[string]any{"cluster_id": id, "node": node} }
	t.Run("ambiguous names retain both IDs without upstream calls", func(t *testing.T) {
		got := call("list_clusters", map[string]any{"query": "same-name"})
		check(t, got, false, "cluster-a")
		check(t, got, false, "cluster-b")
		check(t, call("get_node_status", args("same-name", "shared-node")), true, "Unknown cluster_id")
		if exchanges.Load() != 0 || calls[0].Load() != 0 || calls[1].Load() != 0 {
			t.Fatal("discovery or unknown ID contacted an upstream service")
		}
	})
	t.Run("missing node does not change cluster", func(t *testing.T) {
		check(t, call("get_node_status", args("cluster-b", "absent-node")), true, "is not present in the cluster status")
		if exchanges.Load() != 1 || calls[0].Load() != 0 || calls[1].Load() != 1 {
			t.Fatal("missing node caused fallback or extra requests")
		}
	})
	// The client timeout reads "Client.Timeout exceeded" or "context deadline
	// exceeded", depending on where the request stood when it fired.
	for _, tc := range []struct {
		name string
		mode int32
		want []string
	}{{"daemon unavailable", 1, []string{"503"}}, {"daemon timeout", 2, []string{"Client.Timeout", "deadline exceeded"}}, {"daemon redirect", 3, []string{"307"}}} {
		t.Run(tc.name+" leaves other cluster usable", func(t *testing.T) {
			fault.Store(tc.mode)
			defer fault.Store(0)
			beforeA, beforeB, beforeExchange := calls[0].Load(), calls[1].Load(), exchanges.Load()
			failed := make(chan outcome, 1)
			go func() { failed <- call("get_node_status", args("cluster-b", "shared-node")) }()
			check(t, call("get_node_status", args("cluster-a", "shared-node")), false, "healthy-a")
			got := <-failed
			data, _ := json.Marshal(got.result)
			want := tc.want[0]
			for _, text := range tc.want {
				if strings.Contains(string(data), text) {
					want = text
				}
			}
			check(t, got, true, want)
			if calls[0].Load() != beforeA+1 || calls[1].Load() != beforeB+1 || exchanges.Load() != beforeExchange+2 {
				t.Fatal("failure triggered fallback, a followed redirect, or unexpected retries")
			}
		})
	}
	t.Run("SSO token endpoint outage and recovery", func(t *testing.T) {
		beforeA, beforeB, beforeExchange := calls[0].Load(), calls[1].Load(), exchanges.Load()
		ssoUnavailable.Store(true)
		check(t, call("list_clusters", nil), false, "cluster-a")
		if exchanges.Load() != beforeExchange {
			t.Fatal("catalogue contacted unavailable token endpoint")
		}
		for _, id := range []string{"cluster-a", "cluster-b"} {
			check(t, call("get_node_status", args(id, "shared-node")), true, "Token exchange failed at the SSO")
		}
		if calls[0].Load() != beforeA || calls[1].Load() != beforeB {
			t.Fatal("daemon contacted despite failed exchange")
		}
		ssoUnavailable.Store(false)
		check(t, call("get_node_status", args("cluster-b", "shared-node")), false, "healthy-b")
	})
	t.Run("closed daemon connection leaves other cluster usable", func(t *testing.T) {
		daemons[1].Server.Close()
		beforeA, beforeB := calls[0].Load(), calls[1].Load()
		check(t, call("get_node_status", args("cluster-b", "shared-node")), true, "request OpenSVC daemon")
		check(t, call("get_node_status", args("cluster-a", "shared-node")), false, "healthy-a")
		if calls[0].Load() != beforeA+1 || calls[1].Load() != beforeB {
			t.Fatal("unreachable target caused fallback")
		}
	})
}
