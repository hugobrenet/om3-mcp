package main

import (
	"context"
	"encoding/json"
	"io"
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
	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

// Real MCP SDK requests and the identity bridge share the same checked route.
// Daemon doubles verify signatures/issuer/audience, independently of MCP TLS.
func TestOpenIDIdentityAndToolsUseExplicitCatalogueNode(t *testing.T) {
	key := testutil.NewJWTKey(t)
	const issuer = "https://idp.example.test/application/shared/"
	const subject = "opaque-shared-subject"
	var otherNodeCalls, daemonCalls atomic.Int32
	var mode atomic.Int32
	otherNode := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherNodeCalls.Add(1)
		w.WriteHeader(401)
	}))
	clusters := make(map[string]any)
	tokens := make(map[string]string)
	for _, id := range []string{"cluster-a", "cluster-b"} {
		audience := "client-" + id
		tokens[id] = testutil.OpenIDToken(t, key, issuer, audience, subject, nil)
		daemon := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			daemonCalls.Add(1)
			if r.Header.Get(auth.ClusterIDHeader) != "" || r.Header.Get(auth.NodeHeader) != "" {
				t.Error("routing header leaked to daemon")
			}
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			claims := jwt.MapClaims{}
			if _, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
				jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(issuer), jwt.WithAudience(audience), jwt.WithExpirationRequired()); err != nil {
				w.WriteHeader(401)
				_, _ = w.Write([]byte(raw)) // Hostile error payload must not escape.
				return
			}
			username, _ := claims["preferred_username"].(string)
			if username == "" {
				username, _ = claims["email"].(string)
			}
			if username == "" {
				username, _ = claims["sub"].(string)
			}
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/auth/whoami":
				strategy := "jwt-openid"
				switch mode.Load() {
				case 1:
					strategy = "jwt"
				case 2:
					strategy = "public"
				case 3:
					username = "someone-else"
				case 4:
					w.WriteHeader(503)
					return
				case 5:
					_, _ = w.Write([]byte("malformed"))
					return
				case 6:
					strategy = "basic"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"name": username, "auth": strategy, "raw_grant": "root", "ignored_secret": raw})
			case "/api/object/path":
				_ = json.NewEncoder(w).Encode([]string{id + "/svc/" + username})
			case "/api/object":
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"title":"Forbidden","detail":"namespace denied"}`))
			default:
				t.Errorf("unexpected daemon endpoint %s", r.URL.Path)
				w.WriteHeader(404)
			}
		}))
		// Trust both catalogue nodes so the test detects credential forwarding
		// to a non-selected node, rather than relying on a TLS failure there.
		bundle := append(append([]byte(nil), daemon.CAPEM...), otherNode.CAPEM...)
		if err := os.WriteFile(daemon.CAFile, bundle, 0600); err != nil {
			t.Fatal(err)
		}
		clusters[id] = map[string]any{"name": "Same display name", "expected_cluster_id": id,
			"nodes": map[string]string{"node-a": otherNode.Server.URL, "node-b": daemon.Server.URL},
			"tls":   map[string]string{"ca_file": daemon.CAFile}, "request_timeout": "2s"}
	}
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, clusters))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newLegacyMCPHandler(config.Config{Clusters: catalog})
	if err != nil {
		t.Fatal(err)
	}
	if daemonCalls.Load() != 0 || otherNodeCalls.Load() != 0 {
		t.Fatal("startup contacted a daemon")
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	requestIdentity := func(token, target string, want int) {
		t.Helper()
		r, _ := http.NewRequest("GET", server.URL+"/mcp/auth/whoami", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set(auth.NodeHeader, "node-b")
		if target != "" {
			r.Header.Set(auth.ClusterIDHeader, target)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("target=%s status=%d want=%d body=%s", target, response.StatusCode, want, body)
		}
		if response.Header.Get("Cache-Control") != "no-store" || strings.Contains(string(body), token) || strings.Contains(string(body), "root") {
			t.Fatal("identity bridge leaked credentials/grants or permitted caching")
		}
		if want == 200 {
			var result struct {
				ClusterID string    `json:"cluster_id"`
				Issuer    string    `json:"issuer"`
				Subject   string    `json:"subject"`
				ExpiresAt time.Time `json:"expires_at"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if result.ClusterID != target || result.Issuer != issuer || result.Subject != subject || !result.ExpiresAt.After(time.Now()) {
				t.Fatalf("OpenID identity lost opaque subject/provider: %+v", result)
			}
		}
	}
	for _, id := range []string{"cluster-a", "cluster-b"} {
		requestIdentity(tokens[id], id, 200)
	}
	// Username fallback is confirmed at the daemon; it never changes JWT sub.
	for _, change := range []func(jwt.MapClaims){
		func(c jwt.MapClaims) { delete(c, "preferred_username") },
		func(c jwt.MapClaims) { delete(c, "preferred_username"); delete(c, "email") },
	} {
		requestIdentity(testutil.OpenIDToken(t, key, issuer, "client-cluster-a", subject, change), "cluster-a", 200)
	}
	before := daemonCalls.Load()
	requestIdentity(tokens["cluster-a"], "", 401)
	requestIdentity(tokens["cluster-a"], "unknown", 401)
	for _, node := range []string{"", "unknown"} {
		r, _ := http.NewRequest("GET", server.URL+"/mcp/auth/whoami", nil)
		r.Header.Set("Authorization", "Bearer "+tokens["cluster-a"])
		r.Header.Set(auth.ClusterIDHeader, "cluster-a")
		if node != "" {
			r.Header.Set(auth.NodeHeader, node)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("missing/unknown node accepted: %d", response.StatusCode)
		}
	}
	if daemonCalls.Load() != before {
		t.Fatal("invalid target reached a daemon")
	}
	requestIdentity(tokens["cluster-a"], "cluster-b", 401) // Same issuer/sub, wrong audience.
	requestIdentity(testutil.OpenIDToken(t, testutil.NewJWTKey(t), issuer, "client-cluster-a", subject, nil), "cluster-a", 401)
	requestIdentity(testutil.OpenIDToken(t, key, "https://foreign-idp.example.test/", "client-cluster-a", subject, nil), "cluster-a", 401)
	for responseMode, want := range map[int32]int{1: 401, 2: 401, 3: 401, 4: 502, 5: 502, 6: 401} {
		mode.Store(responseMode)
		requestIdentity(tokens["cluster-a"], "cluster-a", want)
	}
	mode.Store(0)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, id := range []string{"cluster-a", "cluster-b"} {
		c := &http.Client{Transport: mcpBearerTransport{base: server.Client().Transport, token: tokens[id], clusterID: id, node: "node-b"}}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "OpenID agent", Version: "test"}, nil).Connect(ctx,
			&mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: c, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil || len(list.Tools) == 0 {
			t.Fatalf("list tools: %v", err)
		}
		wg.Go(func() {
			for range 3 {
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_cluster_objects", Arguments: map[string]any{}})
				if err != nil {
					t.Error(err)
					return
				}
				body, _ := json.Marshal(result.StructuredContent)
				if result.IsError || !strings.Contains(string(body), id+"/svc/alice") || strings.Contains(string(body), tokens[id]) {
					t.Error("tool request escaped its cluster/user binding")
				}
			}
			denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_object_status", Arguments: map[string]any{"path": "private/svc/example"}})
			if err != nil || !denied.IsError {
				t.Errorf("daemon grant refusal bypassed: %v", err)
			}
		})
	}
	wg.Wait()
	if otherNodeCalls.Load() != 0 {
		t.Fatal("OpenID credentials reached a non-selected node")
	}
	// An explicit, configured node is authoritative for routing. Its refusal
	// must not trigger fallback to the other node, which would accept this JWT.
	before = daemonCalls.Load()
	r, _ := http.NewRequest("GET", server.URL+"/mcp/auth/whoami", nil)
	r.Header.Set("Authorization", "Bearer "+tokens["cluster-a"])
	r.Header.Set(auth.ClusterIDHeader, "cluster-a")
	r.Header.Set(auth.NodeHeader, "node-a")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || otherNodeCalls.Load() != 1 || daemonCalls.Load() != before {
		t.Fatal("explicit node was ignored or refusal triggered fallback")
	}
}
