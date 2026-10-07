package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func TestExchangeUsesAuthenticatedSubjectAndConfidentialClient(t *testing.T) {
	for _, method := range []string{"client_secret_basic", "client_secret_post"} {
		t.Run(method, func(t *testing.T) {
			p := testutil.NewOAuthProvider(t)
			p.Metadata["token_endpoint"] = p.Server.URL + "/token"
			profiles, err := LoadExchangeProfiles(testutil.WriteExchangeProfiles(t, p, method))
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			subject := p.Token(t, testResource, "alice", nil)
			outgoing := p.Token(t, "daemon-a", "alice", func(c jwt.MapClaims) { c["entitlements"] = []string{"guest:test"} })
			p.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.ParseForm() != nil {
					t.Error("invalid exchange HTTP request")
				}
				if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" || r.Form.Get("subject_token") != subject || r.Form.Get("subject_token_type") != accessTokenType || r.Form.Get("requested_token_type") != accessTokenType || r.Form.Get("audience") != "daemon-a" || r.Form.Get("scope") != "openid profile" {
					t.Error("invalid exchange contract")
				}
				if r.Form.Has("resource") || r.Form.Has("actor_token") || r.Header.Get("Cookie") != "" {
					t.Error("unexpected exchange parameters")
				}
				if method == "client_secret_basic" {
					id, secret, ok := r.BasicAuth()
					decoded, _ := url.QueryUnescape(secret)
					if !ok || id != "test-exchange" || decoded != "test-secret&+" || r.Form.Has("client_secret") {
						t.Error("invalid basic client credentials")
					}
				} else if r.Form.Get("client_id") != "test-exchange" || r.Form.Get("client_secret") != "test-secret&+" || r.Header.Get("Authorization") != "" {
					t.Error("invalid post client credentials")
				}
				json.NewEncoder(w).Encode(map[string]any{"access_token": outgoing, "issued_token_type": accessTokenType, "token_type": "Bearer", "expires_in": 60})
			}
			if _, _, err := profiles.Prepare(t.Context(), "test-sso", "daemon-a", "cluster-a"); err == nil || calls.Load() != 0 {
				t.Fatal("unauthenticated exchange attempted")
			}
			v := newTestVerifier(t, p)
			var saved context.Context
			h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel, err := profiles.Prepare(r.Context(), "test-sso", "daemon-a", "cluster-a")
				if err != nil {
					t.Error(err)
					return
				}
				defer cancel()
				saved = ctx
				id, token, ok := ExchangedFromContext(ctx)
				if !ok || id != "cluster-a" || token != outgoing {
					t.Error("incorrect credential binding")
				}
				if _, _, ok := FromContext(ctx); ok {
					t.Error("exchange became legacy delegation")
				}
				deadline, _ := ctx.Deadline()
				if time.Until(deadline) > 61*time.Second {
					t.Error("expires_in did not bound lifetime")
				}
				w.WriteHeader(204)
			}))
			for range 2 {
				if w := oauthRequest(h, subject); w.Code != 204 {
					t.Fatalf("status %d", w.Code)
				}
			}
			if calls.Load() != 2 {
				t.Fatal("tokens cached across requests")
			}
			if _, _, ok := ExchangedFromContext(saved); ok {
				t.Fatal("credential survived call completion")
			}
		})
	}
}

