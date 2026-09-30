package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
)

func loginFixture(t *testing.T, afterToken func(http.ResponseWriter, *http.Request)) (*Server, string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	var token string
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/token":
			user, password, ok := r.BasicAuth()
			if !ok || user != "alice" || password != "synthetic-password" {
				w.WriteHeader(401)
				w.Write([]byte("untrusted daemon error containing synthetic-password"))
				return
			}
			if r.URL.Query().Get("refresh") != "false" {
				t.Error("refresh requested")
			}
			json.NewEncoder(w).Encode(map[string]string{"access_token": token})
		case "/api/cluster/status":
			if afterToken != nil {
				afterToken(w, r)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("missing JWT for identity check")
			}
			json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"config": map[string]string{"id": "example-id"}}})
		default:
			t.Error("unexpected daemon route")
		}
	}))
	var err error
	token, err = jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "alice", "iss": "node-a", "exp": time.Now().Add(10 * time.Minute).Unix(), "token_use": "access", "grant": []string{"guest:example"}}).SignedString(d.CAKey)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := clusterconfig.Load(testutil.WriteTarget(t, "Example cluster", "example-id", d.CAFile, []string{d.Server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Config{PublicURL: "https://192.0.2.10", Clusters: catalog}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return server, token, &calls
}

func startLogin(t *testing.T, s *Server) (*http.Cookie, url.Values) {
	t.Helper()
	client := registerClient(t, s, "Example agent")
	w := request(s, "GET", "/authorize?"+authorizationQuery(s, client).Encode(), "")
	cookie := loginCookie(t, w)
	page := request(s, "GET", "/login", "", cookie)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `name="csrf_token"`) || strings.Contains(page.Body.String(), "fieldset disabled") {
		t.Fatal("active form not displayed")
	}
	return cookie, url.Values{"cluster_ref": {"cluster-a"}, "username": {"alice"}, "secret": {"synthetic-password"}, "csrf_token": {s.requests[cookie.Value].CSRFToken}}
}

func postLogin(s *Server, cookie *http.Cookie, body string, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", s.cfg.PublicURL+"/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", origin)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestLoginStoresJWTServerSideAndRotatesCookie(t *testing.T) {
	s, token, calls := loginFixture(t, nil)
	cookie, form := startLogin(t, s)
	original := s.requests[cookie.Value]
	w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL)
	if w.Code != 303 || w.Header().Get("Location") != s.cfg.PublicURL+"/login" || calls.Load() != 2 || len(s.requests) != 0 || len(s.sessions) != 1 {
		t.Fatalf("login contract: status=%d calls=%d requests=%d sessions=%d", w.Code, calls.Load(), len(s.requests), len(s.sessions))
	}
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.Secure || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode || sessionCookie.Value == cookie.Value || sessionCookie.Path != "/" || sessionCookie.MaxAge <= 0 {
		t.Fatal("session cookie is missing or unsafe")
	}
	session := s.sessions[sessionCookie.Value]
	if session.Daemon.AccessToken != token || session.Daemon.Username != "alice" || session.Daemon.ClusterRef != "cluster-a" || session.Authorization.ClientID != original.ClientID || session.Authorization.State != original.State || session.Authorization.CodeChallenge != original.CodeChallenge || session.Authorization.CSRFToken != "" {
		t.Fatal("authenticated session lost its identity or authorization binding")
	}
	confirmation := request(s, "GET", "/login", "", sessionCookie)
	if confirmation.Code != 200 || !strings.Contains(confirmation.Body.String(), "Authentification OpenSVC réussie") || !strings.Contains(confirmation.Body.String(), "alice") || !strings.Contains(confirmation.Body.String(), "Example cluster") || !strings.Contains(confirmation.Body.String(), `action="/consent"`) || !strings.Contains(confirmation.Body.String(), `value="allow"`) || !strings.Contains(confirmation.Body.String(), `value="deny"`) {
		t.Fatal("confirmation is missing")
	}
	for _, response := range []*httptest.ResponseRecorder{w, confirmation} {
		body := response.Body.String() + response.Header().Get("Set-Cookie")
		for _, secret := range []string{token, "synthetic-password", original.State, original.CodeChallenge} {
			if strings.Contains(body, secret) {
				t.Fatal("browser received secret or authorization context")
			}
		}
	}
	if replay := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL); replay.Code != 400 || calls.Load() != 2 {
		t.Fatal("consumed form was replayed")
	}
	// A verified daemon token is never an MCP credential, even after login.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		for _, cookie := range []*http.Cookie{nil, sessionCookie} {
			r := httptest.NewRequest(method, s.cfg.PublicURL+"/mcp", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), s.cfg.PublicURL+"/.well-known/oauth-protected-resource/mcp") || strings.Contains(w.Body.String(), token) {
				t.Fatal("daemon JWT bypassed the OAuth challenge")
			}
		}
	}
	// Browser state alone does not authorize MCP or redeem a code.
	if request(s, "GET", "/mcp", "", sessionCookie).Code != 401 || request(s, "POST", "/token", "", sessionCookie).Code != 400 {
		t.Fatal("OpenSVC JWT escaped into MCP authorization")
	}
	restarted, err := New(s.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if request(restarted, "GET", "/login", "", sessionCookie).Code != 400 {
		t.Fatal("session survived a process restart")
	}
	s.now = func() time.Time { return session.Daemon.ExpiresAt }
	if request(s, "GET", "/login", "", sessionCookie).Code != 400 || len(s.sessions) != 0 {
		t.Fatal("expired JWT retained an active session")
	}
}

