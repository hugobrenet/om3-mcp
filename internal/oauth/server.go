package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

const (
	clientLifetime  = 24 * time.Hour
	requestLifetime = 10 * time.Minute
	maxClients      = 256
	maxRequests     = 256
	maxBodyBytes    = 16 << 10
	maxQueryBytes   = 8 << 10
	loginCookieName = "__Host-opensvc-mcp-login"
)

type client struct {
	ID           string
	Name         string
	RedirectURIs []string
	ExpiresAt    time.Time
}

// authorization preserves the validated client request server-side. No user
// credentials, MCP Bearer token or PKCE verifier enter this context.
type authorization struct {
	ClientID      string
	ClientName    string
	RedirectURI   string
	Resource      string
	Scope         string
	State         string
	CodeChallenge string
	ExpiresAt     time.Time
}

// Server owns bounded, expiring prototype state. It has no daemon client and
// no token issuer. A process restart intentionally invalidates this state.
type Server struct {
	cfg      Config
	now      func() time.Time
	mu       sync.Mutex
	clients  map[string]client
	requests map[string]authorization
}

func New(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, now: time.Now, clients: make(map[string]client), requests: make(map[string]authorization)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	resource := &oauthex.ProtectedResourceMetadata{
		Resource: s.cfg.PublicURL + "/mcp", AuthorizationServers: []string{s.cfg.PublicURL}, ScopesSupported: []string{Scope},
	}
	metadata := mcpauth.ProtectedResourceMetadataHandler(resource)
	mux.Handle("/.well-known/oauth-protected-resource/mcp", metadata)
	mux.Handle("/.well-known/oauth-protected-resource", metadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("/mcp", s.challenge)
	mux.HandleFunc("/register", s.register)
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/login", s.login)
	// Discovery requires a token_endpoint. This explicit prototype endpoint
	// never reads credentials and returns an OAuth temporarily_unavailable error.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if !method(w, r, http.MethodPost) {
			return
		}
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "This prototype stops at the login form; token issuance is not available.")
	})
	return mux
}

func (s *Server) metadata(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !method(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.PublicURL,
		"authorization_endpoint":                s.cfg.PublicURL + "/authorize",
		"registration_endpoint":                 s.cfg.PublicURL + "/register",
		"token_endpoint":                        s.cfg.PublicURL + "/token",
		"scopes_supported":                      []string{Scope},
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"client_id_metadata_document_supported": false,
		"opensvc_login_prototype":               true,
	})
}

func (s *Server) challenge(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.cfg.PublicURL+`/.well-known/oauth-protected-resource/mcp", scope="`+Scope+`"`)
	// No token is valid until the next increment adds an issuer and verifier.
	oauthError(w, http.StatusUnauthorized, "invalid_token", "OAuth authorization is required; this prototype stops at the login form.")
}

