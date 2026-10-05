package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

type target struct{ clusterID, node string }

// RoutedClient shares immutable clients and TLS connection pools, never tokens.
// Each API operation selects a target from checked, unverified request-scoped claims.
type RoutedClient struct{ clients map[target]*Client }

func NewRouted(catalog *clusterconfig.Catalog) (*RoutedClient, error) {
	if catalog.Len() == 0 {
		return nil, errors.New("daemon routing requires a cluster catalogue")
	}
	routed := &RoutedClient{clients: make(map[target]*Client)}
	for _, cluster := range catalog.List() {
		var roots *x509.CertPool
		if len(cluster.CAPEM) > 0 {
			roots = x509.NewCertPool()
			if !roots.AppendCertsFromPEM(cluster.CAPEM) {
				return nil, errors.New("invalid daemon TLS trust")
			}
		}
		base := &http.Transport{
			// Explicit per-cluster demo option. HTTPS and origin binding remain
			// required, but the peer is NOT authenticated when this is true.
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, InsecureSkipVerify: cluster.TLSInsecure},
			TLSHandshakeTimeout: cluster.RequestTimeout,
			IdleConnTimeout:     90 * time.Second, MaxIdleConns: 200, MaxIdleConnsPerHost: 2,
			// Proxy is intentionally nil: delegated credentials travel directly.
		}
		for node, origin := range cluster.Nodes {
			binding := target{cluster.ExpectedClusterID, node}
			httpClient := &http.Client{
				Transport:     &delegatedTransport{base: base, binding: binding, origin: origin},
				Timeout:       cluster.RequestTimeout,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			api, err := New(origin, httpClient)
			if err != nil {
				return nil, err
			}
			routed.clients[binding] = api
		}
	}
	return routed, nil
}

func (c *RoutedClient) selected(ctx context.Context) (*Client, error) {
	identity, _, ok := auth.FromContext(ctx)
	if !ok {
		return nil, errors.New("daemon request requires a checked delegated JWT")
	}
	api, ok := c.clients[target{identity.ClusterID, identity.Node}]
	if !ok {
		return nil, errors.New("daemon target is not configured")
	}
	return api, nil
}

type delegatedTransport struct {
	base    http.RoundTripper
	binding target
	origin  string
}

func (t *delegatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	identity, token, ok := auth.FromContext(request.Context())
	if !ok || (target{identity.ClusterID, identity.Node}) != t.binding {
		return nil, errors.New("daemon request does not match its checked delegation")
	}
	if request.URL.User != nil || request.URL.Scheme+"://"+request.URL.Host != t.origin || request.Host != "" && request.Host != request.URL.Host {
		return nil, errors.New("daemon request does not match its authorized origin")
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	clone.Header.Del(auth.ClusterIDHeader)
	clone.Header.Del(auth.NodeHeader)
	return t.base.RoundTrip(clone)
}

func (c *RoutedClient) GetJSON(ctx context.Context, path string, query url.Values, output any) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetJSON(ctx, path, query, output)
}

func (c *RoutedClient) PostJSON(ctx context.Context, path string, query url.Values, input, output any) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.PostJSON(ctx, path, query, input, output)
}

func (c *RoutedClient) GetSSE(ctx context.Context, path string, query url.Values, consume func(string, string, []byte) error) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetSSE(ctx, path, query, consume)
}

func (c *RoutedClient) GetStream(ctx context.Context, path string, query url.Values, consume func([]byte) error) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetStream(ctx, path, query, consume)
}

func (c *RoutedClient) GetFile(ctx context.Context, path string, query url.Values) ([]byte, error) {
	api, err := c.selected(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetFile(ctx, path, query)
}

func (c *RoutedClient) GetText(ctx context.Context, path string, query url.Values) ([]byte, error) {
	api, err := c.selected(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetText(ctx, path, query)
}

func (c *RoutedClient) GetNoContent(ctx context.Context, path string, query url.Values) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetNoContent(ctx, path, query)
}
