// Package auth authenticates MCP callers. On the HTTPS listener, OAuth access
// JWTs intended for the MCP resource are verified locally. On the local Unix
// socket, delegated OpenSVC daemon tokens are checked for routing only and
// verified by the daemon of the cluster named in the request header.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

const (
	maxTokenBytes   = 32 << 10
	maxMCPBodyBytes = 1 << 20
	// ClusterIDHeader selects the cluster whose daemon verifies a delegated
	// token. It is never trusted as an identity on its own.
	ClusterIDHeader = "X-OpenSVC-Cluster-ID"
	// nodeHeader is accepted from OpenSVC clients but routing uses the cluster
	// VIP; it is removed so that it never reaches a daemon.
	nodeHeader     = "X-OpenSVC-Node"
	NativeStrategy = "jwt"
	OpenIDStrategy = "jwt-openid"
)

var errUnauthorized = errors.New("invalid OpenSVC access token or target")

type delegatedClaims struct {
	TokenUse          string `json:"token_use"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	jwt.RegisteredClaims
}

// Delegation describes UNVERIFIED claims of a delegated daemon token. They
// only predict the identity that the selected daemon must confirm; they never
// establish an identity by themselves.
type Delegation struct {
	ClusterID string
	Issuer    string
	Subject   string
	ExpiresAt time.Time
	Strategy  string
	Username  string
}

// Delegator checks delegated tokens against the clusters of the catalogue.
type Delegator struct {
	clusters map[string]bool
}

func NewDelegator(catalog *clusterconfig.Catalog) (*Delegator, error) {
	if catalog.Len() == 0 {
		return nil, errors.New("token delegation requires a cluster catalogue")
	}
	d := &Delegator{clusters: make(map[string]bool)}
	for _, cluster := range catalog.List() {
		d.clusters[cluster.ID] = true
	}
	return d, nil
}

// Check selects the cluster from the header and predicts the daemon identity.
// Native tokens are marked by token_use; others follow the OpenID profile.
func (d *Delegator) Check(raw, clusterID string) (Delegation, error) {
	if raw == "" || len(raw) > maxTokenBytes || !validTarget(clusterID) || !d.clusters[clusterID] {
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
	if !validClaimText(parsed.Subject) || !validClaimText(parsed.Issuer) || parsed.ExpiresAt == nil {
		return Delegation{}, errUnauthorized
	}
	strategy, username := NativeStrategy, parsed.Subject
	if parsed.TokenUse != "" {
		// An incomplete native token must not fall back to the OpenID profile.
		if token.Method != jwt.SigningMethodRS256 || parsed.TokenUse != "access" {
			return Delegation{}, errUnauthorized
		}
	} else {
		if len(parsed.Audience) == 0 || !openIDSigningMethod(token.Method.Alg()) {
			return Delegation{}, errUnauthorized
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || !validClaimText(kid) {
			return Delegation{}, errUnauthorized
		}
		for _, audience := range parsed.Audience {
			if !validClaimText(audience) {
				return Delegation{}, errUnauthorized
			}
		}
		// Match the daemon's OpenID username selection. Subject remains the
		// opaque JWT sub for the agent's conversation ownership.
		if parsed.PreferredUsername != "" {
			username = parsed.PreferredUsername
		} else if parsed.Email != "" {
			username = parsed.Email
		}
		if !validClaimText(username) {
			return Delegation{}, errUnauthorized
		}
		strategy = OpenIDStrategy
	}
	return Delegation{ClusterID: clusterID, Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time, Strategy: strategy, Username: username}, nil
}

func openIDSigningMethod(algorithm string) bool {
	switch algorithm {
	case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512":
		return true
	default:
		return false
	}
}

func validClaimText(s string) bool {
	return len(s) > 0 && len(s) <= 256 && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) })
}

func validTarget(value string) bool {
	return validClaimText(value) && !strings.Contains(value, ",")
}

type delegationContextKey struct{}
type delegatedCredential struct {
	delegation Delegation
	token      string
}

// DelegationFromContext never returns credentials for an expired request.
// Only the delegation middleware populates this request-scoped context. The
// claims remain unverified: the daemon alone authenticates the bearer.
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
		if err != nil || len(values) != 1 || len(clusters) != 1 || r.URL.Query().Has("access_token") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"An OpenSVC access JWT and a configured X-OpenSVC-Cluster-ID are required."}`))
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), delegation.ExpiresAt)
		defer cancel()
		ctx = context.WithValue(ctx, delegationContextKey{}, delegatedCredential{delegation: delegation, token: raw})
		request := r.Clone(ctx)
		request.Header.Del("Authorization")
		request.Header.Del(ClusterIDHeader)
		request.Header.Del(nodeHeader)
		request.Body = http.MaxBytesReader(w, request.Body, maxMCPBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request)
	})
}
