// Package daemonlogin exchanges user credentials for an OpenSVC access JWT.
// It has no persistence, refresh support or MCP token issuer.
package daemonlogin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
)

var (
	ErrCredentials     = errors.New("OpenSVC credentials refused")
	ErrForbidden       = errors.New("OpenSVC permissions refused")
	ErrUnavailable     = errors.New("OpenSVC daemon unavailable")
	ErrInvalidResponse = errors.New("OpenSVC daemon response invalid or target identity mismatched")
)

// Session is server-only state; AccessToken must never be serialized to clients.
type Session struct {
	ClusterRef  string
	ClusterID   string
	Endpoint    string
	Username    string
	AccessToken string `json:"-"`
	ExpiresAt   time.Time
}

// Authenticate uses only the first configured endpoint in this increment.
// The timeout bounds the whole exchange; no redirect or implicit retry may
// forward credentials elsewhere. Transports are built only on login submission.
func Authenticate(ctx context.Context, cluster clusterconfig.Cluster, username, password string) (Session, error) {
	if len(cluster.Endpoints) == 0 || cluster.RequestTimeout <= 0 {
		return Session{}, ErrInvalidResponse
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cluster.CAPEM) {
		return Session{}, ErrInvalidResponse
	}
	verifier, err := newJWTVerifier(cluster.CAPEM)
	if err != nil {
		return Session{}, ErrInvalidResponse
	}
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(ctx, cluster.RequestTimeout)
	defer cancel()
	endpoint := cluster.Endpoints[0]
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/auth/token?refresh=false&access_duration=10m", nil)
	if err != nil {
		return Session{}, ErrInvalidResponse
	}
	req.SetBasicAuth(username, password)
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := exchange(client, req, 64<<10, &token); err != nil {
		return Session{}, err
	}
	info, err := verifier.verify(token.AccessToken)
	if err != nil || info.Subject != username || len(info.Grant) == 0 {
		return Session{}, ErrInvalidResponse
	}
	// The JWT issuer is a node name, not the OpenSVC cluster ID. Read identity
	// with the verified JWT and discard it if the configured target differs.
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/cluster/status?namespace=system", nil)
	if err != nil {
		return Session{}, ErrInvalidResponse
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	var status struct {
		Cluster struct {
			Config struct {
				ID string `json:"id"`
			} `json:"config"`
		} `json:"cluster"`
	}
	if err := exchange(client, req, 4<<20, &status); err != nil {
		return Session{}, err
	}
	if status.Cluster.Config.ID != cluster.ExpectedClusterID || !time.Now().Before(info.ExpiresAt.Time) {
		return Session{}, ErrInvalidResponse
	}
	return Session{ClusterRef: cluster.Ref, ClusterID: cluster.ExpectedClusterID, Endpoint: endpoint, Username: info.Subject, AccessToken: token.AccessToken, ExpiresAt: info.ExpiresAt.Time}, nil
}

func exchange(client *http.Client, req *http.Request, limit int64, target any) error {
	response, err := client.Do(req)
	if err != nil {
		// Never propagate request headers, token contents or daemon error bodies.
		return ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrCredentials
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusOK:
	default:
		if response.StatusCode >= 500 {
			return ErrUnavailable
		}
		return ErrInvalidResponse
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || int64(len(data)) > limit || mediaErr != nil || mediaType != "application/json" {
		return ErrInvalidResponse
	}
	if err := json.Unmarshal(data, target); err != nil {
		return ErrInvalidResponse
	}
	return nil
}