func TestExchangeRefusalsAreBoundedAndNeverExposeSSOData(t *testing.T) {
	p := testutil.NewOAuthProvider(t)
	p.Metadata["token_endpoint"] = p.Server.URL + "/token"
	profiles, err := LoadExchangeProfiles(testutil.WriteExchangeProfiles(t, p, "client_secret_basic"))
	if err != nil {
		t.Fatal(err)
	}
	subject := p.Token(t, testResource, "alice", nil)
	identity := OAuthIdentity{Issuer: p.Issuer, Subject: "alice", Resource: testResource, ExpiresAt: time.Now().Add(time.Hour)}
	ctx := context.WithValue(t.Context(), oauthContextKey{}, oauthCredential{identity: identity, token: subject})
	valid := p.Token(t, "daemon-a", "alice", nil)
	for _, tc := range []struct {
		name   string
		status int
		body   any
	}{
		{"refused", 400, map[string]any{"error": "invalid_target", "error_description": "private-upstream-secret"}},
		{"unknown error", 500, map[string]any{"error": "private-upstream-secret"}},
		{"bad type", 200, map[string]any{"access_token": valid, "token_type": "MAC", "issued_token_type": accessTokenType}},
		{"id token", 200, map[string]any{"access_token": valid, "token_type": "Bearer", "issued_token_type": "urn:ietf:params:oauth:token-type:id_token"}},
		{"passthrough", 200, map[string]any{"access_token": subject, "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"wrong audience", 200, map[string]any{"access_token": p.Token(t, "daemon-b", "alice", nil), "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"shared MCP audience", 200, map[string]any{"access_token": p.Token(t, "daemon-a", "alice", func(c jwt.MapClaims) { c["aud"] = []string{"daemon-a", testResource} }), "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"expired", 200, map[string]any{"access_token": p.Token(t, "daemon-a", "alice", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }), "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"opaque", 200, map[string]any{"access_token": "private-upstream-secret", "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"header injection", 200, map[string]any{"access_token": valid + "\r\nprivate-upstream-secret", "token_type": "Bearer", "issued_token_type": accessTokenType}},
		{"zero lifetime", 200, map[string]any{"access_token": valid, "token_type": "Bearer", "issued_token_type": accessTokenType, "expires_in": 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(tc.body)
			}
			_, _, err := profiles.Prepare(ctx, "test-sso", "daemon-a", "cluster-a")
			if err == nil {
				t.Fatal("invalid response accepted")
			}
			for _, secret := range []string{subject, valid, "private-upstream-secret", "test-secret"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("secret leaked in error")
				}
			}
		})
	}
	var redirected atomic.Int32
	trap := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer trap.Close()
	p.TokenHandler = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, trap.URL, 307) }
	if _, _, err := profiles.Prepare(ctx, "test-sso", "daemon-a", "cluster-a"); err == nil || redirected.Load() != 0 {
		t.Fatal("token exchange followed redirect")
	}
	p.TokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", maxOAuthDocumentBytes+1)))
	}
	if _, _, err := profiles.Prepare(ctx, "test-sso", "daemon-a", "cluster-a"); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestExchangeProfilesRejectUnsafeConfiguration(t *testing.T) {
	p := testutil.NewOAuthProvider(t)
	path := testutil.WriteExchangeProfiles(t, p, "client_secret_basic")
	base, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range [][2]string{{"client_secret_basic", "none"}, {"issuer: " + p.Issuer, "issuer: http://sso.test"}, {"request_timeout: 2s", "request_timeout: 0s"}, {"version: 1", "version: 2"}, {"client_id: test-exchange", "client_id: ''"}} {
		s := strings.Replace(string(base), replacement[0], replacement[1], 1)
		if s == string(base) {
			t.Fatal("test did not alter config")
		}
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadExchangeProfiles(path); err == nil {
			t.Fatalf("accepted %s", replacement[1])
		}
	}
}

func TestExchangeSecretFileAndDiscoveryTrust(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := exchangeFile(path, 1024, true); err == nil {
		t.Fatal("world-readable secret accepted")
	}
	if _, err := exchangeFile(t.TempDir(), 1024, true); err == nil {
		t.Fatal("nonregular secret accepted")
	}
	p := testutil.NewOAuthProvider(t)
	profiles, err := LoadExchangeProfiles(testutil.WriteExchangeProfiles(t, p, "client_secret_basic"))
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles.profiles["test-sso"]
	for _, metadata := range []map[string]any{
		{"issuer": "https://attacker.test", "token_endpoint": p.Server.URL + "/token"},
		{"issuer": p.Issuer, "token_endpoint": "http://attacker.test/token"},
		{"issuer": p.Issuer, "token_endpoint": p.Server.URL + "/token", "token_endpoint_auth_methods_supported": []string{"none"}},
	} {
		p.Metadata = metadata
		if _, err := profile.tokenEndpoint(t.Context()); err == nil {
			t.Fatal("untrusted discovery accepted")
		}
	}
	p.Metadata = map[string]any{"issuer": p.Issuer, "token_endpoint": p.Server.URL + "/token"}
	profile.client, err = oauthHTTPClient("", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.tokenEndpoint(t.Context()); err == nil {
		t.Fatal("untrusted SSO certificate accepted")
	}
}
