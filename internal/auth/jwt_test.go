package auth

import (
	"context"
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

func TestCheckNativeJWTClaimsWithoutSignature(t *testing.T) {
	key := testutil.NewJWTKey(t)
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "cluster-id", "", map[string]string{"node-a": "https://192.0.2.20:1215"}))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewChecker(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw := testutil.AccessToken(t, key, "cluster-id", "node-a", "alice", nil)
	identity, err := v.Check(raw, "")
	if err != nil || identity.ClusterID != "cluster-id" || identity.Issuer != "node-a" || identity.Subject != "alice" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	other := testutil.NewJWTKey(t)
	badSignature := testutil.AccessToken(t, other, "cluster-id", "node-a", "alice", nil)
	if _, err := v.Check(badSignature, ""); err != nil {
		t.Fatal("signature was checked locally instead of delegated to the daemon")
	}
	hmac, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"cluster_id": "cluster-id", "iss": "node-a", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix(), "token_use": "access"}).SignedString([]byte("not-a-trusted-key"))
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{"empty": "", "malformed": "not-a-token", "oversized": strings.Repeat("x", maxTokenBytes+1), "wrong algorithm": hmac} {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Check(raw, ""); err != errUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
	for name, change := range map[string]func(jwt.MapClaims){
		"no ID":          func(c jwt.MapClaims) { delete(c, "cluster_id") },
		"unknown ID":     func(c jwt.MapClaims) { c["cluster_id"] = "other-id" },
		"non-string ID":  func(c jwt.MapClaims) { c["cluster_id"] = 42 },
		"unknown issuer": func(c jwt.MapClaims) { c["iss"] = "node-b" },
		"issuer URL":     func(c jwt.MapClaims) { c["iss"] = "https://attacker.invalid" },
		"no issuer":      func(c jwt.MapClaims) { delete(c, "iss") },
		"no subject":     func(c jwt.MapClaims) { delete(c, "sub") },
		"no expiry":      func(c jwt.MapClaims) { delete(c, "exp") },
		"expired":        func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Second).Unix() },
		"not yet valid":  func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() },
		"refresh":        func(c jwt.MapClaims) { c["token_use"] = "refresh" },
		"no use":         func(c jwt.MapClaims) { delete(c, "token_use") },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Check(testutil.AccessToken(t, key, "cluster-id", "node-a", "alice", change), ""); err != errUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := NewChecker(nil); err == nil {
		t.Fatal("empty catalogue accepted")
	}
}

func TestMiddlewareRequestScopedCredentialAndExpiry(t *testing.T) {
	key := testutil.NewJWTKey(t)
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "id", "", map[string]string{"node-a": "https://192.0.2.20:1215"}))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewChecker(catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw := testutil.AccessToken(t, key, "id", "node-a", "alice", nil)
	var saved context.Context
	var calls int
	handler := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		saved = r.Context()
		identity, token, ok := FromContext(saved)
		if !ok || identity.Subject != "alice" || token != raw {
			t.Fatal("checked delegation missing")
		}
		deadline, ok := saved.Deadline()
		if !ok || !deadline.Equal(identity.ExpiresAt) {
			t.Fatal("token expiry not propagated")
		}
		w.WriteHeader(204)
	}))
	request := httptest.NewRequest("POST", "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 || calls != 1 {
		t.Fatal("valid bearer rejected")
	}
	if _, _, ok := FromContext(saved); ok {
		t.Fatal("credentials still usable after request cancellation")
	}
	if _, _, ok := FromContext(context.Background()); ok {
		t.Fatal("credential leaked outside context")
	}
	expired := context.WithValue(context.Background(), contextKey{}, credential{delegation: Delegation{ExpiresAt: time.Now().Add(-time.Second)}, token: raw})
	if _, _, ok := FromContext(expired); ok {
		t.Fatal("expired credentials still usable")
	}
	for _, tc := range []struct {
		path    string
		headers []string
	}{
		{"/mcp", nil}, {"/mcp", []string{"Basic " + raw}}, {"/mcp", []string{"Bearer bad"}},
		{"/mcp", []string{"Bearer " + raw, "Bearer " + raw}}, {"/mcp?access_token=" + raw, nil},
		{"/mcp?access_token=" + raw, []string{"Bearer " + raw}},
	} {
		request := httptest.NewRequest("POST", tc.path, nil)
		for _, header := range tc.headers {
			request.Header.Add("Authorization", header)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 || response.Header().Get("WWW-Authenticate") != "Bearer" || strings.Contains(response.Body.String(), raw) || calls != 1 {
			t.Fatal("invalid credentials admitted or leaked")
		}
	}
}

func TestMiddlewareBoundsBody(t *testing.T) {
	key := testutil.NewJWTKey(t)
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example", "id", "", map[string]string{"node-a": "https://192.0.2.20:1215"}))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewChecker(catalog)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/mcp", strings.NewReader(strings.Repeat("x", maxMCPBodyBytes+1)))
	request.Header.Set("Authorization", "Bearer "+testutil.AccessToken(t, key, "id", "node-a", "alice", nil))
	v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		var oversized *http.MaxBytesError
		if len(data) != maxMCPBodyBytes || !errors.As(err, &oversized) {
			t.Fatalf("body not bounded: bytes=%d error=%v", len(data), err)
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
}
