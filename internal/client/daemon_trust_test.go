package client

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

type trustClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *trustClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *trustClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// trustDaemon serves GET /api/auth/info, answering body, or failing with
// status when it is set.
type trustDaemon struct {
	calls  atomic.Int32
	status atomic.Int32
	body   atomic.Value
}

func newDaemonTrust(t *testing.T) (*DaemonTrust, *trustDaemon, *trustClock) {
	t.Helper()
	fake := &trustDaemon{}
	fake.body.Store(`{"methods":["basic","x509","openid"],"openid":{"client_id":"om3-a","issuer":"https://idp.example.test/application/o/cluster-a/"}}`)
	daemon := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.calls.Add(1)
		if r.URL.Path != "/api/auth/info" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request %s with authorization %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if status := fake.status.Load(); status != 0 {
			w.WriteHeader(int(status))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fake.body.Load().(string)))
	}))
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, map[string]any{"a": map[string]any{
		"name": "a", "cluster_id": "cluster-a", "endpoint": daemon.Server.URL, "request_timeout": "2s", "tls": map[string]string{"ca_file": daemon.CAFile},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	trust, err := NewDaemonTrust(catalog)
	if err != nil {
		t.Fatal(err)
	}
	clock := &trustClock{now: time.Now()}
	trust.now = clock.Now
	return trust, fake, clock
}

func TestDaemonTrustReadsTheProviderOncePerCluster(t *testing.T) {
	trust, fake, clock := newDaemonTrust(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			issuer, client, err := trust.OpenIDTrust(t.Context(), "cluster-a")
			if err != nil || issuer != "https://idp.example.test/application/o/cluster-a/" || client != "om3-a" {
				t.Errorf("issuer=%q client=%q err=%v", issuer, client, err)
			}
		})
	}
	wg.Wait()
	for range 5 {
		_, _, _ = trust.OpenIDTrust(t.Context(), "cluster-a")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("%d reads of the daemon for 15 tokens", fake.calls.Load())
	}
	if _, _, err := trust.OpenIDTrust(t.Context(), "unknown"); !errors.Is(err, auth.ErrNoOpenID) {
		t.Fatalf("unknown cluster: %v", err)
	}
	clock.Add(16 * time.Minute)
	if _, _, err := trust.OpenIDTrust(t.Context(), "cluster-a"); err != nil || fake.calls.Load() != 2 {
		t.Fatalf("not read again after its lifetime: %v %d", err, fake.calls.Load())
	}
}

func TestDaemonTrustKeepsTheLastAnswerAndSpacesRetries(t *testing.T) {
	trust, fake, clock := newDaemonTrust(t)
	fake.status.Store(http.StatusBadGateway)
	for range 3 {
		if _, _, err := trust.OpenIDTrust(t.Context(), "cluster-a"); !errors.Is(err, mcpauth.ErrVerificationUnavailable) {
			t.Fatalf("err = %v", err)
		}
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("an unreachable daemon was asked %d times", fake.calls.Load())
	}
	clock.Add(11 * time.Second)
	fake.status.Store(0)
	if _, _, err := trust.OpenIDTrust(t.Context(), "cluster-a"); err != nil || fake.calls.Load() != 2 {
		t.Fatalf("not retried: %v", err)
	}
	clock.Add(16 * time.Minute)
	fake.status.Store(http.StatusBadGateway)
	if issuer, _, err := trust.OpenIDTrust(t.Context(), "cluster-a"); err != nil || issuer == "" {
		t.Fatalf("last answer lost on a failed refresh: %v", err)
	}
}

func TestDaemonTrustOfADaemonWithoutOpenID(t *testing.T) {
	trust, fake, _ := newDaemonTrust(t)
	fake.body.Store(`{"methods":["basic","x509"]}`)
	if _, _, err := trust.OpenIDTrust(t.Context(), "cluster-a"); !errors.Is(err, auth.ErrNoOpenID) {
		t.Fatalf("err = %v", err)
	}
}
