package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func testDelegator(t *testing.T) *Delegator {
	t.Helper()
	definition := func(id, endpoint string) map[string]any {
		return map[string]any{"name": "Same display name", "cluster_id": id, "endpoint": endpoint, "request_timeout": "2s"}
	}
	catalog, err := clusterconfig.Load(testutil.WriteCatalog(t, map[string]any{
		"a": definition("cluster-a", "https://192.0.2.20:1215"), "b": definition("cluster-b", "https://192.0.2.21:1215"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	delegator, err := NewDelegator(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return delegator
}

func TestDelegatedNativeClaimsWithoutSignature(t *testing.T) {
	d := testDelegator(t)
	key := testutil.NewJWTKey(t)
	raw := testutil.AccessToken(t, key, "node-a", "alice", nil)
	for _, cluster := range []string{"cluster-a", "cluster-b"} {
		delegation, err := d.Check(raw, cluster)
		if err != nil || delegation.ClusterID != cluster || delegation.Issuer != "node-a" || delegation.Subject != "alice" || delegation.Username != "alice" || delegation.Strategy != NativeStrategy {
			t.Fatalf("cluster=%s delegation=%+v err=%v", cluster, delegation, err)
		}
	}
	// The selected daemon verifies the signature; no key is checked here.
	if _, err := d.Check(testutil.AccessToken(t, testutil.NewJWTKey(t), "node-a", "alice", nil), "cluster-a"); err != nil {
		t.Fatal("signature was checked locally instead of delegated to the daemon")
	}
	// A legacy cluster claim never selects or overrides the target.
	if delegation, err := d.Check(testutil.AccessToken(t, key, "node-a", "alice", func(c jwt.MapClaims) { c["cluster_id"] = "cluster-b" }), "cluster-a"); err != nil || delegation.ClusterID != "cluster-a" {
		t.Fatalf("claim overrode the header: %+v %v", delegation, err)
	}
	for _, cluster := range []string{"", "unknown", " cluster-a ", "cluster-a,cluster-b", "cluster-a\n", strings.Repeat("x", 257)} {
		if _, err := d.Check(raw, cluster); err != errUnauthorized {
			t.Fatalf("invalid cluster %q accepted: %v", cluster, err)
		}
	}
	hmac, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "node-a", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix(), "token_use": "access"}).SignedString([]byte("not-a-trusted-key"))
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"empty": "", "malformed": "not-a-token", "oversized": strings.Repeat("x", maxTokenBytes+1), "wrong algorithm": hmac} {
		t.Run(name, func(t *testing.T) {
			if _, err := d.Check(raw, "cluster-a"); err != errUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
	for name, change := range map[string]func(jwt.MapClaims){
		"no issuer":     func(c jwt.MapClaims) { delete(c, "iss") },
		"no subject":    func(c jwt.MapClaims) { delete(c, "sub") },
		"no expiry":     func(c jwt.MapClaims) { delete(c, "exp") },
		"expired":       func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Second).Unix() },
		"not yet valid": func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() },
		"refresh":       func(c jwt.MapClaims) { c["token_use"] = "refresh" },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := d.Check(testutil.AccessToken(t, key, "node-a", "alice", change), "cluster-a"); err != errUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := NewDelegator(nil); err == nil {
		t.Fatal("empty catalogue accepted")
	}
}

func TestDelegatedOpenIDClaims(t *testing.T) {
	d := testDelegator(t)
	key := testutil.NewJWTKey(t)
	const issuer = "https://idp.example.test/application/cluster/"
	raw := testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", nil)
	delegation, err := d.Check(raw, "cluster-b")
	if err != nil || delegation.ClusterID != "cluster-b" || delegation.Issuer != issuer || delegation.Subject != "opaque-sub" || delegation.Username != "alice" || delegation.Strategy != OpenIDStrategy {
		t.Fatalf("delegation=%+v err=%v", delegation, err)
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
		"refresh marker":          func(c jwt.MapClaims) { c["token_use"] = "refresh" },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := d.Check(testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", change), "cluster-a"); err != errUnauthorized {
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
		delegation, err := d.Check(testutil.OpenIDToken(t, key, issuer, "client-a", "opaque-sub", tc.change), "cluster-a")
		if err != nil || delegation.Username != tc.username || delegation.Subject != "opaque-sub" {
			t.Fatalf("username fallback: %+v %v", delegation, err)
		}
	}
}

func TestDelegatedOpenIDAlgorithmsAndKeyID(t *testing.T) {
	d := testDelegator(t)
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
		_, err = d.Check(raw, "cluster-a")
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
		if _, err := d.Check(raw, "cluster-a"); err != errUnauthorized {
			t.Fatal("unsafe algorithm accepted")
		}
	}
}

func TestDelegationMiddlewareRequestScopedCredentialAndExpiry(t *testing.T) {
	d := testDelegator(t)
	raw := testutil.AccessToken(t, testutil.NewJWTKey(t), "node-a", "alice", nil)
	var saved context.Context
	var calls int
	handler := d.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		saved = r.Context()
		delegation, token, ok := DelegationFromContext(saved)
		if !ok || delegation.Subject != "alice" || delegation.ClusterID != "cluster-a" || token != raw {
			t.Fatal("checked delegation missing")
		}
		deadline, ok := saved.Deadline()
		if !ok || !deadline.Equal(delegation.ExpiresAt) {
			t.Fatal("token expiry not propagated")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get(ClusterIDHeader) != "" || r.Header.Get(nodeHeader) != "" {
			t.Fatal("credentials or targets remain in handler headers")
		}
		w.WriteHeader(204)
	}))
	request := httptest.NewRequest("POST", "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set(ClusterIDHeader, "cluster-a")
	request.Header.Set(nodeHeader, "ignored-node")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 || calls != 1 {
		t.Fatal("valid bearer rejected")
	}
	if _, _, ok := DelegationFromContext(saved); ok {
		t.Fatal("credentials still usable after request cancellation")
	}
	if _, _, ok := DelegationFromContext(context.Background()); ok {
		t.Fatal("credential leaked outside context")
	}
	expired := context.WithValue(context.Background(), delegationContextKey{}, delegatedCredential{delegation: Delegation{ExpiresAt: time.Now().Add(-time.Second)}, token: raw})
	if _, _, ok := DelegationFromContext(expired); ok {
		t.Fatal("expired credentials still usable")
	}
	for _, tc := range []struct {
		path     string
		headers  []string
		clusters []string
	}{
		{"/mcp", nil, []string{"cluster-a"}}, {"/mcp", []string{"Basic " + raw}, []string{"cluster-a"}}, {"/mcp", []string{"Bearer bad"}, []string{"cluster-a"}},
		{"/mcp", []string{"Bearer " + raw, "Bearer " + raw}, []string{"cluster-a"}}, {"/mcp?access_token=" + raw, nil, []string{"cluster-a"}},
		{"/mcp?access_token=" + raw, []string{"Bearer " + raw}, []string{"cluster-a"}},
		{"/mcp", []string{"Bearer " + raw}, nil}, {"/mcp", []string{"Bearer " + raw}, []string{""}},
		{"/mcp", []string{"Bearer " + raw}, []string{"cluster-a", "cluster-a"}}, {"/mcp", []string{"Bearer " + raw}, []string{"cluster-a,cluster-b"}},
		{"/mcp", []string{"Bearer " + raw}, []string{"unknown"}},
	} {
		request := httptest.NewRequest("POST", tc.path, nil)
		for _, header := range tc.headers {
			request.Header.Add("Authorization", header)
		}
		for _, cluster := range tc.clusters {
			request.Header.Add(ClusterIDHeader, cluster)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 || response.Header().Get("WWW-Authenticate") != "Bearer" || strings.Contains(response.Body.String(), raw) || calls != 1 {
			t.Fatalf("invalid credentials admitted or leaked: %+v", tc)
		}
	}
}

func TestDelegationMiddlewareBoundsBody(t *testing.T) {
	d := testDelegator(t)
	request := httptest.NewRequest("POST", "/mcp", strings.NewReader(strings.Repeat("x", maxMCPBodyBytes+1)))
	request.Header.Set("Authorization", "Bearer "+testutil.AccessToken(t, testutil.NewJWTKey(t), "node-a", "alice", nil))
	request.Header.Set(ClusterIDHeader, "cluster-a")
	d.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		var oversized *http.MaxBytesError
		if len(data) != maxMCPBodyBytes || !errors.As(err, &oversized) {
			t.Fatalf("body not bounded: bytes=%d error=%v", len(data), err)
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
}
