package oauth

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
	"errors"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/daemonlogin"
)

//go:embed login.html
var loginHTML string
var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

type loginPage struct {
	ClientName    string
	Clusters      []clusterconfig.ClusterSummary
	CSRFToken     string
	Error         string
	Authenticated bool
	ClusterName   string
	Username      string
	ExpiresAt     string
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	// HTML form POSTs under no-referrer send Origin: null. Preserve the
	// same-origin Origin required below without sending referrers elsewhere.
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		oauthError(w, 405, "invalid_request", "Unsupported HTTP method.")
		return
	}
	if r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request", "Start a new connection from your MCP client.")
		return
	}
	cookie, cookieErr := r.Cookie(loginCookieName)
	s.mu.Lock()
	s.prune(s.now())
	var a authorization
	var exists bool
	if cookieErr == nil {
		a, exists = s.requests[cookie.Value]
	}
	if !exists && r.Method == http.MethodGet {
		if sessionCookie, err := r.Cookie(sessionCookieName); err == nil {
			if session, ok := s.sessions[sessionCookie.Value]; ok {
				s.mu.Unlock()
				cluster, _ := s.cfg.Clusters.Lookup(session.Daemon.ClusterRef)
				renderLogin(w, http.StatusOK, loginPage{Authenticated: true, ClientName: session.Authorization.ClientName, ClusterName: cluster.Name, Username: session.Daemon.Username, ExpiresAt: session.Daemon.ExpiresAt.UTC().Format(time.RFC3339)})
				return
			}
			clearCookie(w, sessionCookieName)
		}
	}
	s.mu.Unlock()
	if !exists {
		clearCookie(w, loginCookieName)
		oauthError(w, 400, "invalid_request", "This connection request is missing or expired. Start a new connection from your MCP client.")
		return
	}
	page := loginPage{ClientName: a.ClientName, Clusters: s.cfg.Clusters.Summaries(), CSRFToken: a.CSRFToken}
	if r.Method == http.MethodGet {
		renderLogin(w, http.StatusOK, page)
		return
	}
	// Validate context and origin before reading any credentials.
	if r.Header.Get("Origin") != s.cfg.PublicURL {
		oauthError(w, 403, "invalid_request", "Login must originate from this MCP.")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		oauthError(w, 415, "invalid_request", "Login requires an encoded HTML form.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		oauthError(w, 400, "invalid_request", "Login form is invalid or too large.")
		return
	}
	form, err := url.ParseQuery(string(body))
	clear(body)
	if err != nil || len(form) != 4 {
		oauthError(w, 400, "invalid_request", "Provide the cluster, username, password and form token.")
		return
	}
	for _, field := range []string{"cluster_ref", "username", "secret", "csrf_token"} {
		if len(form[field]) != 1 {
			oauthError(w, 400, "invalid_request", "Login parameters must have one value each.")
			return
		}
	}
	if subtle.ConstantTimeCompare([]byte(form.Get("csrf_token")), []byte(a.CSRFToken)) != 1 {
		oauthError(w, 403, "invalid_request", "Invalid login form token.")
		return
	}
	cluster, ok := s.cfg.Clusters.Lookup(form.Get("cluster_ref"))
	username, password := form.Get("username"), form.Get("secret")
	form.Del("secret")
	if !ok || !validDisplayName(username) || strings.ContainsAny(username, "/:") || len(password) == 0 || len(password) > 4096 {
		page.Error = "Choisissez un cluster enregistré et renseignez des identifiants valides."
		renderLogin(w, 400, page)
		return
	}
	s.mu.Lock()
	s.prune(s.now())
	current, live := s.requests[cookie.Value]
	if !live || current.InFlight || current.Attempts >= maxLoginAttempts || s.inFlight >= maxConcurrentLogins || len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		oauthError(w, 429, "temporarily_unavailable", "Start a new connection or retry later; login is unavailable for this request.")
		return
	}
	current.Attempts++
	current.InFlight = true
	s.requests[cookie.Value] = current
	s.inFlight++
	s.mu.Unlock()
	result, authErr := daemonlogin.Authenticate(r.Context(), cluster, username, password)
	password = ""
	sessionID, randomErr := randomID()
	s.mu.Lock()
	s.inFlight--
	s.prune(s.now())
	current, live = s.requests[cookie.Value]
	if live {
		current.InFlight = false
		s.requests[cookie.Value] = current
	}
	if authErr != nil {
		s.mu.Unlock()
		status := http.StatusBadGateway
		page.Error = "La réponse du cluster ne permet pas de valider cette connexion."
		switch {
		case errors.Is(authErr, daemonlogin.ErrCredentials):
			status = 401
			page.Error = "Identifiants OpenSVC refusés."
		case errors.Is(authErr, daemonlogin.ErrForbidden):
			status = 403
			page.Error = "L’utilisateur ne dispose pas des permissions nécessaires."
		case errors.Is(authErr, daemonlogin.ErrUnavailable):
			status = 503
			page.Error = "Le daemon du cluster est indisponible ou sa connexion TLS ne peut pas être validée."
		}
		renderLogin(w, status, page)
		return
	}
	if !live || !s.now().Before(result.ExpiresAt) || randomErr != nil || len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		oauthError(w, 400, "invalid_request", "This connection request expired or could not be completed. Start a new connection.")
		return
	}
	// Consume the form request, rotate its cookie, and retain only the verified
	// JWT plus identity. Passwords never enter persistent or session state.
	current.CSRFToken = ""
	delete(s.requests, cookie.Value)
	if previous, err := r.Cookie(sessionCookieName); err == nil {
		delete(s.sessions, previous.Value)
	}
	s.sessions[sessionID] = authenticatedSession{Authorization: current, Daemon: result}
	s.mu.Unlock()
	clearCookie(w, loginCookieName)
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: sessionID, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: max(1, int(result.ExpiresAt.Sub(s.now()).Seconds())), Expires: result.ExpiresAt})
	http.Redirect(w, r, s.cfg.PublicURL+"/login", http.StatusSeeOther)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func renderLogin(w http.ResponseWriter, status int, page loginPage) {
	var rendered bytes.Buffer
	if err := loginTemplate.Execute(&rendered, page); err != nil {
		oauthError(w, 500, "server_error", "Unable to display the login form.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(rendered.Bytes())
}
