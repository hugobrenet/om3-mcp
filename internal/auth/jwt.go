// Package auth checks delegated JWT claims and routing, not signatures.
// Only the target daemon authenticates the credential.
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
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
)

const (
	maxTokenBytes   = 32 << 10
	maxMCPBodyBytes = 1 << 20
)

var errUnauthorized = errors.New("invalid native OpenSVC access token")

type claims struct {
	ClusterID string `json:"cluster_id"`
	TokenUse  string `json:"token_use"`
	jwt.RegisteredClaims
}

// Delegation describes UNVERIFIED claims for routing only. It is not an
// authenticated identity and must never grant access to MCP-local user data.
type Delegation struct {
	ClusterID string
	Issuer    string
	Subject   string
	ExpiresAt time.Time
}

type Checker struct{ nodes map[string]map[string]string }

func NewChecker(catalog *clusterconfig.Catalog) (*Checker, error) {
	if catalog.Len() == 0 {
		return nil, errors.New("JWT delegation requires a cluster catalogue")
	}
	c := &Checker{nodes: make(map[string]map[string]string)}
	for _, cluster := range catalog.List() {
		c.nodes[cluster.ExpectedClusterID] = cluster.Nodes
	}
	return c, nil
}

func (c *Checker) Check(raw string) (Delegation, error) {
	if raw == "" || len(raw) > maxTokenBytes {
		return Delegation{}, errUnauthorized
	}
	var parsed claims
	token, _, err := jwt.NewParser().ParseUnverified(raw, &parsed)
	// Keeping the native token shape does not prove the signature is genuine.
	if err != nil || token == nil || token.Method != jwt.SigningMethodRS256 {
		return Delegation{}, errUnauthorized
	}
	if err := jwt.NewValidator(jwt.WithExpirationRequired()).Validate(&parsed); err != nil {
		return Delegation{}, errUnauthorized
	}
	if !validClaimText(parsed.Subject) || parsed.TokenUse != "access" || parsed.ExpiresAt == nil {
		return Delegation{}, errUnauthorized
	}
	nodes, ok := c.nodes[parsed.ClusterID]
	if !ok {
		return Delegation{}, errUnauthorized
	}
	if _, ok := nodes[parsed.Issuer]; !ok {
		return Delegation{}, errUnauthorized
	}
	return Delegation{ClusterID: parsed.ClusterID, Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time}, nil
}

func validClaimText(s string) bool {
	return len(s) > 0 && len(s) <= 256 && strings.TrimSpace(s) == s && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) })
}

type contextKey struct{}
type credential struct {
	delegation Delegation
	token      string
}

// FromContext never returns credentials for an expired request. Only the
// claim-checking middleware can populate this request-scoped context. These
// claims remain unverified: the daemon alone authenticates the bearer.
func FromContext(ctx context.Context) (Delegation, string, bool) {
	c, ok := ctx.Value(contextKey{}).(credential)
	if !ok || ctx.Err() != nil || !time.Now().Before(c.delegation.ExpiresAt) {
		return Delegation{}, "", false
	}
	return c.delegation, c.token, true
}

func (c *Checker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		var raw string
		if len(values) == 1 {
			parts := strings.Fields(values[0])
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				raw = parts[1]
			}
		}
		delegation, err := c.Check(raw)
		// Tokens in query strings are never an alternative authentication path.
		if err != nil || r.URL.Query().Has("access_token") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"A native OpenSVC access JWT with acceptable claims is required."}`))
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), delegation.ExpiresAt)
		defer cancel()
		ctx = context.WithValue(ctx, contextKey{}, credential{delegation: delegation, token: raw})
		r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