type registration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		oauthError(w, http.StatusUnsupportedMediaType, "invalid_client_metadata", "Registration requires application/json.")
		return
	}
	var data registration
	if err := decodeRegistration(w, r, &data); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "Registration must be a bounded JSON object without duplicate fields or trailing data.")
		return
	}
	if len(data.RedirectURIs) == 0 || len(data.RedirectURIs) > 8 {
		oauthError(w, 400, "invalid_redirect_uri", "Provide between one and eight loopback HTTP callback URLs.")
		return
	}
	for _, uri := range data.RedirectURIs {
		if _, err := callbackURL(uri); err != nil {
			oauthError(w, 400, "invalid_redirect_uri", "Only loopback HTTP callbacks without credentials or fragments are accepted by this prototype.")
			return
		}
	}
	if data.ClientName == "" {
		data.ClientName = "Client MCP"
	}
	if !validDisplayName(data.ClientName) || data.TokenEndpointAuthMethod != "" && data.TokenEndpointAuthMethod != "none" || !validScope(data.Scope) {
		oauthError(w, 400, "invalid_client_metadata", "Provide a bounded client name, public client authentication and the mcp:access scope.")
		return
	}
	if len(data.GrantTypes) > 4 || len(data.ResponseTypes) > 1 || len(data.ResponseTypes) == 1 && data.ResponseTypes[0] != "code" {
		oauthError(w, 400, "invalid_client_metadata", "Only the authorization code flow is available in this prototype.")
		return
	}
	for _, grant := range data.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata", "Only the authorization code flow is available in this prototype.")
			return
		}
	}
	if len(data.GrantTypes) > 0 && !slices.Contains(data.GrantTypes, "authorization_code") {
		oauthError(w, 400, "invalid_client_metadata", "The client must support authorization_code.")
		return
	}
	id, err := randomID()
	if err != nil {
		oauthError(w, 500, "server_error", "Unable to register the client.")
		return
	}
	s.mu.Lock()
	now := s.now()
	s.prune(now)
	if len(s.clients) >= maxClients {
		s.mu.Unlock()
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "The client registration capacity has been reached.")
		return
	}
	s.clients[id] = client{ID: id, Name: data.ClientName, RedirectURIs: data.RedirectURIs, ExpiresAt: now.Add(clientLifetime)}
	s.mu.Unlock()
	// RFC 7591 permits provisioning accepted metadata with suitable defaults.
	// Respond with the public auth method and authorization_code only, even if
	// a client requested refresh_token; no refresh capability is advertised.
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": id, "client_id_issued_at": now.Unix(),
		"client_name": data.ClientName, "redirect_uris": data.RedirectURIs,
		"token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code"},
		"response_types": []string{"code"}, "scope": Scope,
	})
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	if len(r.URL.RawQuery) > maxQueryBytes {
		oauthError(w, 400, "invalid_request", "Authorization request is too large.")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		oauthError(w, 400, "invalid_request", "Invalid authorization query.")
		return
	}
	for _, values := range q {
		if len(values) != 1 || len(values[0]) > 2048 {
			oauthError(w, 400, "invalid_request", "Authorization parameters must have a single bounded value.")
			return
		}
	}
	if q.Get("response_type") != "code" || q.Get("state") == "" || q.Get("code_verifier") != "" || q.Get("response_mode") != "" && q.Get("response_mode") != "query" {
		oauthError(w, 400, "invalid_request", "Authorization requires response_type=code, state and query response mode.")
		return
	}
	if q.Get("resource") != s.cfg.PublicURL+"/mcp" {
		oauthError(w, 400, "invalid_target", "The requested resource must match this MCP.")
		return
	}
	if !validScope(q.Get("scope")) {
		oauthError(w, 400, "invalid_scope", "Only mcp:access is supported.")
		return
	}
	challenge := q.Get("code_challenge")
	digest, err := base64.RawURLEncoding.DecodeString(challenge)
	if q.Get("code_challenge_method") != "S256" || err != nil || len(digest) != 32 || base64.RawURLEncoding.EncodeToString(digest) != challenge {
		oauthError(w, 400, "invalid_request", "PKCE requires a valid S256 code challenge.")
		return
	}
	callback, err := callbackURL(q.Get("redirect_uri"))
	if err != nil {
		oauthError(w, 400, "invalid_request", "Invalid callback URL.")
		return
	}
	id, err := randomID()
	if err != nil {
		oauthError(w, 500, "server_error", "Unable to create an authorization request.")
		return
	}
	s.mu.Lock()
	now := s.now()
	s.prune(now)
	c, found := s.clients[q.Get("client_id")]
	if !found || !matchesCallback(c.RedirectURIs, callback) {
		s.mu.Unlock()
		// Never redirect errors to an unvalidated callback, or echo query values.
		oauthError(w, 400, "invalid_request", "Unknown client or unregistered callback.")
		return
	}
	if len(s.requests) >= maxRequests {
		s.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "The authorization request capacity has been reached.")
		return
	}
	s.requests[id] = authorization{
		ClientID: c.ID, ClientName: c.Name,
		RedirectURI: q.Get("redirect_uri"), Resource: q.Get("resource"), Scope: Scope,
		State: q.Get("state"), CodeChallenge: challenge, ExpiresAt: now.Add(requestLifetime),
	}
	s.mu.Unlock()
	noStore(w)
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: id, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(requestLifetime.Seconds())})
	// The opaque context stays in a cookie, not in the login URL.
	http.Redirect(w, r, s.cfg.PublicURL+"/login", http.StatusSeeOther)
}

func (s *Server) prune(now time.Time) {
	for id, c := range s.clients {
		if !now.Before(c.ExpiresAt) {
			delete(s.clients, id)
		}
	}
	for id, request := range s.requests {
		_, exists := s.clients[request.ClientID]
		if !now.Before(request.ExpiresAt) || !exists {
			delete(s.requests, id)
		}
	}
}

func callbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "http" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") {
		return nil, errors.New("invalid loopback callback")
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("callback is not loopback")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid callback port")
		}
	}
	return u, nil
}

func matchesCallback(registered []string, requested *url.URL) bool {
	for _, raw := range registered {
		u, err := callbackURL(raw)
		if err != nil {
			continue
		}
		// RFC 8252: only the port may vary for loopback IP redirects. For the
		// localhost hostname the registered URI must match exactly.
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
			u.Host = u.Hostname()
			copy := *requested
			copy.Host = requested.Hostname()
			if u.String() == copy.String() {
				return true
			}
		} else if raw == requested.String() {
			return true
		}
	}
	return false
}

func validScope(scope string) bool {
	return scope == "" || scope == Scope
}

func randomID() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "Unsupported HTTP method.")
	return false
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func decodeRegistration(w http.ResponseWriter, r *http.Request, target *registration) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("expected field name")
		}
		if _, exists := fields[key]; exists {
			return errors.New("duplicate field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}
