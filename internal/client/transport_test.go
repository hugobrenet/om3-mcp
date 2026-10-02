package client

import "net/http"

// Test daemon credentials belong to the supplied HTTP transport, independent
// of the MCP request context. This fixture is not a production auth path.
func daemonTestHTTPClient(base *http.Client, token string) *http.Client {
	clone := *base
	clone.Transport = daemonTestTransport{base: base.Transport, token: token}
	return &clone
}

type daemonTestTransport struct {
	base  http.RoundTripper
	token string
}

func (t daemonTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}
