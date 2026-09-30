package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"go.yaml.in/yaml/v2"
)

// Exercise the compiled HTTPS entrypoint with real SDK calls and two synthetic
// cluster authorities. This checks credential/cluster isolation, not every tool.
func TestHTTPSOAuthToolsIsolateUsersAndClusters(t *testing.T) {
	clusters := make(map[string]any)
	for _, ref := range []string{"cluster-a", "cluster-b"} {
		users := make(map[string]string)
		d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/auth/token" {
				user, password, ok := r.BasicAuth()
				if !ok || password != "synthetic-password" || users[user] == "" {
					w.WriteHeader(401)
					return
				}
				json.NewEncoder(w).Encode(map[string]string{"access_token": users[user]})
				return
			}
			user := ""
			for name, token := range users {
				if r.Header.Get("Authorization") == "Bearer "+token {
					user = name
				}
			}
			if user == "" {
				w.WriteHeader(401)
				return
			}
			switch r.URL.Path {
			case "/api/cluster/status":
				json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"config": map[string]string{"id": ref}}})
			case "/api/object/path":
				json.NewEncoder(w).Encode([]string{user + "/svc/" + ref})
			case "/api/object":
				w.WriteHeader(403)
				fmt.Fprint(w, `{"title":"Forbidden","detail":"namespace access denied"}`)
			default:
				t.Errorf("unexpected daemon path %s", r.URL.Path)
				w.WriteHeader(404)
			}
		}))
		for _, user := range []string{"alice", "bob"} {
			token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": user, "iss": "node-a", "exp": time.Now().Add(10 * time.Minute).Unix(), "token_use": "access", "grant": []string{"guest:" + user}}).SignedString(d.CAKey)
			if err != nil {
				t.Fatal(err)
			}
			users[user] = token
		}
		clusters[ref] = map[string]any{"name": "Example " + ref, "expected_cluster_id": ref, "endpoints": []string{d.Server.URL}, "tls": map[string]string{"ca_file": d.CAFile}, "request_timeout": "2s"}
	}
	data, err := yaml.Marshal(map[string]any{"version": 1, "clusters": clusters})
	if err != nil {
		t.Fatal(err)
	}
	clusterFile := filepath.Join(t.TempDir(), "clusters.yaml")
	if err := os.WriteFile(clusterFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	cert, key, roots := writeListenerCertificate(t)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	c := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	origin := startHTTPSBinary(t, c, cert, key, clusterFile)
	// Reuse browser state to check that a second login never replaces a token's
	// server-held daemon identity, including identical usernames across clusters.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Jar = jar
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme+"://"+r.URL.Host != origin {
			return http.ErrUseLastResponse
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions := make([]*mcp.ClientSession, 0, 3)
	expected := []string{"alice/svc/cluster-a", "bob/svc/cluster-a", "alice/svc/cluster-b"}
	for i, pair := range [][2]string{{"cluster-a", "alice"}, {"cluster-a", "bob"}, {"cluster-b", "alice"}} {
		token := authenticateHTTPSAgent(t, ctx, c, origin, pair[0], pair[1])
		client := &http.Client{Transport: mcpBearerTransport{base: transport, token: token}, Timeout: 3 * time.Second}
		session, err := mcp.NewClient(&mcp.Implementation{Name: "Example agent", Version: "test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: origin + "/mcp", HTTPClient: client, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatalf("connect authenticated agent: %v", err)
		}
		t.Cleanup(func() {
			if err := session.Close(); err != nil {
				t.Error(err)
			}
		})
		sessions = append(sessions, session)
		list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil || len(list.Tools) == 0 {
			t.Fatalf("list authenticated tools: %v", err)
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_cluster_objects", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("call authorized tool: %v", err)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(encoded), expected[i]) || !strings.Contains(string(encoded), `"source":"opensvc_daemon"`) {
			t.Fatal("tool result escaped its cluster/user binding")
		}
		if strings.Contains(string(encoded), token) {
			t.Fatal("MCP token leaked into tool output")
		}
		denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_object_status", Arguments: map[string]any{"path": "private/svc/example"}})
		if err != nil || !denied.IsError {
			t.Fatalf("daemon permission failure was bypassed: %v", err)
		}
	}
	for i, session := range sessions {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_cluster_objects", Arguments: map[string]any{}})
		if err != nil || result.IsError {
			t.Fatalf("prior token invalidated by a new login: %v", err)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(encoded), expected[i]) {
			t.Fatal("new browser login changed an existing token's identity")
		}
	}
}

func authenticateHTTPSAgent(t *testing.T, ctx context.Context, c *http.Client, origin, cluster, user string) string {
	t.Helper()
	registration, err := oauthex.RegisterClient(ctx, origin+"/register", &oauthex.ClientRegistrationMetadata{ClientName: "Example agent", RedirectURIs: []string{"http://127.0.0.1/callback/client-a"}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}}, c)
	if err != nil {
		t.Fatal(err)
	}
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {registration.ClientID}, "redirect_uri": {"http://127.0.0.1:5432/callback/client-a"}, "response_type": {"code"}, "resource": {origin + "/mcp"}, "scope": {"mcp:access"}, "state": {"example-client-state"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}}
	response, err := c.Get(origin + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	page := readTestPage(t, response)
	csrf := pageCSRF(t, page)
	response = postHTTPSForm(t, c, origin, "/login", url.Values{"cluster_ref": {cluster}, "username": {user}, "secret": {"synthetic-password"}, "csrf_token": {csrf}}, true)
	page = readTestPage(t, response)
	if !strings.Contains(page, "Autoriser et revenir") {
		t.Fatal("login did not reach consent")
	}
	response = postHTTPSForm(t, c, origin, "/consent", url.Values{"decision": {"allow"}, "csrf_token": {pageCSRF(t, page)}}, true)
	_ = response.Body.Close()
	callback, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 303 || callback.Query().Get("state") != q.Get("state") || callback.Query().Get("iss") != origin || callback.Query().Get("code") == "" {
		t.Fatal("bad OAuth callback")
	}
	response = postHTTPSForm(t, c, origin, "/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {registration.ClientID}, "redirect_uri": {q.Get("redirect_uri")}, "code_verifier": {verifier}, "code": {callback.Query().Get("code")}, "resource": {origin + "/mcp"}}, false)
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	err = json.NewDecoder(response.Body).Decode(&issued)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || len(issued.AccessToken) != 43 {
		t.Fatalf("bad token exchange: status=%d err=%v", response.StatusCode, err)
	}
	return issued.AccessToken
}

func readTestPage(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	page, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("browser page: status=%d err=%v", response.StatusCode, err)
	}
	return string(page)
}

var csrfPattern = regexp.MustCompile(`name="csrf_token" type="hidden" value="([^"]+)"`)

func pageCSRF(t *testing.T, page string) string {
	t.Helper()
	match := csrfPattern.FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("missing form token")
	}
	return match[1]
}

func postHTTPSForm(t *testing.T, c *http.Client, origin, path string, form url.Values, browser bool) *http.Response {
	t.Helper()
	request, err := http.NewRequest("POST", origin+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if browser {
		request.Header.Set("Origin", origin)
	}
	response, err := c.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type mcpBearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}
