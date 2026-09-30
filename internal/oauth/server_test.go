package oauth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func prototype(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{PublicURL: "https://192.0.2.10", ClusterRef: "cluster-a", ClusterName: "Example cluster"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func request(s *Server, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, s.cfg.PublicURL+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func registerClient(t *testing.T, s *Server, name string) string {
	t.Helper()
	body, err := json.Marshal(registration{ClientName: name, RedirectURIs: []string{"http://127.0.0.1:4321/callback/test"}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}})
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/register", string(body))
	if w.Code != http.StatusCreated {
		t.Fatalf("register client: %d %s", w.Code, w.Body.String())
	}
	var data struct {
		ClientID   string   `json:"client_id"`
		AuthMethod string   `json:"token_endpoint_auth_method"`
		Grants     []string `json:"grant_types"`
		Secret     string   `json:"client_secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.ClientID == "" || data.AuthMethod != "none" || len(data.Grants) != 1 || data.Grants[0] != "authorization_code" || data.Secret != "" {
		t.Fatalf("unexpected registration: %+v", data)
	}
	return data.ClientID
}

func authorizationQuery(s *Server, id string) url.Values {
	return url.Values{
		"client_id": {id}, "redirect_uri": {"http://127.0.0.1:5432/callback/test"},
		"response_type": {"code"}, "scope": {Scope}, "resource": {s.cfg.PublicURL + "/mcp"},
		"state":                 {"client-csrf-state-must-not-appear-in-html"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))},
	}
}

func loginCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %v", cookies)
	}
	c := cookies[0]
	if c.Name != loginCookieName || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 600 {
		t.Fatalf("unsafe context cookie: %+v", c)
	}
	return c
}

func TestPrototypeDiscoveryRegistrationAuthorizationAndLogin(t *testing.T) {
	s := prototype(t)
	for _, bearer := range []string{"", "Bearer downstream-token-must-never-be-accepted"} {
		r := httptest.NewRequest("POST", s.cfg.PublicURL+"/mcp", nil)
		r.Header.Set("Authorization", bearer)
		// Forwarded/request host data must not influence canonical metadata URLs.
		r.Host = "attacker.example"
		r.Header.Set("X-Forwarded-Host", "attacker.example")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), s.cfg.PublicURL+"/.well-known/oauth-protected-resource/mcp") || strings.Contains(w.Body.String(), bearer) && bearer != "" {
			t.Fatalf("unexpected challenge: %d %v %s", w.Code, w.Header(), w.Body.String())
		}
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		w := request(s, "GET", path, "")
		var metadata struct {
			Resource string   `json:"resource"`
			Servers  []string `json:"authorization_servers"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || metadata.Resource != s.cfg.PublicURL+"/mcp" || len(metadata.Servers) != 1 || metadata.Servers[0] != s.cfg.PublicURL {
			t.Fatalf("bad protected metadata: %+v", metadata)
		}
	}
	meta := request(s, "GET", "/.well-known/oauth-authorization-server", "")
	var metadata map[string]any
	if err := json.Unmarshal(meta.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["issuer"] != s.cfg.PublicURL || metadata["registration_endpoint"] != s.cfg.PublicURL+"/register" || metadata["token_endpoint"] != s.cfg.PublicURL+"/token" || metadata["opensvc_login_prototype"] != true {
		t.Fatalf("bad authorization metadata: %+v", metadata)
	}
	id := registerClient(t, s, "Codex")
	q := authorizationQuery(s, id)
	w := request(s, "GET", "/authorize?"+q.Encode(), "")
	if w.Code != 303 || w.Header().Get("Location") != s.cfg.PublicURL+"/login" {
		t.Fatalf("authorize: %d %s", w.Code, w.Body.String())
	}
	cookie := loginCookie(t, w)
	context := s.requests[cookie.Value]
	if context.ClientID != id || context.ClusterRef != "cluster-a" || context.RedirectURI != q.Get("redirect_uri") || context.State != q.Get("state") || context.Resource != s.cfg.PublicURL+"/mcp" || context.CodeChallenge != q.Get("code_challenge") {
		t.Fatalf("request context lost binding: %+v", context)
	}
	page := request(s, "GET", "/login", "", cookie)
	if page.Code != 200 || page.Header().Get("Content-Type") != "text/html; charset=utf-8" || page.Header().Get("Cache-Control") != "no-store" || page.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(page.Header().Get("Content-Security-Policy"), "form-action 'none'") {
		t.Fatalf("login response: %d %v", page.Code, page.Header())
	}
	for _, expected := range []string{"Codex", "Example cluster", "Prototype", `<fieldset disabled>`, `type="password"`, `<button type="submit" disabled>`} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Errorf("login page is missing %q", expected)
		}
	}
	for _, hidden := range []string{cookie.Value, q.Get("state"), q.Get("code_challenge"), q.Get("redirect_uri")} {
		if strings.Contains(page.Body.String(), hidden) {
			t.Errorf("login page leaks request context")
		}
	}
}

func TestAuthorizeRejectsInvalidRequestsWithoutRedirect(t *testing.T) {
	s := prototype(t)
	id := registerClient(t, s, "Codex")
	for _, tc := range []struct{ key, value string }{
		{"client_id", "unknown"}, {"redirect_uri", "http://127.0.0.1:5432/other"},
		{"redirect_uri", "http://localhost:5432/callback/test"}, {"redirect_uri", "http://attacker.example/callback/test"},
		{"response_type", "token"}, {"response_mode", "fragment"}, {"state", ""},
		{"resource", "https://other.example/mcp"}, {"scope", "root"},
		{"code_challenge_method", "plain"}, {"code_challenge", "invalid"},
		{"code_challenge", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{42}, 31))},
		{"code_verifier", "must-never-store-verifier"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			q := authorizationQuery(s, id)
			q.Set(tc.key, tc.value)
			w := request(s, "GET", "/authorize?"+q.Encode(), "")
			if w.Code != 400 || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
				t.Fatalf("invalid request accepted or redirected: %d %v", w.Code, w.Header())
			}
		})
	}
	q := authorizationQuery(s, id)
	q.Add("redirect_uri", "http://attacker.example/callback")
	if w := request(s, "GET", "/authorize?"+q.Encode(), ""); w.Code != 400 {
		t.Fatalf("duplicate query accepted: %d", w.Code)
	}
	if w := request(s, "GET", "/authorize?malformed=%xx", ""); w.Code != 400 {
		t.Fatalf("malformed query accepted: %d", w.Code)
	}
	if len(s.requests) != 0 {
		t.Fatal("invalid requests allocated authorization state")
	}
}

