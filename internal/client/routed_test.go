package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func withCredential(t *testing.T, catalog *clusterconfig.Catalog, token string, run func(context.Context)) {
	t.Helper()
	v, err := auth.NewChecker(catalog)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { run(r.Context()); w.WriteHeader(204) })).ServeHTTP(response, request)
	if response.Code != 204 {
		t.Fatalf("authentication failed: %d", response.Code)
	}
}

func TestRoutedClientUsesIndependentTrustAndDelegatedCredential(t *testing.T) {
	key := testutil.NewJWTKey(t)
	token := testutil.AccessToken(t, key, "id", "node-a", "alice", nil)
	var foreignCalls atomic.Int32
	foreign := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls.Add(1); w.WriteHeader(200) }))
	var calls atomic.Int32
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("daemon did not receive the unchanged JWT")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, foreign.Server.URL+"/sink", 302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	load := func(ca string) *clusterconfig.Catalog {
		catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "id", ca, map[string]string{"node-a": d.Server.URL}))
		if err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	catalog := load(d.CAFile)
	t.Setenv("HTTPS_PROXY", foreign.Server.URL)
	api, err := NewRouted(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("startup contacted daemon")
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := api.GetJSON(context.Background(), "/read", nil, &result); err == nil {
		t.Fatal("unauthenticated request accepted")
	}
	withCredential(t, catalog, token, func(ctx context.Context) {
		if err := api.GetJSON(ctx, "/read", nil, &result); err != nil || !result.OK {
			t.Fatalf("verified daemon request: %v", err)
		}
		err := api.GetJSON(ctx, "/redirect", nil, &result)
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.StatusCode != 302 || foreignCalls.Load() != 0 {
			t.Fatal("redirect or proxy forwarded credentials")
		}
	})
	for _, ca := range []string{foreign.CAFile, ""} {
		untrusted := load(ca)
		api, err := NewRouted(untrusted)
		if err != nil {
			t.Fatal(err)
		}
		withCredential(t, catalog, token, func(ctx context.Context) {
			err := api.GetJSON(ctx, "/read", nil, &result)
			if err == nil || strings.Contains(err.Error(), token) {
				t.Fatal("untrusted TLS accepted or credentials exposed")
			}
		})
	}
	if foreignCalls.Load() != 0 || calls.Load() != 2 {
		t.Fatal("unexpected authenticated network calls")
	}
}