func TestLoginRejectsUntrustedFormsBeforeDaemonCalls(t *testing.T) {
	s, _, calls := loginFixture(t, nil)
	cookie, base := startLogin(t, s)
	for _, tc := range []struct {
		name   string
		change func(url.Values)
		extra  string
		origin string
		want   int
	}{
		{"cross origin", nil, "", "https://attacker.example", 403},
		{"absent origin", nil, "", "", 403},
		{"null origin", nil, "", "null", 403},
		{"bad csrf", func(v url.Values) { v.Set("csrf_token", "wrong") }, "", s.cfg.PublicURL, 403},
		{"unknown cluster", func(v url.Values) { v.Set("cluster_ref", "unknown") }, "", s.cfg.PublicURL, 400},
		{"endpoint injection", nil, "&endpoint=https%3A%2F%2Fattacker.example", s.cfg.PublicURL, 400},
		{"duplicate username", nil, "&username=bob", s.cfg.PublicURL, 400},
		{"empty password", func(v url.Values) { v.Set("secret", "") }, "", s.cfg.PublicURL, 400},
		{"full object path", func(v url.Values) { v.Set("username", "system/usr/alice") }, "", s.cfg.PublicURL, 400},
		{"oversized body", nil, "&padding=" + strings.Repeat("x", maxBodyBytes), s.cfg.PublicURL, 400},
		{"invalid encoding", nil, "&x=%zz", s.cfg.PublicURL, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			for k, v := range base {
				form[k] = append([]string(nil), v...)
			}
			if tc.change != nil {
				tc.change(form)
			}
			w := postLogin(s, cookie, form.Encode()+tc.extra, tc.origin)
			if w.Code != tc.want || calls.Load() != 0 || len(s.sessions) != 0 || strings.Contains(w.Body.String(), "synthetic-password") {
				t.Fatalf("unsafe form handling: status=%d calls=%d", w.Code, calls.Load())
			}
		})
	}
	// Invalid context/origin must be rejected without even reading a body.
	for _, tc := range []struct {
		cookie *http.Cookie
		origin string
	}{{nil, s.cfg.PublicURL}, {cookie, "https://attacker.example"}, {cookie, "null"}} {
		r := httptest.NewRequest("POST", s.cfg.PublicURL+"/login", nil)
		r.Body = forbiddenBody{t}
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		s.Handler().ServeHTTP(httptest.NewRecorder(), r)
	}
}

func TestLoginFailureAndAttemptBounds(t *testing.T) {
	s, _, calls := loginFixture(t, nil)
	cookie, form := startLogin(t, s)
	form.Set("secret", "wrong-password")
	for i := 0; i < maxLoginAttempts; i++ {
		w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL)
		if w.Code != 401 || !strings.Contains(w.Body.String(), "Identifiants OpenSVC refusés") || strings.Contains(w.Body.String(), "synthetic-password") || strings.Contains(w.Body.String(), "wrong-password") || len(s.sessions) != 0 {
			t.Fatal("unsafe login failure")
		}
	}
	if w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL); w.Code != 429 || calls.Load() != maxLoginAttempts {
		t.Fatal("attempt limit missing")
	}
	cookie, form = startLogin(t, s)
	s.inFlight = maxConcurrentLogins
	if w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL); w.Code != 429 || calls.Load() != maxLoginAttempts {
		t.Fatal("concurrent login limit missing")
	}
	s.inFlight = 0
	for len(s.sessions) < maxSessions {
		s.sessions[string(rune(len(s.sessions)))] = authenticatedSession{Authorization: s.requests[cookie.Value]}
	}
	// Give the fabricated entries a future JWT expiry so pruning retains them.
	for key, session := range s.sessions {
		session.Daemon.ExpiresAt = s.now().Add(time.Hour)
		session.ExpiresAt = s.now().Add(time.Hour)
		s.sessions[key] = session
	}
	if w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL); w.Code != 429 || calls.Load() != maxLoginAttempts {
		t.Fatal("session capacity missing")
	}
}

func TestLoginRejectsWrongClusterWithoutCreatingSession(t *testing.T) {
	s, token, _ := loginFixture(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"config": map[string]string{"id": "other-cluster"}}})
	})
	cookie, form := startLogin(t, s)
	w := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL)
	if w.Code != 502 || len(s.sessions) != 0 || strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), "other-cluster") {
		t.Fatal("wrong cluster created a session or exposed daemon data")
	}
}

func TestLoginConcurrentSubmissionAndContextExpiry(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s, _, calls := loginFixture(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"config": map[string]string{"id": "example-id"}}})
	})
	cookie, form := startLogin(t, s)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- postLogin(s, cookie, form.Encode(), s.cfg.PublicURL) }()
	<-entered
	duplicate := postLogin(s, cookie, form.Encode(), s.cfg.PublicURL)
	if duplicate.Code != 429 || calls.Load() != 2 {
		t.Error("duplicate submission contacted daemon")
	}
	s.mu.Lock()
	current := s.requests[cookie.Value]
	current.ExpiresAt = s.now().Add(-time.Second)
	s.requests[cookie.Value] = current
	s.mu.Unlock()
	close(release)
	if w := <-done; w.Code != 400 || len(s.sessions) != 0 || s.inFlight != 0 {
		t.Fatal("expired in-flight context created a session")
	}
}
