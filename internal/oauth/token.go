package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// consent consumes the authenticated browser session only after an explicit,
// same-origin decision. No browser cookie is a credential for /mcp.
func (s *Server) consent(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !method(w, r, http.MethodPost) {
		return
	}
	if r.Header.Get("Origin") != s.cfg.PublicURL {
		oauthError(w, http.StatusForbidden, "invalid_request", "Consent must originate from this MCP.")
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "This authorization request is missing or expired.")
		return
	}
	s.mu.Lock()
	s.prune(s.now())
	session, live := s.sessions[cookie.Value]
	s.mu.Unlock()
	if !live {
		clearCookie(w, sessionCookieName)
		oauthError(w, http.StatusBadRequest, "invalid_request", "This authorization request is missing or expired.")
		return
	}
	form, err := readForm(w, r)
	if err != nil || len(form) != 2 || len(form["csrf_token"]) != 1 || len(form["decision"]) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Provide a bounded consent decision and form token.")
		return
	}
	if subtle.ConstantTimeCompare([]byte(form.Get("csrf_token")), []byte(session.CSRFToken)) != 1 {
		oauthError(w, http.StatusForbidden, "invalid_request", "Invalid consent form token.")
		return
	}
	decision := form.Get("decision")
	if decision != "allow" && decision != "deny" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Invalid consent decision.")
		return
	}
	var code string
	if decision == "allow" {
		code, err = randomID()
		if err != nil {
			oauthError(w, http.StatusInternalServerError, "server_error", "Unable to complete authorization.")
			return
		}
	}
	s.mu.Lock()
	now := s.now()
	s.prune(now)
	current, live := s.sessions[cookie.Value]
	if !live || current.CSRFToken != session.CSRFToken {
		s.mu.Unlock()
		oauthError(w, http.StatusBadRequest, "invalid_request", "This authorization request is missing or already consumed.")
		return
	}
	if decision == "allow" && len(s.codes) >= maxCodes {
		s.mu.Unlock()
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Authorization capacity has been reached; retry later.")
		return
	}
	delete(s.sessions, cookie.Value)
	if decision == "allow" {
		current.CSRFToken = ""
		expiresAt := now.Add(codeLifetime)
		if current.Daemon.ExpiresAt.Before(expiresAt) {
			expiresAt = current.Daemon.ExpiresAt
		}
		s.codes[sha256.Sum256([]byte(code))] = authorizationCode{Session: current, ExpiresAt: expiresAt}
	}
	s.mu.Unlock()
	clearCookie(w, sessionCookieName)
	callback, _ := url.Parse(current.Authorization.RedirectURI) // Validated on /authorize.
	q := callback.Query()
	q.Set("state", current.Authorization.State)
	q.Set("iss", s.cfg.PublicURL)
	if decision == "allow" {
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	callback.RawQuery = q.Encode()
	w.Header().Set("Location", callback.String())
	w.WriteHeader(http.StatusSeeOther)
}

// token accepts public clients using Authorization Code and PKCE S256. Codes
// and access tokens are indexed by hash; daemon credentials never leave memory.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	if len(r.Header.Values("Authorization")) != 0 {
		oauthError(w, http.StatusBadRequest, "invalid_client", "Only public clients with token_endpoint_auth_method=none are supported.")
		return
	}
	form, err := readForm(w, r)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Token requests require a bounded URL-encoded form without duplicate parameters.")
		return
	}
	if _, exists := form["client_secret"]; exists {
		oauthError(w, http.StatusBadRequest, "invalid_client", "Client secrets are not supported.")
		return
	}
	if form.Get("grant_type") == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Provide an authorization code grant.")
		return
	}
	if form.Get("grant_type") != "authorization_code" {
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "Only authorization_code is supported.")
		return
	}
	if form.Get("resource") != s.cfg.PublicURL+"/mcp" {
		oauthError(w, http.StatusBadRequest, "invalid_target", "The resource must match this MCP.")
		return
	}
	if !validScope(form.Get("scope")) {
		oauthError(w, http.StatusBadRequest, "invalid_scope", "Only mcp:access is supported.")
		return
	}
	if form.Get("client_id") == "" || form.Get("redirect_uri") == "" || !validOpaqueID(form.Get("code")) || !validVerifier(form.Get("code_verifier")) {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Provide the client, callback, code and PKCE verifier.")
		return
	}
	accessToken, err := randomID()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "Unable to issue an access token.")
		return
	}
	hash := sha256.Sum256([]byte(form.Get("code")))
	digest := sha256.Sum256([]byte(form.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	s.mu.Lock()
	now := s.now()
	s.prune(now)
	code, found := s.codes[hash]
	a := code.Session.Authorization
	if !found || a.ClientID != form.Get("client_id") || a.RedirectURI != form.Get("redirect_uri") || a.Resource != form.Get("resource") || subtle.ConstantTimeCompare([]byte(a.CodeChallenge), []byte(challenge)) != 1 {
		s.mu.Unlock()
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The authorization grant is invalid, expired or already consumed.")
		return
	}
	if len(s.tokens) >= maxAccessTokens {
		s.mu.Unlock()
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Access token capacity has been reached; retry later.")
		return
	}
	var handler http.Handler
	if s.handlerFactory != nil {
		cluster, _ := s.cfg.Clusters.Lookup(code.Session.Daemon.ClusterRef)
		handler, err = s.handlerFactory(cluster, code.Session.Daemon)
		if err != nil || handler == nil {
			s.mu.Unlock()
			oauthError(w, http.StatusInternalServerError, "server_error", "Unable to prepare MCP access.")
			return
		}
	}
	// Token validity cannot outlive the native daemon credential.
	expiresAt := code.Session.Daemon.ExpiresAt
	expiresIn := int(expiresAt.Sub(s.now()) / time.Second)
	if expiresIn < 1 || !s.now().Before(code.ExpiresAt) {
		delete(s.codes, hash)
		s.mu.Unlock()
		oauthError(w, http.StatusBadRequest, "invalid_grant", "The authorization grant has expired.")
		return
	}
	delete(s.codes, hash)
	s.tokens[sha256.Sum256([]byte(accessToken))] = accessGrant{Session: code.Session, ExpiresAt: expiresAt, Handler: handler}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": expiresIn, "scope": Scope})
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(r.Header.Values("Authorization")) != 1 || len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || !validOpaqueID(fields[1]) {
		s.challenge(w, r)
		return
	}
	s.mu.Lock()
	s.prune(s.now())
	grant, exists := s.tokens[sha256.Sum256([]byte(fields[1]))]
	s.mu.Unlock()
	if !exists {
		s.challenge(w, r)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.cfg.PublicURL {
		oauthError(w, http.StatusForbidden, "invalid_request", "MCP requests must not originate from another website.")
		return
	}
	if _, exists := r.URL.Query()["access_token"]; exists {
		oauthError(w, http.StatusBadRequest, "invalid_request", "Use the Authorization header for MCP access tokens.")
		return
	}
	if grant.Handler == nil {
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "MCP tools are not configured.")
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), grant.ExpiresAt)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	grant.Handler.ServeHTTP(w, r)
}

func readForm(w http.ResponseWriter, r *http.Request) (url.Values, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" {
		return nil, errors.New("invalid form type or query")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	defer clear(body)
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	for _, values := range form {
		if len(values) != 1 || len(values[0]) > 4096 {
			return nil, errors.New("invalid form parameters")
		}
	}
	return form, nil
}

func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c) {
			continue
		}
		return false
	}
	return true
}

func validOpaqueID(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}
