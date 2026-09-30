package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/daemonlogin"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

type codeFlow struct {
	cookie      *http.Cookie
	decision    url.Values
	exchange    url.Values
	state       string
	daemonToken string
}

func beginCodeFlow(t *testing.T, s *Server, daemonToken string) codeFlow {
	t.Helper()
	id := registerClient(t, s, "Example agent")
	q := authorizationQuery(s, id)
	digest := sha256.Sum256([]byte(testVerifier))
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	login := loginCookie(t, request(s, "GET", "/authorize?"+q.Encode(), ""))
	form := url.Values{"cluster_ref": {"cluster-a"}, "username": {"alice"}, "secret": {"synthetic-password"}, "csrf_token": {s.requests[login.Value].CSRFToken}}
	w := postLogin(s, login, form.Encode(), s.cfg.PublicURL)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login failed: %d", w.Code)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("missing consent cookie")
	}
	pending := s.sessions[cookie.Value]
	return codeFlow{
		cookie: cookie, decision: url.Values{"decision": {"allow"}, "csrf_token": {pending.CSRFToken}},
		exchange: url.Values{"grant_type": {"authorization_code"}, "client_id": {id}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {testVerifier}, "resource": {s.cfg.PublicURL + "/mcp"}},
		state:    q.Get("state"), daemonToken: daemonToken,
	}
}

