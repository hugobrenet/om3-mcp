package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/client"
)

// serveWhoAmI is a narrow identity bridge, not an MCP tool or token issuer.
// It is served only on the delegated Unix socket, and proves native/OpenID
// authentication by asking the daemon of the cluster named by the header.
func serveWhoAmI(api *client.DelegatedClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		delegation, _, ok := auth.DelegationFromContext(r.Context())
		// refuse answers a problem and records one audit line for it.
		refuse := func(status int, outcome string, extra ...slog.Attr) {
			writeIdentityProblem(w, status)
			auditWhoAmI(r.Context(), delegation, outcome, status, started, extra...)
		}
		if !ok {
			refuse(401, "expired")
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength != 0 {
			refuse(400, "bad_request")
			return
		}
		var identity struct {
			Name string `json:"name"`
			Auth string `json:"auth"`
		}
		if err := api.GetJSON(r.Context(), "/api/auth/whoami", nil, &identity); err != nil {
			if !time.Now().Before(delegation.ExpiresAt) {
				refuse(401, "expired")
				return
			}
			var apiError *client.APIError
			if errors.As(err, &apiError) && (apiError.StatusCode == 401 || apiError.StatusCode == 403) {
				refuse(apiError.StatusCode, "daemon_refused", slog.Int("daemon_status", apiError.StatusCode))
				return
			}
			extra := []slog.Attr{slog.String("detail", auditValue(err.Error()))}
			if errors.As(err, &apiError) {
				extra = append(extra, slog.Int("daemon_status", apiError.StatusCode))
			}
			refuse(502, "daemon_failed", extra...)
			return
		}
		// A public/basic response or a different JWT strategy cannot authenticate
		// this profile. OpenID names use preferred_username, email, then sub;
		// the daemon verifies those signed claims, issuer and audience itself.
		if identity.Auth != delegation.Strategy || identity.Name == "" || identity.Name != delegation.Username {
			refuse(401, "identity_mismatch", slog.String("auth", auditValue(identity.Auth)))
			return
		}
		if _, _, ok := auth.DelegationFromContext(r.Context()); !ok {
			refuse(401, "expired")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			ClusterID string    `json:"cluster_id"`
			Issuer    string    `json:"issuer"`
			Subject   string    `json:"subject"`
			ExpiresAt time.Time `json:"expires_at"`
		}{delegation.ClusterID, delegation.Issuer, delegation.Subject, delegation.ExpiresAt})
		auditWhoAmI(r.Context(), delegation, "ok", 200, started)
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
