package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	// daemonTrustLifetime is how long the OpenID provider a daemon publishes
	// is reused: a change of its configuration applies within that time.
	daemonTrustLifetime = 15 * time.Minute
	// daemonTrustRetry spaces the attempts after a failure, so that an
	// unreachable daemon is not asked again for every token.
	daemonTrustRetry = 10 * time.Second
)

// DaemonTrust gives the OpenID provider the daemon of each cluster accepts,
// read from its public GET /api/auth/info: the issuer and the client ID its
// tokens are intended for. It is read for a cluster when a token first needs
// it, never for every token, and without any token: the request carries no
// credential. The last answer is kept when a refresh fails.
type DaemonTrust struct {
	clients map[string]*Client
	now     func() time.Time
	mu      sync.Mutex
	entries map[string]*daemonTrust
}

type daemonTrust struct {
	known            bool
	issuer, clientID string // empty when the daemon accepts no OpenID token
	expires          time.Time
	retry            time.Time
	err              error
	pending          chan struct{} // closed when the read in progress ends
}

func NewDaemonTrust(catalog *clusterconfig.Catalog) (*DaemonTrust, error) {
	d := &DaemonTrust{clients: make(map[string]*Client), now: time.Now, entries: make(map[string]*daemonTrust)}
	for _, cluster := range catalog.List() {
		base, err := clusterTransport(cluster)
		if err != nil {
			return nil, err
		}
		httpClient := &http.Client{
			Transport:     base,
			Timeout:       cluster.RequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		api, err := New(cluster.Endpoint, httpClient)
		if err != nil {
			return nil, err
		}
		d.clients[cluster.ID] = api
		d.entries[cluster.ID] = &daemonTrust{}
	}
	return d, nil
}

// OpenIDTrust returns the issuer and the client ID the daemon of clusterID
// accepts. It returns auth.ErrNoOpenID when the daemon accepts no OpenID
// token, and an error wrapping mcpauth.ErrVerificationUnavailable when its
// answer is unknown.
func (d *DaemonTrust) OpenIDTrust(ctx context.Context, clusterID string) (string, string, error) {
	for {
		d.mu.Lock()
		e, ok := d.entries[clusterID]
		if !ok {
			d.mu.Unlock()
			return "", "", auth.ErrNoOpenID
		}
		now := d.now()
		fresh := e.known && now.Before(e.expires)
		if e.pending == nil && (fresh || now.Before(e.retry)) {
			defer d.mu.Unlock()
			return e.answer()
		}
		if e.pending != nil {
			pending := e.pending
			stale := e.known
			d.mu.Unlock()
			if stale {
				// A refresh is in progress: the last answer serves meanwhile.
				d.mu.Lock()
				defer d.mu.Unlock()
				return e.answer()
			}
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return "", "", fmt.Errorf("%w: daemon OpenID configuration", mcpauth.ErrVerificationUnavailable)
			}
		}
		e.pending = make(chan struct{})
		d.mu.Unlock()

		issuer, clientID, err := d.read(ctx, clusterID)

		d.mu.Lock()
		if err == nil {
			e.known, e.issuer, e.clientID, e.err = true, issuer, clientID, nil
			e.expires = d.now().Add(daemonTrustLifetime)
		} else if ctx.Err() == nil {
			// A caller's own cancellation says nothing about the daemon.
			e.err, e.retry = err, d.now().Add(daemonTrustRetry)
		}
		close(e.pending)
		e.pending = nil
		d.mu.Unlock()
		if err != nil && ctx.Err() != nil {
			return "", "", fmt.Errorf("%w: daemon OpenID configuration", mcpauth.ErrVerificationUnavailable)
		}
	}
}

// answer is the last answer, else the last error. Called with d.mu held.
func (e *daemonTrust) answer() (string, string, error) {
	switch {
	case e.known && e.issuer == "":
		return "", "", auth.ErrNoOpenID
	case e.known:
		return e.issuer, e.clientID, nil
	default:
		return "", "", fmt.Errorf("%w: daemon OpenID configuration: %w", mcpauth.ErrVerificationUnavailable, e.err)
	}
}

// read asks the daemon which OpenID provider it accepts. An answer without
// one, or with an invalid one, means the daemon accepts no OpenID token.
func (d *DaemonTrust) read(ctx context.Context, clusterID string) (string, string, error) {
	var info struct {
		OpenID *struct {
			Issuer   string `json:"issuer"`
			ClientID string `json:"client_id"`
		} `json:"openid"`
	}
	if err := d.clients[clusterID].GetJSON(ctx, "/api/auth/info", nil, &info); err != nil {
		return "", "", errors.New("daemon auth info unavailable")
	}
	if info.OpenID == nil || !mcpauth.ValidClaimText(info.OpenID.Issuer) || !mcpauth.ValidClaimText(info.OpenID.ClientID) {
		return "", "", nil
	}
	return info.OpenID.Issuer, info.OpenID.ClientID, nil
}
