package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
)

func TestIdentityBridgeValidatesJWTAtDaemonOnly(t *testing.T) {
	key := testutil.NewJWTKey(t)
	other := testutil.NewJWTKey(t)
	valid := testutil.AccessToken(t, key, "cluster-a", "node-a", "alice", nil)
	forged := testutil.AccessToken(t, other, "cluster-a", "node-a", "alice", nil)
	var calls atomic.Int32
	var mode atomic.Int32
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/auth/whoami" || r.Method != "GET" {
			t.Error("identity bridge called an unexpected endpoint")
		}
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if raw != valid && raw != forged {
			t.Error("JWT was changed before reaching the daemon")
		}
		var claims jwt.RegisteredClaims
		if _, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired()); err != nil {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(raw)) // Deliberately hostile error body must not escape.
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode.Load() {
		case 1:
			_, _ = w.Write([]byte(`{"name":"bob","auth":"jwt"}`))
		case 2:
			_, _ = w.Write([]byte(`{"name":"alice","auth":"public"}`))
		case 3:
			w.WriteHeader(503)
		case 4:
			_, _ = w.Write([]byte("malformed identity"))
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "alice", "auth": "jwt", "raw_grant": "root", "ignored_secret": raw})
		}
	}))
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "cluster-a", d.CAFile, map[string]string{"node-a": d.Server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newMCPHandler(config.Config{Clusters: catalog})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	request := func(token, route string) (int, []byte) {
		r, _ := http.NewRequest("GET", server.URL+route, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Error("identity response is cacheable")
		}
		if strings.Contains(string(body), token) || strings.Contains(string(body), "root") {
			t.Error("identity bridge leaked credential or grants")
		}
		return response.StatusCode, body
	}
	status, body := request(valid, "/mcp/auth/whoami")
	if status != 200 || !strings.Contains(string(body), `"subject":"alice"`) || !strings.Contains(string(body), `"cluster_id":"cluster-a"`) || !strings.Contains(string(body), `"issuer":"node-a"`) {
		t.Fatalf("identity response: %d %s", status, body)
	}
	if status, _ := request(forged, "/mcp/auth/whoami"); status != 401 {
		t.Fatal("daemon refusal bypassed")
	}
	for responseMode, want := range map[int32]int{1: 401, 2: 401, 3: 502, 4: 502} {
		mode.Store(responseMode)
		if status, _ := request(valid, "/mcp/auth/whoami"); status != want {
			t.Fatalf("mode=%d status=%d", responseMode, status)
		}
	}
	before := calls.Load()
	if status, _ := request(valid, "/mcp/auth/whoami?endpoint=https://foreign.invalid"); status != 400 || calls.Load() != before {
		t.Fatal("caller supplied identity target accepted")
	}
	if status, _ := request("malformed", "/mcp/auth/whoami"); status != 401 || calls.Load() != before {
		t.Fatal("malformed JWT reached daemon")
	}
	// A forged but well-shaped token can initialize/list MCP tools. That is not
	// authentication; daemon signature verification protects actual API reads.
	if status, _ := request(forged, "/mcp"); status == 401 || calls.Load() != before {
		t.Fatal("MCP tried to authenticate the signature locally")
	}
}