func TestRegistrationRejectsUnsafeOrMalformedMetadata(t *testing.T) {
	s := prototype(t)
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"redirect_uris":null}`, `{"redirect_uris":["http://attacker.example/callback"]}`,
		`{"redirect_uris":["javascript:alert(1)"]}`, `{"redirect_uris":["http://user@127.0.0.1:4321/callback"]}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback#fragment"]}`,
		`{"redirect_uris":["http://127.0.0.1:65536/callback"]}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"token_endpoint_auth_method":"client_secret_basic"}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"scope":"root"}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"grant_types":["client_credentials"]}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"response_types":["token"]}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"client_name":"x","client_name":"y"}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"]} {}`,
		`{"redirect_uris":["http://127.0.0.1:4321/callback"],"client_name":"` + strings.Repeat("a", 129) + `"}`,
		`{"unknown":"` + strings.Repeat("a", maxBodyBytes) + `"}`,
	} {
		w := request(s, "POST", "/register", body)
		if w.Code != 400 {
			t.Fatalf("unsafe or malformed metadata accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if len(s.clients) != 0 {
		t.Fatal("invalid metadata allocated client state")
	}
}

func TestLoginRequiresLiveContextAndEscapesClientName(t *testing.T) {
	s := prototype(t)
	id := registerClient(t, s, `<script>alert("client")</script>`)
	q := authorizationQuery(s, id)
	w := request(s, "GET", "/authorize?"+q.Encode(), "")
	cookie := loginCookie(t, w)
	page := request(s, "GET", "/login", "", cookie)
	if page.Code != 200 || strings.Contains(page.Body.String(), "<script>") || !strings.Contains(page.Body.String(), "&lt;script&gt;") {
		t.Fatal("client metadata was not escaped")
	}
	for _, cookies := range [][]*http.Cookie{nil, {{Name: loginCookieName, Value: "unknown"}}} {
		if w := request(s, "GET", "/login", "", cookies...); w.Code != 400 {
			t.Fatal("login without a valid context accepted")
		}
	}
	if w := request(s, "GET", "/login?client_id=other", "", cookie); w.Code != 400 {
		t.Fatal("login query accepted")
	}
	now := s.now()
	s.now = func() time.Time { return now.Add(requestLifetime) }
	if w := request(s, "GET", "/login", "", cookie); w.Code != 400 {
		t.Fatal("expired context accepted")
	}
	if len(s.requests) != 0 {
		t.Fatal("expired context was not pruned")
	}
	s.now = func() time.Time { return now.Add(clientLifetime) }
	if w := request(s, "GET", "/authorize?"+q.Encode(), ""); w.Code != 400 {
		t.Fatal("expired client accepted")
	}
	if len(s.clients) != 0 {
		t.Fatal("expired client was not pruned")
	}
}

type forbiddenBody struct{ t *testing.T }

func (b forbiddenBody) Read([]byte) (int, error) {
	b.t.Error("prototype read a credential body")
	return 0, io.EOF
}
func (forbiddenBody) Close() error { return nil }

func TestPrototypeDoesNotReadCredentialsOrIssueTokens(t *testing.T) {
	s := prototype(t)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/login", 405}, {"/token", 503}} {
		r := httptest.NewRequest("POST", s.cfg.PublicURL+tc.path, nil)
		r.Body = forbiddenBody{t}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s returned %d", tc.path, w.Code)
		}
		if strings.Contains(w.Body.String(), "access_token") || strings.Contains(w.Body.String(), "refresh_token") {
			t.Fatal("prototype issued tokens")
		}
	}
}

