package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/daemonlogin"
)

// NewSession binds an API client to one verified daemon session. This transport
// uses only the selected cluster's public trust and server-held access JWT.
func NewSession(cluster clusterconfig.Cluster, session daemonlogin.Session) (*Client, error) {
	if len(cluster.Endpoints) == 0 || cluster.Endpoints[0] != session.Endpoint || cluster.Ref != session.ClusterRef || cluster.ExpectedClusterID != session.ClusterID || session.Username == "" || session.AccessToken == "" || !time.Now().Before(session.ExpiresAt) || cluster.RequestTimeout <= 0 {
		return nil, errors.New("invalid daemon session binding")
	}
	endpoint, err := url.Parse(session.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
		return nil, errors.New("invalid daemon session endpoint")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(cluster.CAPEM) {
		return nil, errors.New("invalid daemon session trust")
	}
	base := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DisableKeepAlives: true}
	httpClient := &http.Client{
		Transport:     &sessionTransport{base: base, origin: endpoint.Scheme + "://" + endpoint.Host, token: session.AccessToken, expiresAt: session.ExpiresAt},
		Timeout:       cluster.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return New(session.Endpoint, httpClient)
}

type sessionTransport struct {
	base      http.RoundTripper
	origin    string
	token     string
	expiresAt time.Time
}

func (t *sessionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.User != nil || request.URL.Scheme+"://"+request.URL.Host != t.origin || request.Host != "" && request.Host != request.URL.Host {
		return nil, errors.New("daemon request does not match its authorized origin")
	}
	if !time.Now().Before(t.expiresAt) {
		return nil, errors.New("daemon session has expired")
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}
