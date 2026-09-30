// Package oauth implements the remote MCP authorization prototype through login.
// It authenticates OpenSVC users but does not issue OAuth codes or MCP tokens yet.
package oauth

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
)

const Scope = "mcp:access"

// Config contains the canonical origin and immutable administrator catalogue.
// The target is chosen on /login and bound after verified OpenSVC authentication.
type Config struct {
	PublicURL string
	Clusters  *clusterconfig.Catalog
}

// Validate requires an explicit HTTPS origin, independent of the TCP bind
// address and request headers. Routes and the issuer derive from this origin.
func (c Config) Validate() error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(c.PublicURL, "#") {
		return fmt.Errorf("public URL must be an HTTPS origin without a path, credentials, query or fragment")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("public URL port must be between 1 and 65535")
		}
	}
	if c.Clusters.Len() == 0 {
		return fmt.Errorf("a validated cluster catalogue is required")
	}
	return nil
}

func validDisplayName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && strings.TrimSpace(name) == name && utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl)
}
