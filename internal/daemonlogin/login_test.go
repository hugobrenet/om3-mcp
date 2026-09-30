package daemonlogin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
)

func TestAuthenticateVerifiedTLSJWTAndClusterIdentity(t *testing.T) {
	const user = "alice"
	const password = "synthetic-password"
	var token string
	var calls int
	mode := "ok"
	d := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auth/token" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != password || r.Method != "POST" || r.URL.Query().Get("refresh") != "false" || r.URL.Query().Get("subject") != "" || r.URL.Query().Get("access_duration") != "10m" {
				t.Error("daemon login request contract changed")
			}
			switch mode {
			case "401":
				w.WriteHeader(401)
				w.Write([]byte(password))
				return
			case "403":
				w.WriteHeader(403)
				return
			case "503":
				w.WriteHeader(503)
				return
			case "redirect":
				w.Header().Set("Location", "https://attacker.example/token")
				w.WriteHeader(307)
				return
			case "malformed":
				w.Write([]byte(password))
				return
			case "oversized":
				w.Write([]byte(strings.Repeat("x", 64<<10+1)))
				return
			case "trailing":
				w.Write([]byte(`{"access_token":"ignored"}{}`))
				return
			case "wrong content type":
				w.Header().Set("Content-Type", "application/jsonx")
			case "timeout":
				<-r.Context().Done()
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": token, "access_expired_at": time.Now().Add(time.Minute).Format(time.RFC3339)})
		} else if r.URL.Path == "/api/cluster/status" {
			if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("namespace") != "system" {
				t.Error("identity check did not use the JWT")
			}
			id := "example-id"
			if mode == "wrong cluster" {
				id = "other-id"
			}
			json.NewEncoder(w).Encode(map[string]any{"cluster": map[string]any{"config": map[string]string{"id": id}}})
		} else {
			t.Error("unexpected daemon route")
		}
	}))
	cluster := clusterconfig.Cluster{Ref: "cluster-a", Name: "Example cluster", ExpectedClusterID: "example-id", Endpoints: []string{d.Server.URL, "https://192.0.2.21:1215"}, CAPEM: d.CAPEM, RequestTimeout: 2 * time.Second}
	sign := func(changes jwt.MapClaims) string {
		claims := jwt.MapClaims{"sub": user, "iss": "node-a", "exp": time.Now().Add(time.Minute).Unix(), "token_use": "access", "grant": []string{"guest:example"}}
		for key, value := range changes {
			if value == nil {
				delete(claims, key)
			} else {
				claims[key] = value
			}
		}
		raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(d.CAKey)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	token = sign(nil)
	result, err := Authenticate(t.Context(), cluster, user, password)
	if err != nil || result.Username != user || result.ClusterID != "example-id" || result.Endpoint != d.Server.URL || result.AccessToken != token || calls != 2 {
		t.Fatalf("expected verified JWT and target identity, got error %v, calls %d", err, calls)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), password) {
		t.Fatal("session serialization leaks credentials")
	}
	for _, tc := range []struct {
		mode    string
		want    error
		changes jwt.MapClaims
	}{
		{"401", ErrCredentials, nil}, {"403", ErrForbidden, nil}, {"503", ErrUnavailable, nil},
		{"redirect", ErrInvalidResponse, nil}, {"malformed", ErrInvalidResponse, nil}, {"oversized", ErrInvalidResponse, nil}, {"trailing", ErrInvalidResponse, nil},
		{"wrong cluster", ErrInvalidResponse, nil},
		{"wrong content type", ErrInvalidResponse, nil},
		{"expired", ErrInvalidResponse, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}},
		{"missing expiration", ErrInvalidResponse, jwt.MapClaims{"exp": nil}},
		{"wrong subject", ErrInvalidResponse, jwt.MapClaims{"sub": "bob"}},
		{"refresh JWT", ErrInvalidResponse, jwt.MapClaims{"token_use": "refresh"}},
		{"missing issuer", ErrInvalidResponse, jwt.MapClaims{"iss": ""}},
		{"no grants", ErrInvalidResponse, jwt.MapClaims{"grant": []string{}}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			mode = tc.mode
			calls = 0
			token = sign(tc.changes)
			result, err := Authenticate(t.Context(), cluster, user, password)
			if !errors.Is(err, tc.want) || result.AccessToken != "" || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), token) {
				t.Fatalf("failure contract: %v", err)
			}
			if tc.mode != "wrong cluster" && calls != 1 {
				t.Errorf("unexpected identity check after login failure: %d", calls)
			}
		})
	}
	mode = "ok"
	token = sign(nil)
	t.Run("wrong signature", func(t *testing.T) {
		parts := strings.Split(token, ".")
		parts[2] = strings.Repeat("A", len(parts[2]))
		token = strings.Join(parts, ".")
		if _, err := Authenticate(t.Context(), cluster, user, password); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("invalid signature accepted: %v", err)
		}
	})
	t.Run("untrusted TLS", func(t *testing.T) {
		other := testutil.NewDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("credentials reached untrusted daemon") }))
		wrong := cluster
		wrong.CAPEM = other.CAPEM
		if _, err := Authenticate(t.Context(), wrong, user, password); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("untrusted TLS accepted: %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := Authenticate(ctx, cluster, user, password); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("cancelled login accepted: %v", err)
		}
	})
	t.Run("whole exchange timeout", func(t *testing.T) {
		mode = "timeout"
		short := cluster
		short.RequestTimeout = 100 * time.Millisecond
		if _, err := Authenticate(t.Context(), short, user, password); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unbounded login: %v", err)
		}
	})
}