func TestPrototypeBoundsStateAndConcurrentRegistration(t *testing.T) {
	s := prototype(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); registerClient(t, s, "Concurrent client") }()
	}
	wg.Wait()
	if len(s.clients) != 32 {
		t.Fatalf("concurrent registrations lost: %d", len(s.clients))
	}
	id := registerClient(t, s, "Codex")
	for len(s.clients) < maxClients {
		s.clients["capacity-"+string(rune(len(s.clients)))] = client{ExpiresAt: s.now().Add(clientLifetime)}
	}
	if w := request(s, "POST", "/register", `{"redirect_uris":["http://127.0.0.1/callback"]}`); w.Code != 429 {
		t.Fatal("registration capacity not enforced")
	}
	for len(s.requests) < maxRequests {
		s.requests["capacity-"+string(rune(len(s.requests)))] = authorization{ClientID: id, ExpiresAt: s.now().Add(requestLifetime)}
	}
	if w := request(s, "GET", "/authorize?"+authorizationQuery(s, id).Encode(), ""); w.Code != 429 {
		t.Fatal("request capacity not enforced")
	}
}

func TestCallbackMatching(t *testing.T) {
	for _, tc := range []struct {
		registered, requested string
		match                 bool
	}{
		{"http://127.0.0.1/callback", "http://127.0.0.1:5432/callback", true},
		{"http://[::1]/callback", "http://[::1]:5432/callback", true},
		{"http://localhost:4321/callback", "http://localhost:5432/callback", false},
		{"http://127.0.0.1/callback", "http://localhost:5432/callback", false},
		{"http://127.0.0.1/callback", "http://127.0.0.2:5432/callback", false},
		{"http://127.0.0.1/callback?x=y", "http://127.0.0.1:5432/callback?x=z", false},
		{"http://127.0.0.1/callback/test", "http://127.0.0.1:5432/callback/other", false},
	} {
		u, err := callbackURL(tc.requested)
		if err != nil {
			t.Fatal(err)
		}
		if got := matchesCallback([]string{tc.registered}, u); got != tc.match {
			t.Fatalf("callback %s vs %s: %t", tc.registered, tc.requested, got)
		}
	}
}

func TestRejectInvalidPublicConfiguration(t *testing.T) {
	for _, origin := range []string{"", "http://192.0.2.10", "https://192.0.2.10/mcp", "https://user:secret@192.0.2.10", "https://192.0.2.10?x=y", "https://192.0.2.10#fragment", "https://192.0.2.10:65536", "https:///"} {
		if _, err := New(Config{PublicURL: origin, ClusterRef: "cluster-a", ClusterName: "Cluster"}); err == nil {
			t.Fatalf("accepted public URL %q", origin)
		}
	}
	for _, cfg := range []Config{{PublicURL: "https://192.0.2.10", ClusterName: "Cluster"}, {PublicURL: "https://192.0.2.10", ClusterRef: "lab ai", ClusterName: "Cluster"}, {PublicURL: "https://192.0.2.10", ClusterRef: "cluster-a"}} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("accepted incomplete configuration: %+v", cfg)
		}
	}
}
