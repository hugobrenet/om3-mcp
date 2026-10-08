package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func TestHealthWithoutAuthenticationOrDaemonContact(t *testing.T) {
	var calls, connections atomic.Int32
	daemon := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	daemon.Listener = &countingListener{Listener: daemon.Listener, connections: &connections}
	daemon.StartTLS()
	t.Cleanup(daemon.Close)
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, map[string]any{
		"cluster-a": map[string]any{
			"name": "Example cluster", "cluster_id": "cluster-a",
			"endpoint": daemon.URL, "request_timeout": "1s",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newMCPHandler(config.Config{Clusters: catalog, OAuth: testOAuthConfig()})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	for _, state := range []string{"unhealthy daemon", "stopped daemon"} {
		if state == "stopped daemon" {
			daemon.Close()
		}
		t.Run(state, func(t *testing.T) {
			for _, tc := range []struct {
				method string
				bearer string
			}{
				{http.MethodGet, ""},
				{http.MethodGet, "Bearer malformed"},
				{http.MethodHead, ""},
			} {
				request, err := http.NewRequestWithContext(t.Context(), tc.method, server.URL+"/health", nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.bearer != "" {
					request.Header.Set("Authorization", tc.bearer)
				}
				response, err := server.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusOK || response.Header.Get("WWW-Authenticate") != "" {
					t.Fatalf("health status=%d, headers=%v", response.StatusCode, response.Header)
				}
				if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("unexpected health headers: %v", response.Header)
				}
				wantBody := "{\"status\":\"ok\"}\n"
				if tc.method == http.MethodHead {
					wantBody = ""
				}
				if string(body) != wantBody {
					t.Fatalf("health body=%q, want=%q", body, wantBody)
				}
			}
		})
	}
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
		{http.MethodPut, "/health", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/health", http.StatusMethodNotAllowed},
		{http.MethodGet, "/health/unknown", http.StatusNotFound},
		{http.MethodPost, "/mcp", http.StatusUnauthorized},
		{http.MethodGet, "/mcp/auth/whoami", http.StatusNotFound},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s %s: status=%d, want=%d", tc.method, tc.path, response.Code, tc.status)
		}
	}
	if calls.Load() != 0 || connections.Load() != 0 {
		t.Fatal("startup or health check contacted the daemon")
	}
}
