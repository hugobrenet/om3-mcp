package oauth

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
)

//go:embed login.html
var loginHTML string

var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	// POST is rejected without reading the request body, even if a caller
	// bypasses the disabled HTML controls and sends real credentials.
	if !method(w, r, http.MethodGet) {
		return
	}
	noStore(w)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
	cookie, err := r.Cookie(loginCookieName)
	if err != nil || r.URL.RawQuery != "" {
		oauthError(w, 400, "invalid_request", "Start a new connection from your MCP client.")
		return
	}
	s.mu.Lock()
	s.prune(s.now())
	a, exists := s.requests[cookie.Value]
	s.mu.Unlock()
	if !exists || a.ClusterRef != s.cfg.ClusterRef {
		http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		oauthError(w, 400, "invalid_request", "This connection request is missing or expired. Start a new connection from your MCP client.")
		return
	}
	var page bytes.Buffer
	if err := loginTemplate.Execute(&page, struct{ ClientName, ClusterName string }{a.ClientName, s.cfg.ClusterName}); err != nil {
		oauthError(w, 500, "server_error", "Unable to display the login form.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page.Bytes())
}
