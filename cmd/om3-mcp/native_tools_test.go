package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

// Real SDK calls through the compiled HTTPS entrypoint with independent TLS
// and JWT authorities. Same subjects and issuers in different clusters are safe.
func TestHTTPSNativeToolsIsolateUsersClustersAndIssuerNodes(t *testing.T) {
	for _, binary := range []bool{false, true} {
		name := "in_process"
		if binary {
			name = "compiled_entrypoint"
		}
		t.Run(name, func(t *testing.T) { testNativeTools(t, binary) })
	}
}

func testNativeTools(t *testing.T, binary bool) {
	clusters := make(map[string]any)
	type caller struct{ token, want string }
	var callers []caller
	var daemonCalls atomic.Int32
	var invalidTokens []string
	for _, id := range []string{"cluster-a", "cluster-b"} {
		signingKey := testutil.NewJWTKey(t)
		nodes := make(map[string]string)
		var tlsCAFile string
		// Different HTTPS authorities need separate cluster CA bundles below.
		var caBundle []byte
		for _, issuer := range []string{"node-a", "node-b"} {
			users := make(map[string]string)
			for _, user := range []string{"alice", "bob"} {
				token := testutil.AccessToken(t, signingKey, id, issuer, user, nil)
				users[token] = user
				callers = append(callers, caller{token, user + "/svc/" + id + "-" + issuer})
			}
			d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				daemonCalls.Add(1)
				user := users[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
				if user == "" {
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/object/path":
					_ = json.NewEncoder(w).Encode([]string{user + "/svc/" + id + "-" + issuer})
				case "/api/object":
					w.WriteHeader(403)
					_, _ = fmt.Fprint(w, `{"title":"Forbidden","detail":"namespace access denied"}`)
				default:
					t.Errorf("unexpected daemon path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			nodes[issuer] = d.Server.URL
			caBundle = append(caBundle, d.CAPEM...)
			tlsCAFile = d.CAFile
		}
		// Both node TLS CAs are trusted, neither is the JWT signing authority.
		if err := os.WriteFile(tlsCAFile, caBundle, 0600); err != nil {
			t.Fatal(err)
		}
		clusters[id] = map[string]any{"name": "Example " + id, "expected_cluster_id": id, "nodes": nodes, "tls": map[string]string{"ca_file": tlsCAFile}, "request_timeout": "2s"}
		for _, change := range []func(jwt.MapClaims){
			func(c jwt.MapClaims) { delete(c, "cluster_id") },
			func(c jwt.MapClaims) { c["iss"] = "unknown-node" },
			func(c jwt.MapClaims) { c["cluster_id"] = "unknown-cluster" },
		} {
			invalidTokens = append(invalidTokens, testutil.AccessToken(t, signingKey, id, "node-a", "alice", change))
		}
	}
	invalidTokens = append(invalidTokens, "malformed-token")
	cert, key, roots := writeListenerCertificate(t)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	c := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	clusterFile := testutil.WriteCatalog(t, clusters)
	var origin string
	if binary {
		origin = startHTTPSBinary(t, c, cert, key, clusterFile)
	} else {
		catalog, err := clusterconfig.Load(clusterFile)
		if err != nil {
			t.Fatal(err)
		}
		handler, err := newMCPHandler(config.Config{Clusters: catalog})
		if err != nil {
			t.Fatal(err)
		}
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewUnstartedServer(handler)
		server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
		server.StartTLS()
		t.Cleanup(server.Close)
		origin = server.URL
	}
	for _, token := range invalidTokens {
		request, _ := http.NewRequest("POST", origin+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := c.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("invalid JWT returned %d", response.StatusCode)
		}
	}
	if daemonCalls.Load() != 0 {
		t.Fatal("startup or rejected JWT contacted a daemon")
	}
	for _, tc := range []struct{ body, origin string }{
		{strings.Repeat("x", (1<<20)+1), ""},
		{`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "https://foreign.example"},
	} {
		request, _ := http.NewRequest("POST", origin+"/mcp", strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+callers[0].token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		response, err := c.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode < 400 || response.StatusCode >= 500 {
			t.Fatalf("unsafe request returned %d", response.StatusCode)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var sessions []*mcp.ClientSession
	for _, caller := range callers {
		httpClient := &http.Client{Transport: mcpBearerTransport{base: transport, token: caller.token}, Timeout: 3 * time.Second}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "Example native agent", Version: "test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: origin + "/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		info := session.InitializeResult().ServerInfo
		if info == nil || info.Name != "om3-mcp" {
			t.Fatalf("unexpected MCP server identity: %#v", info)
		}
		sessions = append(sessions, session)
		list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil || len(list.Tools) == 0 {
			t.Fatalf("list native tools: %v", err)
		}
	}
	if daemonCalls.Load() != 0 {
		t.Fatal("MCP initialization or tool discovery contacted a daemon")
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(sessions))
	for i, session := range sessions {
		wg.Go(func() {
			for range 3 {
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_cluster_objects", Arguments: map[string]any{}})
				if err != nil {
					errors <- err
					return
				}
				encoded, _ := json.Marshal(result.StructuredContent)
				if result.IsError || !strings.Contains(string(encoded), callers[i].want) || !strings.Contains(string(encoded), `"source":"opensvc_daemon"`) || strings.Contains(string(encoded), callers[i].token) {
					errors <- fmt.Errorf("tool result escaped its signed cluster/node/user binding")
					return
				}
			}
			denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_object_status", Arguments: map[string]any{"path": "private/svc/example"}})
			if err != nil || !denied.IsError {
				errors <- fmt.Errorf("daemon permission failure was bypassed: %v", err)
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

type mcpBearerTransport struct {
	base      http.RoundTripper
	token     string
	clusterID string
	node      string
}

func (t mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	clone.Header.Del("X-OpenSVC-Cluster-ID")
	if t.clusterID != "" {
		clone.Header.Set("X-OpenSVC-Cluster-ID", t.clusterID)
	}
	clone.Header.Del("X-OpenSVC-Node")
	if t.node != "" {
		clone.Header.Set("X-OpenSVC-Node", t.node)
	}
	return t.base.RoundTrip(clone)
}
