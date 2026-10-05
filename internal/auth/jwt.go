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
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

const (
	maxTokenBytes   = 32 << 10
	maxMCPBodyBytes = 1 << 20
	ClusterIDHeader = "X-OpenSVC-Cluster-ID"
	NodeHeader      = "X-OpenSVC-Node"
	NativeStrategy  = "jwt"
	OpenIDStrategy  = "jwt-openid"
)

var errUnauthorized = errors.New("invalid OpenSVC access token or target")

type claims struct {
	ClusterID         string `json:"cluster_id"`
	TokenUse          string `json:"token_use"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	jwt.RegisteredClaims
}

// Delegation describes UNVERIFIED claims for routing only. It is not an
// authenticated identity and must never grant access to MCP-local user data.
type Delegation struct {
	ClusterID string
	Issuer    string
	Subject   string
	ExpiresAt time.Time
	Node      string
	Strategy  string
	Username  string
}

type Checker struct {
	nodes map[string]map[string]string
}

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

func (c *Checker) Check(raw, targetCluster, targetNode string) (Delegation, error) {
	if raw == "" || len(raw) > maxTokenBytes {
		return Delegation{}, errUnauthorized
	}
	if targetCluster != "" && !validTarget(targetCluster) || targetNode != "" && !validTarget(targetNode) {
		return Delegation{}, errUnauthorized
	}
	var parsed claims
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
	node, strategy, username := parsed.Issuer, NativeStrategy, parsed.Subject
	if parsed.ClusterID != "" || parsed.TokenUse != "" {
		// An incomplete native token must not fall back to the OpenID profile.
		if token.Method != jwt.SigningMethodRS256 || !validClaimText(parsed.ClusterID) || parsed.TokenUse != "access" || targetCluster != "" && targetCluster != parsed.ClusterID {
			return Delegation{}, errUnauthorized
		}
		targetCluster = parsed.ClusterID
		if targetNode != "" && targetNode != parsed.Issuer {
			return Delegation{}, errUnauthorized
		}
	} else {
		if targetCluster == "" || targetNode == "" || len(parsed.Audience) == 0 || !openIDSigningMethod(token.Method.Alg()) {
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
		node, strategy = targetNode, OpenIDStrategy
	}
	nodes, ok := c.nodes[targetCluster]
	if !ok || node == "" {
		return Delegation{}, errUnauthorized
	}
	if _, ok := nodes[node]; !ok {
		return Delegation{}, errUnauthorized
	}
	return Delegation{ClusterID: targetCluster, Issuer: parsed.Issuer, Subject: parsed.Subject, ExpiresAt: parsed.ExpiresAt.Time, Node: node, Strategy: strategy, Username: username}, nil
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

func targetFromHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 || !validTarget(values[0]) {
		return "", false
	}
	return values[0], true
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
		targetCluster, clusterOK := targetFromHeader(r.Header, ClusterIDHeader)
		targetNode, nodeOK := targetFromHeader(r.Header, NodeHeader)
		delegation, err := c.Check(raw, targetCluster, targetNode)
		// Tokens in query strings are never an alternative authentication path.
		if err != nil || !clusterOK || !nodeOK || r.URL.Query().Has("access_token") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"An OpenSVC access JWT with acceptable claims and target is required."}`))
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), delegation.ExpiresAt)
		defer cancel()
		ctx = context.WithValue(ctx, contextKey{}, credential{delegation: delegation, token: raw})
		r.Header.Del("Authorization")
		r.Header.Del(ClusterIDHeader)
		r.Header.Del(NodeHeader)
		r.Body = http.MaxBytesReader(w, r.Body, maxMCPBodyBytes)
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
