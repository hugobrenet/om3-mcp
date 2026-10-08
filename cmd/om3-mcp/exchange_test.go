package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func TestOAuthExchangeMultiClusterTools(t *testing.T) {
	p := testutil.NewOAuthProvider(t)
	resource := "https://mcp.example.test/mcp"
	p.Metadata["token_endpoint"] = p.Server.URL + "/token"
	var exchanges, daemonCalls atomic.Int32
	var fail atomic.Bool
	p.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		id, _, ok := r.BasicAuth()
		if !ok || id != "test-exchange" {
			t.Error("missing confidential identity")
		}
		r.ParseForm()
		var claims jwt.RegisteredClaims
		if _, err := jwt.ParseWithClaims(r.Form.Get("subject_token"), &claims, func(*jwt.Token) (any, error) { return &p.Key.PublicKey, nil }, jwt.WithAudience(resource), jwt.WithIssuer(p.Issuer)); err != nil {
			t.Error("exchange lost subject identity")
			w.WriteHeader(400)
			return
		}
		if fail.Load() {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_target","error_description":"secret-must-not-leak"}`)
			return
		}
		aud := r.Form.Get("audience")
		if aud != "daemon-a" && aud != "daemon-b" {
			t.Error("unconfigured audience requested")
		}
		raw := p.Token(t, aud, claims.Subject, func(c jwt.MapClaims) { c["entitlements"] = []string{"root"} })
		json.NewEncoder(w).Encode(map[string]any{"access_token": raw, "token_type": "Bearer", "issued_token_type": "urn:ietf:params:oauth:token-type:access_token", "expires_in": 120})
	}
	clusters := make(map[string]any)
	for _, id := range []string{"a", "b"} {
		d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			daemonCalls.Add(1)
			var claims jwt.RegisteredClaims
			if _, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &claims, func(*jwt.Token) (any, error) { return &p.Key.PublicKey, nil }, jwt.WithAudience("daemon-"+id), jwt.WithIssuer(p.Issuer)); err != nil {
				t.Error("wrong token reached daemon")
				w.WriteHeader(401)
				return
			}
			if r.Header.Get(auth.ClusterIDHeader) != "" || r.Header.Get("X-OpenSVC-Node") != "" {
				t.Error("legacy target headers leaked")
			}
			if claims.Subject == "denied" {
				w.WriteHeader(403)
				return
			}
			fmt.Fprintf(w, `{"cluster":{"config":{"id":%q,"name":%q,"nodes":["shared-node"]},"node":{"shared-node":{"status":{"agent":%q}}}}}`, "cluster-"+id, id, claims.Subject+"-"+id)
		}))
		clusters[id] = map[string]any{"name": "dev" + id, "cluster_id": "cluster-" + id, "endpoint": d.Server.URL, "auth": map[string]string{"profile": "test-sso", "audience": "daemon-" + id}, "request_timeout": "2s", "tls": map[string]string{"ca_file": d.CAFile}}
	}
	catalog, err := clusterconfig.Load(testutil.WriteYAML(t, map[string]any{"clusters": clusters}))
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := auth.LoadExchangeProfiles(testutil.WriteExchangeProfiles(t, p, "client_secret_basic"))
	if err != nil {
		t.Fatal(err)
	}
	var audit syncBuffer
	defer func(logger *slog.Logger) { auditLogger = logger }(auditLogger)
	auditLogger = slog.New(slog.NewTextHandler(&audit, nil))
	h, err := newMCPHandler(config.Config{Clusters: catalog, Exchange: profiles, OAuth: auth.OAuthConfig{ResourceURL: resource, Issuer: p.Issuer, CAFile: p.CAFile}})
	if err != nil {
		t.Fatal(err)
	}
	mcpServer := testutil.NewDaemon(t, h)
	connect := func(user string) *mcp.ClientSession {
		t.Helper()
		token := p.Token(t, resource, user, func(c jwt.MapClaims) { c["azp"] = "codex-test" })
		client := &http.Client{Transport: mcpBearerTransport{base: mcpServer.Server.Client().Transport, token: token, clusterID: "ignored-header", node: "ignored-node"}}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: mcpServer.Server.URL + "/mcp", HTTPClient: client, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	a := connect("alice")
	b := connect("bob")
	list, err := a.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		data, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Required []string `json:"required"`
		}
		json.Unmarshal(data, &schema)
		found := false
		for _, name := range schema.Required {
			if name == "cluster_id" {
				found = true
			}
		}
		if (tool.Name != "list_clusters") != found {
			t.Fatalf("incorrect target schema for %s", tool.Name)
		}
	}
	discovery, err := a.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_clusters", Arguments: map[string]any{"limit": 1}})
	if err != nil || discovery.IsError {
		t.Fatalf("catalogue failed: %v %#v", err, discovery)
	}
	data, _ := json.Marshal(discovery)
	if strings.Contains(string(data), "https://") || strings.Contains(string(data), "test-sso") {
		t.Fatal("catalogue exposed private routing data")
	}
	if exchanges.Load() != 0 || daemonCalls.Load() != 0 {
		t.Fatal("discovery contacted upstream services")
	}
	for _, args := range []map[string]any{{"node": "shared-node"}, {"cluster_id": "unknown", "node": "shared-node"}, {"cluster_id": "cluster-a", "node": "shared-node", "endpoint": "https://attacker.test"}} {
		res, err := a.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_node_status", Arguments: args})
		if err != nil || !res.IsError {
			t.Fatalf("invalid target accepted: %v", err)
		}
	}
	if exchanges.Load() != 0 || daemonCalls.Load() != 0 {
		t.Fatal("invalid target caused exchange")
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			session, user := a, "alice"
			if i%2 == 1 {
				session, user = b, "bob"
			}
			id := "a"
			if i%4 >= 2 {
				id = "b"
			}
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_node_status", Arguments: map[string]any{"cluster_id": "cluster-" + id, "node": "shared-node"}})
			if err != nil {
				t.Error(err)
				return
			}
			data, _ := json.Marshal(res)
			if res.IsError || !strings.Contains(string(data), user+"-"+id) {
				t.Errorf("wrong user/cluster result: %s", data)
			}
		})
	}
	wg.Wait()
	if exchanges.Load() != 8 || daemonCalls.Load() != 8 {
		t.Fatalf("unexpected exchange/request counts: %d/%d", exchanges.Load(), daemonCalls.Load())
	}
	fail.Store(true)
	res, err := a.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_node_status", Arguments: map[string]any{"cluster_id": "cluster-a", "node": "shared-node"}})
	if err != nil || !res.IsError || daemonCalls.Load() != 8 {
		t.Fatal("SSO refusal did not stop daemon call")
	}
	data, _ = json.Marshal(res)
	if !strings.Contains(string(data), "invalid_target") || strings.Contains(string(data), "secret-must-not-leak") {
		t.Fatal("SSO refusal not safely reported")
	}
	fail.Store(false)
	denied := connect("denied")
	res, err = denied.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_node_status", Arguments: map[string]any{"cluster_id": "cluster-a", "node": "shared-node"}})
	if err != nil || !res.IsError {
		t.Fatal("daemon grant refusal lost")
	}
	data, _ = json.Marshal(res)
	if !strings.Contains(string(data), "403") {
		t.Fatal("daemon refusal not reported")
	}

	// One credential-free audit line per tool call.
	logs := audit.String()
	for _, secret := range []string{"eyJ", "secret-must-not-leak", "Bearer", "test-secret"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("audit log contains credential material %q:\n%s", secret, logs)
		}
	}
	for _, want := range [][]string{
		{"tool=list_clusters", "cluster_id=\"\"", "subject=alice", "client_id=codex-test", "exchange=none", "outcome=ok"},
		{"tool=get_node_status", "cluster_id=unknown", "exchange=none", "outcome=tool_error"},
		{"tool=get_node_status", "cluster_id=cluster-b", "subject=bob", "exchange=ok", "daemon_subject=bob", "outcome=ok"},
		{"cluster_id=cluster-a", "subject=alice", `exchange="Token exchange refused by SSO (invalid_target)."`, "daemon_subject=\"\"", "outcome=tool_error"},
		{"cluster_id=cluster-a", "subject=denied", "exchange=ok", "daemon_subject=denied", "outcome=tool_error", "403"},
	} {
		found := false
		for _, line := range strings.Split(logs, "\n") {
			matched := strings.Contains(line, "msg=\"mcp tool call\"")
			for _, field := range want {
				matched = matched && strings.Contains(line, " "+field)
			}
			found = found || matched
		}
		if !found {
			t.Errorf("missing audit line with %v in:\n%s", want, logs)
		}
	}
	if n := strings.Count(logs, "msg=\"mcp tool call\""); n != 14 {
		t.Errorf("expected 14 audited tool calls, got %d", n)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