func TestDemoTLSBypassIsExplicitAndIsolatedPerCluster(t *testing.T) {
	key := testutil.NewJWTKey(t)
	token := testutil.AccessToken(t, key, "demo-id", "node-a", "alice", nil)
	var calls, foreignCalls atomic.Int32
	foreign := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("daemon did not receive the unchanged demo JWT")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, foreign.Server.URL+"/sink", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	// The generated certificate covers the loopback IP, but not localhost.
	endpoint, err := url.Parse(d.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Host = "localhost:" + endpoint.Port()
	definition := func(id string, tlsConfig map[string]any) map[string]any {
		return map[string]any{
			"name": id, "expected_cluster_id": id,
			"nodes": map[string]string{"node-a": endpoint.String()},
			"tls":   tlsConfig, "request_timeout": "2s",
		}
	}
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, map[string]any{
		"secure":     definition("secure-id", map[string]any{}),
		"trusted-ca": definition("trusted-ca-id", map[string]any{"ca_file": d.CAFile}),
		"demo":       definition("demo-id", map[string]any{"insecure": true}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTPS_PROXY", foreign.Server.URL)
	api, err := NewRouted(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	refuseSecure := func() {
		for _, id := range []string{"secure-id", "trusted-ca-id"} {
			secureToken := testutil.AccessToken(t, key, id, "node-a", "alice", nil)
			withCredential(t, catalog, secureToken, func(ctx context.Context) {
				if err := api.GetJSON(ctx, "/read", nil, &result); err == nil {
					t.Fatalf("TLS verification bypass leaked into %s", id)
				}
			})
		}
	}
	refuseSecure()
	withCredential(t, catalog, token, func(ctx context.Context) {
		if err := api.GetJSON(ctx, "/read", nil, &result); err != nil || !result.OK {
			t.Fatalf("explicit demo bypass failed: %v", err)
		}
		var apiError *APIError
		if err := api.GetJSON(ctx, "/redirect", nil, &result); !errors.As(err, &apiError) || apiError.StatusCode != http.StatusFound {
			t.Fatalf("demo redirect was not refused: %v", err)
		}
	})
	refuseSecure()
	if calls.Load() != 2 || foreignCalls.Load() != 0 {
		t.Fatalf("unexpected authenticated calls: daemon=%d foreign=%d", calls.Load(), foreignCalls.Load())
	}
}

func TestDelegatedTransportRefusesOriginIdentityChangesAndCancelledCredentials(t *testing.T) {
	key := testutil.NewJWTKey(t)
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "id", "", map[string]string{"node-a": "https://192.0.2.20:1215"}))
	if err != nil {
		t.Fatal(err)
	}
	token := testutil.AccessToken(t, key, "id", "node-a", "alice", nil)
	var calls int
	tr := &delegatedTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network call")
	}), binding: target{"id", "node-a"}, origin: "https://192.0.2.20:1215"}
	withCredential(t, catalog, token, func(ctx context.Context) {
		for _, endpoint := range []string{"http://192.0.2.20:1215/api/test", "https://192.0.2.21:1215/api/test", "https://user@192.0.2.20:1215/api/test"} {
			req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
			if _, err := tr.RoundTrip(req); err == nil {
				t.Fatal("foreign origin accepted")
			}
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", tr.origin+"/api/test", nil)
		req.Host = "attacker.example"
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("foreign Host accepted")
		}
		req.Host = ""
		tr.binding = target{"other-id", "node-a"}
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("foreign cluster identity accepted")
		}
		tr.binding = target{"id", "node-b"}
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("foreign node identity accepted")
		}
		tr.binding = target{"id", "node-a"}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		req = req.WithContext(cancelled)
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("cancelled token sent to daemon")
		}
	})
	if calls != 0 {
		t.Fatal("invalid request reached the network")
	}
}

func TestRoutedClientDelegatesEveryAPIResponseMode(t *testing.T) {
	key := testutil.NewJWTKey(t)
	token := testutil.AccessToken(t, key, "id", "node-a", "alice", nil)
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("filter") != "a b" {
			t.Error("credentials or query lost by routed client")
		}
		switch r.URL.Path {
		case "/json", "/post":
			if r.URL.Path == "/post" && r.Method != "POST" {
				t.Error("POST changed method")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("example"))
		case "/file":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("example"))
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("example"))
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: log\nid: 1\ndata: example\n\n"))
		case "/empty":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "id", d.CAFile, map[string]string{"node-a": d.Server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	api, err := NewRouted(catalog)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"filter": {"a b"}}
	var output struct {
		OK bool `json:"ok"`
	}
	consume := func(data []byte) error {
		if string(data) != "example" {
			return errors.New("unexpected stream data")
		}
		return nil
	}
	for name, call := range map[string]func(context.Context) error{
		"json": func(ctx context.Context) error { return api.GetJSON(ctx, "/json", query, &output) },
		"post": func(ctx context.Context) error {
			return api.PostJSON(ctx, "/post", query, map[string]string{"key": "value"}, &output)
		},
		"text": func(ctx context.Context) error {
			data, err := api.GetText(ctx, "/text", query)
			if err == nil {
				err = consume(data)
			}
			return err
		},
		"file": func(ctx context.Context) error {
			data, err := api.GetFile(ctx, "/file", query)
			if err == nil {
				err = consume(data)
			}
			return err
		},
		"stream": func(ctx context.Context) error { return api.GetStream(ctx, "/stream", query, consume) },
		"sse": func(ctx context.Context) error {
			return api.GetSSE(ctx, "/sse", query, func(event, id string, data []byte) error {
				if event != "log" || id != "1" {
					return errors.New("unexpected SSE metadata")
				}
				return consume(data)
			})
		},
		"empty": func(ctx context.Context) error { return api.GetNoContent(ctx, "/empty", query) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(context.Background()); err == nil {
				t.Fatal("unauthenticated operation allowed")
			}
			withCredential(t, catalog, token, func(ctx context.Context) {
				if err := call(ctx); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