func postForm(s *Server, path string, form url.Values, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, s.cfg.PublicURL+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func approveFlow(t *testing.T, s *Server, flow *codeFlow) string {
	t.Helper()
	w := postForm(s, "/consent", flow.decision, flow.cookie, s.cfg.PublicURL)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("consent failed: %d", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "http" || u.Host != "127.0.0.1:5432" || u.Path != "/callback/test" || q.Get("state") != flow.state || q.Get("iss") != s.cfg.PublicURL || !validOpaqueID(q.Get("code")) || q.Get("error") != "" {
		t.Fatal("callback lost its validated binding")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("unsafe callback headers")
	}
	flow.exchange.Set("code", q.Get("code"))
	return q.Get("code")
}

func exchangeFlow(t *testing.T, s *Server, flow codeFlow) string {
	t.Helper()
	w := postForm(s, "/token", flow.exchange, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("exchange failed: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		AccessToken string `json:"access_token"`
		Type        string `json:"token_type"`
		Scope       string `json:"scope"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !validOpaqueID(response.AccessToken) || response.AccessToken == flow.daemonToken || response.Type != "Bearer" || response.Scope != Scope || response.ExpiresIn < 1 || response.ExpiresIn > 600 || strings.Contains(w.Body.String(), "refresh_token") || strings.Contains(w.Body.String(), flow.daemonToken) {
		t.Fatal("unsafe MCP token response")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
		t.Fatal("unsafe token response headers")
	}
	return response.AccessToken
}

func TestConsentAndPKCEIssueDistinctClusterBoundToken(t *testing.T) {
	s, daemonToken, calls := loginFixture(t, nil)
	var boundUser, boundCluster string
	s.handlerFactory = func(cluster clusterconfig.Cluster, session daemonlogin.Session) (http.Handler, error) {
		boundUser, boundCluster = session.Username, cluster.Ref
		if session.AccessToken != daemonToken || session.ClusterRef != cluster.Ref || session.ClusterID != cluster.ExpectedClusterID {
			t.Fatal("lost daemon session binding")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }), nil
	}
	flow := beginCodeFlow(t, s, daemonToken)
	page := request(s, "GET", "/login", "", flow.cookie)
	if !strings.Contains(page.Body.String(), "Autoriser et revenir") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "form-action 'self' http://127.0.0.1:5432;") || len(s.codes) != 0 || len(s.tokens) != 0 {
		t.Fatal("login bypassed consent")
	}
	for _, secret := range []string{daemonToken, "synthetic-password", testVerifier, flow.state} {
		if strings.Contains(page.Body.String(), secret) {
			t.Fatal("consent exposed credentials or OAuth state")
		}
	}
	code := approveFlow(t, s, &flow)
	if len(s.sessions) != 0 || len(s.codes) != 1 {
		t.Fatal("consent was not consumed")
	}
	if replay := postForm(s, "/consent", flow.decision, flow.cookie, s.cfg.PublicURL); replay.Code != 400 {
		t.Fatal("consent was replayed")
	}
	token := exchangeFlow(t, s, flow)
	if len(s.codes) != 0 || len(s.tokens) != 1 || boundUser != "alice" || boundCluster != "cluster-a" || calls.Load() != 2 {
		t.Fatal("token issuance lost isolation or contacted the daemon again")
	}
	grant := s.tokens[sha256.Sum256([]byte(token))]
	if grant.Session.Authorization.ClientID != flow.exchange.Get("client_id") || grant.Session.Authorization.Resource != s.cfg.PublicURL+"/mcp" || grant.Session.Daemon.AccessToken != daemonToken || !grant.ExpiresAt.Equal(grant.Session.Daemon.ExpiresAt) {
		t.Fatal("incorrect access grant")
	}
	for _, bearer := range []string{code, flow.cookie.Value, daemonToken, strings.Repeat("A", 43)} {
		r := httptest.NewRequest(http.MethodPost, s.cfg.PublicURL+"/mcp", nil)
		r.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("non-MCP credential authorized tools")
		}
	}
	r := httptest.NewRequest(http.MethodPost, s.cfg.PublicURL+"/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatal("valid MCP token refused")
	}
	if w := postForm(s, "/token", flow.exchange, nil, ""); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_grant") || len(s.tokens) != 1 {
		t.Fatal("code was reused")
	}
}

func TestConsentRejectsUntrustedDecisionsAndSupportsDenial(t *testing.T) {
	s, token, calls := loginFixture(t, nil)
	flow := beginCodeFlow(t, s, token)
	for _, tc := range []struct {
		name, origin, csrf, decision string
		cookie                       *http.Cookie
		status                       int
	}{
		{"foreign origin", "https://attacker.example", flow.decision.Get("csrf_token"), "allow", flow.cookie, 403},
		{"null origin", "null", flow.decision.Get("csrf_token"), "allow", flow.cookie, 403},
		{"missing origin", "", flow.decision.Get("csrf_token"), "allow", flow.cookie, 403},
		{"missing cookie", s.cfg.PublicURL, flow.decision.Get("csrf_token"), "allow", nil, 400},
		{"wrong csrf", s.cfg.PublicURL, "wrong", "allow", flow.cookie, 403},
		{"unknown decision", s.cfg.PublicURL, flow.decision.Get("csrf_token"), "other", flow.cookie, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"csrf_token": {tc.csrf}, "decision": {tc.decision}}
			w := postForm(s, "/consent", form, tc.cookie, tc.origin)
			if w.Code != tc.status || w.Header().Get("Location") != "" || len(s.codes) != 0 || len(s.tokens) != 0 || calls.Load() != 2 {
				t.Fatal("invalid consent redirected or created credentials")
			}
		})
	}
	bad := cloneForm(flow.decision)
	bad.Add("decision", "deny")
	if w := postForm(s, "/consent", bad, flow.cookie, s.cfg.PublicURL); w.Code != 400 {
		t.Fatal("duplicate consent accepted")
	}
	flow.decision.Set("decision", "deny")
	w := postForm(s, "/consent", flow.decision, flow.cookie, s.cfg.PublicURL)
	u, _ := url.Parse(w.Header().Get("Location"))
	if w.Code != 303 || u.Query().Get("error") != "access_denied" || u.Query().Get("state") != flow.state || u.Query().Get("iss") != s.cfg.PublicURL || u.Query().Get("code") != "" || len(s.sessions) != 0 || len(s.codes) != 0 || len(s.tokens) != 0 {
		t.Fatal("denial lost its binding or created credentials")
	}
}

func TestTokenRejectsMismatchedBindingsAndMalformedRequests(t *testing.T) {
	s, token, _ := loginFixture(t, nil)
	flow := beginCodeFlow(t, s, token)
	approveFlow(t, s, &flow)
	otherClient := registerClient(t, s, "Other agent")
	for _, tc := range []struct{ field, value, errorCode string }{
		{"client_id", otherClient, "invalid_grant"}, {"redirect_uri", "http://127.0.0.1:9876/callback/test", "invalid_grant"},
		{"resource", "https://other.example/mcp", "invalid_target"}, {"scope", "root", "invalid_scope"},
		{"code_verifier", strings.Repeat("a", 43), "invalid_grant"}, {"code_verifier", strings.Repeat("a", 42), "invalid_request"},
		{"code_verifier", strings.Repeat("a", 129), "invalid_request"}, {"code_verifier", strings.Repeat("a", 42) + "!", "invalid_request"},
		{"code", strings.Repeat("A", 43), "invalid_grant"}, {"client_secret", "forbidden-secret", "invalid_client"},
		{"grant_type", "refresh_token", "unsupported_grant_type"},
	} {
		t.Run(tc.field+tc.value, func(t *testing.T) {
			form := cloneForm(flow.exchange)
			form.Set(tc.field, tc.value)
			w := postForm(s, "/token", form, nil, "")
			if w.Code != 400 || !strings.Contains(w.Body.String(), tc.errorCode) || len(s.tokens) != 0 || len(s.codes) != 1 || strings.Contains(w.Body.String(), testVerifier) || strings.Contains(w.Body.String(), flow.exchange.Get("code")) {
				t.Fatalf("bad request accepted or leaked: %d", w.Code)
			}
		})
	}
	duplicate := cloneForm(flow.exchange)
	duplicate.Add("code_verifier", testVerifier)
	if w := postForm(s, "/token", duplicate, nil, ""); w.Code != 400 {
		t.Fatal("duplicate token parameter accepted")
	}
	for _, tc := range []struct{ path, contentType, body, auth string }{
		{"/token?code=must-not-use-query", "application/x-www-form-urlencoded", flow.exchange.Encode(), ""},
		{"/token", "application/json", `{"code":"fake"}`, ""},
		{"/token", "application/x-www-form-urlencoded", "malformed=%xx", ""},
		{"/token", "application/x-www-form-urlencoded", strings.Repeat("x", maxBodyBytes+1), ""},
		{"/token", "application/x-www-form-urlencoded", flow.exchange.Encode(), "Basic ZmFrZTo="},
	} {
		r := httptest.NewRequest("POST", s.cfg.PublicURL+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		if tc.auth != "" {
			r.Header.Set("Authorization", tc.auth)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 || len(s.tokens) != 0 {
			t.Fatal("malformed exchange accepted")
		}
	}
	exchangeFlow(t, s, flow) // Failed proofs do not consume a legitimate grant.
}

func TestOAuthExpiryRestartAndMCPRequestProtection(t *testing.T) {
	s, token, _ := loginFixture(t, nil)
	s.handlerFactory = func(clusterconfig.Cluster, daemonlogin.Session) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), nil
	}
	flow := beginCodeFlow(t, s, token)
	approveFlow(t, s, &flow)
	access := exchangeFlow(t, s, flow)
	for _, tc := range []struct {
		query, origin string
		duplicate     bool
		want          int
	}{
		{"", "https://attacker.example", false, 403}, {"", "null", false, 403}, {"?access_token=" + access, "", false, 400}, {"", "", true, 401},
	} {
		r := httptest.NewRequest("POST", s.cfg.PublicURL+"/mcp"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+access)
		if tc.duplicate {
			r.Header.Add("Authorization", "Bearer "+access)
		}
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("MCP protection status %d want %d", w.Code, tc.want)
		}
	}
	restarted, err := New(s.cfg, s.handlerFactory)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", s.cfg.PublicURL+"/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+access)
	w := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("token survived restart")
	}
	expiry := s.tokens[sha256.Sum256([]byte(access))].ExpiresAt
	s.now = func() time.Time { return expiry }
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 || len(s.tokens) != 0 {
		t.Fatal("expired token accepted")
	}
	s.now = time.Now
	flow = beginCodeFlow(t, s, token)
	// The fixture's real JWT is still valid; advance only the OAuth clock.
	approveFlow(t, s, &flow)
	hash := sha256.Sum256([]byte(flow.exchange.Get("code")))
	expired := s.codes[hash].ExpiresAt
	s.now = func() time.Time { return expired }
	if w := postForm(s, "/token", flow.exchange, nil, ""); w.Code != 400 || len(s.tokens) != 0 {
		t.Fatal("expired code accepted")
	}
}

func TestConcurrentCodeRedemptionIssuesOnlyOneToken(t *testing.T) {
	s, token, _ := loginFixture(t, nil)
	flow := beginCodeFlow(t, s, token)
	approveFlow(t, s, &flow)
	const parallel = 8
	results := make(chan int, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- postForm(s, "/token", flow.exchange, nil, "").Code }()
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		} else if status != 400 {
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if success != 1 || len(s.tokens) != 1 || len(s.codes) != 0 {
		t.Fatal("code redeemed more than once")
	}
}

func TestOAuthStateCapacityAndConsentExpiry(t *testing.T) {
	s, token, _ := loginFixture(t, nil)
	flow := beginCodeFlow(t, s, token)
	pending := s.sessions[flow.cookie.Value]
	for i := 0; i < maxCodes; i++ {
		s.codes[sha256.Sum256([]byte(fmt.Sprint(i)))] = authorizationCode{Session: pending, ExpiresAt: s.now().Add(codeLifetime)}
	}
	if w := postForm(s, "/consent", flow.decision, flow.cookie, s.cfg.PublicURL); w.Code != 429 || len(s.sessions) != 1 {
		t.Fatal("code capacity not enforced")
	}
	clear(s.codes)
	approveFlow(t, s, &flow)
	for i := 0; i < maxAccessTokens; i++ {
		s.tokens[sha256.Sum256([]byte(fmt.Sprint(i)))] = accessGrant{Session: pending, ExpiresAt: pending.Daemon.ExpiresAt}
	}
	if w := postForm(s, "/token", flow.exchange, nil, ""); w.Code != 429 || len(s.codes) != 1 {
		t.Fatal("token capacity not enforced")
	}
	clear(s.tokens)
	exchangeFlow(t, s, flow)
	flow = beginCodeFlow(t, s, token)
	expiry := s.sessions[flow.cookie.Value].ExpiresAt
	s.now = func() time.Time { return expiry }
	if w := postForm(s, "/consent", flow.decision, flow.cookie, s.cfg.PublicURL); w.Code != 400 || w.Header().Get("Location") != "" {
		t.Fatal("expired consent redirected")
	}
}

func cloneForm(form url.Values) url.Values {
	clone := make(url.Values, len(form))
	for name, values := range form {
		clone[name] = append([]string(nil), values...)
	}
	return clone
}
