package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func openIDChecker(t *testing.T) *Checker {
	t.Helper()
	definition := func(id, defaultNode string) map[string]any {
		return map[string]any{"name": "Same display name", "expected_cluster_id": id, "default_node": defaultNode,
			"nodes": map[string]string{"node-a": "https://192.0.2.20:1215", "node-b": "https://192.0.2.21:1215"}, "request_timeout": "2s"}
	}
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, map[string]any{
		"a": definition("cluster-a", "node-b"), "b": definition("cluster-b", "node-a"), "native": definition("native-only", ""),
	}))
	if err != nil {
		t.Fatal(err)
	}
	checker, err := NewChecker(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return checker
}

func TestOpenIDClaimsAndExplicitRouting(t *testing.T) {
	checker := openIDChecker(t)
	key := testutil.NewJWTKey(t)
	const issuer = "https://idp.example.test/application/cluster/"
	raw := testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", nil)
	for target, node := range map[string]string{"cluster-a": "node-b", "cluster-b": "node-a"} {
		delegation, err := checker.Check(raw, target)
		if err != nil || delegation.ClusterID != target || delegation.Node != node || delegation.Issuer != issuer || delegation.Subject != "opaque-sub" || delegation.Username != "alice" || delegation.Strategy != OpenIDStrategy {
			t.Fatalf("target=%s delegation=%+v err=%v", target, delegation, err)
		}
	}
	// Structural checks cannot decide the expected audience or signature;
	// only the selected daemon may authenticate this candidate.
	for _, target := range []string{"", "unknown", "native-only", " cluster-a ", "cluster-a,cluster-b"} {
		if _, err := checker.Check(raw, target); err != errUnauthorized {
			t.Fatalf("invalid target %q accepted: %v", target, err)
		}
	}
	for name, change := range map[string]func(jwt.MapClaims){
		"issuer":                  func(c jwt.MapClaims) { delete(c, "iss") },
		"subject":                 func(c jwt.MapClaims) { delete(c, "sub") },
		"audience":                func(c jwt.MapClaims) { delete(c, "aud") },
		"empty audience":          func(c jwt.MapClaims) { c["aud"] = "" },
		"invalid audience":        func(c jwt.MapClaims) { c["aud"] = 42 },
		"invalid audience member": func(c jwt.MapClaims) { c["aud"] = []string{"client-a", ""} },
		"expiry":                  func(c jwt.MapClaims) { delete(c, "exp") },
		"expired":                 func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Second).Unix() },
		"not yet valid":           func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() },
		"bad username":            func(c jwt.MapClaims) { c["preferred_username"] = " padded " },
		"native marker":           func(c jwt.MapClaims) { c["cluster_id"] = "cluster-a" },
		"refresh marker":          func(c jwt.MapClaims) { c["token_use"] = "refresh" },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := checker.Check(testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", change), "cluster-a"); err != errUnauthorized {
				t.Fatalf("invalid claims accepted: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		change   func(jwt.MapClaims)
		username string
	}{
		{func(c jwt.MapClaims) { delete(c, "preferred_username") }, "alice@example.test"},
		{func(c jwt.MapClaims) { delete(c, "preferred_username"); delete(c, "email") }, "opaque-sub"},
		{func(c jwt.MapClaims) { c["aud"] = []string{"client-a", "another-client"} }, "alice"},
	} {
		delegation, err := checker.Check(testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", tc.change), "cluster-a")
		if err != nil || delegation.Username != tc.username || delegation.Subject != "opaque-sub" {
			t.Fatalf("username fallback: %+v %v", delegation, err)
		}
	}

	native := testutil.AccessToken(t, key, "cluster-a", "node-a", "alice", nil)
	for _, target := range []string{"", "cluster-a"} {
		delegation, err := checker.Check(native, target)
		if err != nil || delegation.Node != "node-a" || delegation.Strategy != NativeStrategy {
			t.Fatal("native routing used the default node")
		}
	}
	if _, err := checker.Check(native, "cluster-b"); err != errUnauthorized {
		t.Fatal("native cluster binding was overridden by the header")
	}
}

func TestOpenIDAlgorithmsAndKeyID(t *testing.T) {
	checker := openIDChecker(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{"iss": "https://idp.example.test/", "sub": "opaque", "aud": "client", "exp": time.Now().Add(time.Hour).Unix()}
	for _, kid := range []any{"example-key", nil, "", 42, " padded "} {
		token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		if kid != nil {
			token.Header["kid"] = kid
		}
		raw, err := token.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = checker.Check(raw, "cluster-a")
		if (err == nil) != (kid == "example-key") {
			t.Fatalf("kid=%v err=%v", kid, err)
		}
	}
	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodNone} {
		var signingKey any = []byte("untrusted")
		if method == jwt.SigningMethodNone {
			signingKey = jwt.UnsafeAllowNoneSignatureType
		}
		token := jwt.NewWithClaims(method, claims)
		token.Header["kid"] = "example-key"
		raw, err := token.SignedString(signingKey)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := checker.Check(raw, "cluster-a"); err != errUnauthorized {
			t.Fatal("unsafe algorithm accepted")
		}
	}
}

func TestMiddlewareRejectsAmbiguousTargetHeaders(t *testing.T) {
	checker := openIDChecker(t)
	raw := testutil.OpenIDToken(t, testutil.NewJWTKey(t), "https://idp.example.test/", "client", "opaque", nil)
	for _, headers := range [][]string{nil, {""}, {"cluster-a", "cluster-a"}, {"cluster-a,cluster-b"}, {" cluster-a "}, {strings.Repeat("x", 257)}, {"cluster-a\n"}, {"unknown"}, {"native-only"}, {"cluster-a"}} {
		called := false
		handler := checker.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			delegation, token, ok := FromContext(r.Context())
			if !ok || token != raw || delegation.Node != "node-b" || delegation.ClusterID != "cluster-a" {
				t.Fatal("OpenID request context lost routing or credentials")
			}
			if r.Header.Get("Authorization") != "" || r.Header.Get(ClusterIDHeader) != "" {
				t.Fatal("credentials remain in handler headers")
			}
			w.WriteHeader(204)
		}))
		request := httptest.NewRequest("POST", "/mcp", nil)
		request.Header.Set("Authorization", "Bearer "+raw)
		for _, header := range headers {
			request.Header.Add(ClusterIDHeader, header)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		wantValid := len(headers) == 1 && headers[0] == "cluster-a"
		if called != wantValid || wantValid && response.Code != 204 || !wantValid && response.Code != 401 {
			t.Fatalf("headers=%q status=%d called=%v", headers, response.Code, called)
		}
	}
}
