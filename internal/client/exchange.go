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

// ExchangeClient binds each call to one configured VIP and one exchanged token.
// It shares connection pools, never a current cluster or user credentials.
type ExchangeClient struct {
	profiles *auth.ExchangeProfiles
	targets  map[string]clusterconfig.Cluster
	clients  map[string]*Client
}

func NewExchange(catalog *clusterconfig.Catalog, profiles *auth.ExchangeProfiles) (*ExchangeClient, error) {
	if catalog.Len() == 0 || profiles == nil {
		return nil, errors.New("token exchange requires clusters with auth and auth profiles")
	}
	c := &ExchangeClient{profiles: profiles, targets: make(map[string]clusterconfig.Cluster), clients: make(map[string]*Client)}
	for _, cluster := range catalog.List() {
		if !profiles.Has(cluster.AuthProfile) {
			return nil, errors.New("cluster references an unknown authentication profile")
		}
		var roots *x509.CertPool
		if len(cluster.CAPEM) > 0 {
			roots = x509.NewCertPool()
			if !roots.AppendCertsFromPEM(cluster.CAPEM) {
				return nil, errors.New("invalid daemon TLS trust")
			}
		}
		base := &http.Transport{
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, InsecureSkipVerify: cluster.TLSInsecure},
			TLSHandshakeTimeout: cluster.RequestTimeout, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		}
		client := &http.Client{Transport: &exchangedTransport{base: base, clusterID: cluster.ID, origin: cluster.Endpoint}, Timeout: cluster.RequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		api, err := New(cluster.Endpoint, client)
		if err != nil {
			return nil, err
		}
		c.targets[cluster.ID] = cluster
		c.clients[cluster.ID] = api
	}
	return c, nil
}

func (c *ExchangeClient) Prepare(ctx context.Context, clusterID string) (context.Context, context.CancelFunc, error) {
	cluster, ok := c.targets[clusterID]
	if !ok {
		return nil, nil, errors.New("Unknown cluster_id. Use list_clusters and specify the cluster explicitly.")
	}
	return c.profiles.Prepare(ctx, cluster.AuthProfile, cluster.Audience, cluster.ID)
}

func (c *ExchangeClient) selected(ctx context.Context) (*Client, error) {
	id, _, ok := auth.ExchangedFromContext(ctx)
	if !ok {
		return nil, errors.New("daemon request requires an unexpired exchanged token")
	}
	api, ok := c.clients[id]
	if !ok {
		return nil, errors.New("daemon target is not configured")
	}
	return api, nil
}

type exchangedTransport struct {
	base              http.RoundTripper
	clusterID, origin string
}

func (t *exchangedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	id, token, ok := auth.ExchangedFromContext(r.Context())
	if !ok || id != t.clusterID {
		return nil, errors.New("daemon request does not match its token exchange")
	}
	if r.URL.User != nil || r.URL.Scheme+"://"+r.URL.Host != t.origin || r.Host != "" && r.Host != r.URL.Host {
		return nil, errors.New("daemon request does not match its configured origin")
	}
	request := r.Clone(r.Context())
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Del(auth.ClusterIDHeader)
	request.Header.Del("X-OpenSVC-Node")
	return t.base.RoundTrip(request)
}

func (c *ExchangeClient) GetJSON(ctx context.Context, path string, query url.Values, output any) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetJSON(ctx, path, query, output)
}

func (c *ExchangeClient) PostJSON(ctx context.Context, path string, query url.Values, input, output any) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.PostJSON(ctx, path, query, input, output)
}

func (c *ExchangeClient) GetSSE(ctx context.Context, path string, query url.Values, consume func(string, string, []byte) error) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetSSE(ctx, path, query, consume)
}

func (c *ExchangeClient) GetStream(ctx context.Context, path string, query url.Values, consume func([]byte) error) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetStream(ctx, path, query, consume)
}

func (c *ExchangeClient) GetFile(ctx context.Context, path string, query url.Values) ([]byte, error) {
	api, err := c.selected(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetFile(ctx, path, query)
}

func (c *ExchangeClient) GetText(ctx context.Context, path string, query url.Values) ([]byte, error) {
	api, err := c.selected(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetText(ctx, path, query)
}

func (c *ExchangeClient) GetNoContent(ctx context.Context, path string, query url.Values) error {
	api, err := c.selected(ctx)
	if err != nil {
		return err
	}
	return api.GetNoContent(ctx, path, query)
}
