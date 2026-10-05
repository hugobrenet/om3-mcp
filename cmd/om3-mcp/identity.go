package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/client"
)

// serveWhoAmI is a narrow identity bridge, not an MCP tool or token issuer.
// It proves native/OpenID authentication by asking the catalogue-selected daemon.
func serveWhoAmI(api *client.RoutedClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		delegation, _, ok := auth.FromContext(r.Context())
		if !ok {
			writeIdentityProblem(w, 401)
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			writeIdentityProblem(w, 400)
			return
		}
		var identity struct {
			Name string `json:"name"`
			Auth string `json:"auth"`
		}
		if err := api.GetJSON(r.Context(), "/api/auth/whoami", nil, &identity); err != nil {
			if !time.Now().Before(delegation.ExpiresAt) {
				writeIdentityProblem(w, 401)
				return
			}
			var apiError *client.APIError
			if errors.As(err, &apiError) && (apiError.StatusCode == 401 || apiError.StatusCode == 403) {
				writeIdentityProblem(w, apiError.StatusCode)
				return
			}
			writeIdentityProblem(w, 502)
			return
		}
		// A public/basic response or a different JWT strategy cannot authenticate
		// this profile. OpenID names use preferred_username, email, then sub;
		// the daemon verifies those signed claims, issuer and audience itself.
		if identity.Auth != delegation.Strategy || identity.Name == "" || identity.Name != delegation.Username {
			writeIdentityProblem(w, 401)
			return
		}
		if _, _, ok := auth.FromContext(r.Context()); !ok {
			writeIdentityProblem(w, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			ClusterID string    `json:"cluster_id"`
			Issuer    string    `json:"issuer"`
			Subject   string    `json:"subject"`
			ExpiresAt time.Time `json:"expires_at"`
		}{delegation.ClusterID, delegation.Issuer, delegation.Subject, delegation.ExpiresAt})
	}
}

func writeIdentityProblem(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	if status == 401 {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
	}{"about:blank", http.StatusText(status), status})
}
