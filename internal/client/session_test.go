package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/daemonlogin"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
)

func TestSessionClientUsesPrivateTrustAndServerCredential(t *testing.T) {
	const token = "synthetic-daemon-token"
	var calls atomic.Int32
	foreign := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("daemon did not receive its server-held credential")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, foreign.Server.URL+"/sink", 302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	cluster := clusterconfig.Cluster{Ref: "cluster-a", ExpectedClusterID: "example-id", Endpoints: []string{d.Server.URL}, CAPEM: d.CAPEM, RequestTimeout: time.Second}
	session := daemonlogin.Session{ClusterRef: cluster.Ref, ClusterID: cluster.ExpectedClusterID, Endpoint: d.Server.URL, Username: "alice", AccessToken: token, ExpiresAt: time.Now().Add(time.Minute)}
	t.Setenv("HTTPS_PROXY", foreign.Server.URL)
	api, err := NewSession(cluster, session)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := api.GetJSON(context.Background(), "/read", nil, &result); err != nil || !result.OK {
		t.Fatalf("verified daemon request: %v", err)
	}
	err = api.GetJSON(context.Background(), "/redirect", nil, &result)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 302 || calls.Load() != 0 {
		t.Fatal("redirect or proxy forwarded daemon credentials")
	}
	cluster.CAPEM = foreign.CAPEM
	api, err = NewSession(cluster, session)
	if err != nil {
		t.Fatal(err)
	}
	err = api.GetJSON(context.Background(), "/read", nil, &result)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatal("untrusted TLS accepted or credentials exposed")
	}
}

func TestSessionTransportRefusesOriginChangesAndExpiredCredentials(t *testing.T) {
	var calls atomic.Int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected network call")
	})
	tr := &sessionTransport{base: base, origin: "https://192.0.2.20:1215", token: "synthetic-token", expiresAt: time.Now().Add(time.Minute)}
	for _, endpoint := range []string{"http://192.0.2.20:1215/api/test", "https://192.0.2.21:1215/api/test", "https://user@192.0.2.20:1215/api/test"} {
		req, _ := http.NewRequest("GET", endpoint, nil)
		if _, err := tr.RoundTrip(req); err == nil {
			t.Fatal("foreign origin accepted")
		}
	}
	req, _ := http.NewRequest("GET", tr.origin+"/api/test", nil)
	req.Host = "attacker.example"
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatal("foreign Host accepted")
	}
	req.Host = ""
	tr.expiresAt = time.Now().Add(-time.Second)
	if _, err := tr.RoundTrip(req); err == nil || calls.Load() != 0 {
		t.Fatal("expired token sent to daemon")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
