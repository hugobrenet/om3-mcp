// Package auth authenticates MCP callers. On the HTTPS listener, OAuth access
// JWTs intended for the MCP resource are verified locally. On the local Unix
// socket, the OpenID Connect access tokens of the users are verified locally
// too, with the OpenID provider the daemon of the cluster named in the request
// header accepts, before they reach that daemon, which decides the access.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	maxMCPBodyBytes = 1 << 20
	// ClusterIDHeader selects the cluster whose daemon verifies a delegated
	// token. It is never trusted as an identity on its own.
	ClusterIDHeader = "X-OpenSVC-Cluster-ID"
	// NodeHeader is accepted from OpenSVC clients but routing uses the cluster
	// VIP; it is removed so that it never reaches a daemon.
	NodeHeader = "X-OpenSVC-Node"
)

var errUnauthorized = errors.New("invalid OpenID Connect access token or target")

// ErrNoOpenID is the error of a cluster whose daemon accepts no OpenID
// Connect token: no token of the socket can reach it.
var ErrNoOpenID = errors.New("the daemon of the cluster accepts no OpenID Connect token")

// TrustSource gives the OpenID provider the daemon of a cluster accepts: the
// issuer and the client ID its tokens are intended for. It returns
// ErrNoOpenID, or an error wrapping mcpauth.ErrVerificationUnavailable when
// the answer of the daemon is unknown.
type TrustSource interface {
	OpenIDTrust(ctx context.Context, clusterID string) (issuer, clientID string, err error)
}

type delegatedClaims struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	jwt.RegisteredClaims
}

// Delegation describes the claims of a delegated OpenID Connect access token,
// whose signature, issuer, audience and lifetime the middleware verified.
// The selected daemon still decides the access of that identity.
type Delegation struct {
	ClusterID string
	Issuer    string
	Subject   string
	ExpiresAt time.Time
	// Username is the name the daemon derives from the token:
	// preferred_username, else email, else sub.
	Username string
}

// Delegator checks delegated tokens against the clusters of the catalogue,
// and verifies them with the OpenID provider of the selected cluster.
type Delegator struct {
	clusters map[string]bool
	trust    TrustSource
	verifier *mcpauth.JWTVerifier
}

func NewDelegator(catalog *clusterconfig.Catalog, trust TrustSource, verifier *mcpauth.JWTVerifier) (*Delegator, error) {
	if catalog.Len() == 0 {
		return nil, errors.New("token delegation requires a cluster catalogue")
	}
	if trust == nil || verifier == nil {
		return nil, errors.New("token delegation requires the OpenID trust of the clusters")
	}
	d := &Delegator{clusters: make(map[string]bool), trust: trust, verifier: verifier}
	for _, cluster := range catalog.List() {
		d.clusters[cluster.ID] = true
	}
	return d, nil
}

// Check selects the cluster from the header and reads, unverified, the
// identity the daemon of that cluster derives from an OpenID Connect access
// token. Verify then proves the token before any daemon request.
func (d *Delegator) Check(raw, clusterID string) (Delegation, error) {
	if raw == "" || len(raw) > mcpauth.MaxTokenBytes || !validTarget(clusterID) || !d.clusters[clusterID] {
		return Delegation{}, errUnauthorized
	}
	var parsed delegatedClaims
	token, _, err := jwt.NewParser().ParseUnverified(raw, &parsed)
	// Keeping the token shape does not prove the signature is genuine.
	if err != nil || token == nil {
		return Delegation{}, errUnauthorized
	}
	if err := jwt.NewValidator(jwt.WithExpirationRequired()).Validate(&parsed); err != nil {
		return Delegation{}, errUnauthorized
	}
	if !mcpauth.ValidClaimText(parsed.Subject) || !mcpauth.ValidClaimText(parsed.Issuer) || parsed.ExpiresAt == nil {
		return Delegation{}, errUnauthorized
	}
	if len(parsed.Audience) == 0 || !mcpauth.OpenIDSigningMethod(token.Method.Alg()) {
		return Delegation{}, errUnauthorized
	}
	kid, ok := token.Header["kid"].(string)
	if !ok || !mcpauth.ValidClaimText(kid) {
		return Delegation{}, errUnauthorized
	}
	for _, audience := range parsed.Audience {
		if !mcpauth.ValidClaimText(audience) {
			return Delegation{}, errUnauthorized
		}
	}
	// Match the daemon's OpenID username selection. Subject remains the
	// opaque JWT sub for the agent's conversation ownership.
	username := parsed.Subject
	if parsed.PreferredUsername != "" {
		username = parsed.PreferredUsername
	} else if parsed.Email != "" {
		username = parsed.Email
	}
	if !mcpauth.ValidClaimText(username) {
		return Delegation{}, errUnauthorized
	}
	return Delegation{ClusterID: clusterID, Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time, Username: username}, nil
}

func validTarget(value string) bool {
	return mcpauth.ValidClaimText(value) && !strings.Contains(value, ",")
}

type delegationContextKey struct{}
type delegatedCredential struct {
	delegation Delegation
	token      string
}

// DelegationFromContext never returns credentials for an expired request.
// Only the delegation middleware populates this request-scoped context, with
// a verified token; the daemon still decides its access.
func DelegationFromContext(ctx context.Context) (Delegation, string, bool) {
	c, ok := ctx.Value(delegationContextKey{}).(delegatedCredential)
	if !ok || ctx.Err() != nil || !time.Now().Before(c.delegation.ExpiresAt) {
		return Delegation{}, "", false
	}
	return c.delegation, c.token, true
}

// Middleware requires one Bearer token and one cluster ID header. It is only
// mounted on the local Unix socket, never on the OAuth HTTPS listener.
func (d *Delegator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		var raw string
		if len(values) == 1 {
			parts := strings.Fields(values[0])
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				raw = parts[1]
			}
		}
		clusters := r.Header.Values(ClusterIDHeader)
		var clusterID string
		if len(clusters) == 1 {
			clusterID = clusters[0]
		}
		delegation, err := d.Check(raw, clusterID)
		// Tokens in query strings are never an alternative authentication path.
		if err == nil && len(values) == 1 && len(clusters) == 1 && !r.URL.Query().Has("access_token") {
			err = d.verify(r.Context(), raw, delegation)
		}
		if errors.Is(err, mcpauth.ErrVerificationUnavailable) {
			w.Header().Set("Retry-After", "30")
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"The OpenID provider of the cluster could not be read; retry later."}`))
			return
		}
		if err != nil || len(values) != 1 || len(clusters) != 1 || r.URL.Query().Has("access_token") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"An OpenID Connect access token and a configured X-OpenSVC-Cluster-ID are required."}`))
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), delegation.ExpiresAt)
		defer cancel()
		ctx = context.WithValue(ctx, delegationContextKey{}, delegatedCredential{delegation: delegation, token: raw})
		request := r.Clone(ctx)
		request.Header.Del("Authorization")
		request.Header.Del(ClusterIDHeader)
		request.Header.Del(NodeHeader)
		request.Body = http.MaxBytesReader(w, request.Body, maxMCPBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request)
	})
}

// verify proves the token with the OpenID provider the daemon of the selected
// cluster accepts: signature by a key of its JWKS, issuer, audience (its
// client ID) and lifetime. A refused token never reaches the daemon.
func (d *Delegator) verify(ctx context.Context, raw string, delegation Delegation) error {
	issuer, clientID, err := d.trust.OpenIDTrust(ctx, delegation.ClusterID)
	if err != nil {
		if errors.Is(err, mcpauth.ErrVerificationUnavailable) {
			return err
		}
		return errUnauthorized
	}
	// A token of another issuer is refused without loading any key.
	if delegation.Issuer != issuer {
		return errUnauthorized
	}
	_, err = d.verifier.Verify(ctx, raw, issuer, clientID)
	return err
}
