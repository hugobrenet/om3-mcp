// Package oauth implements the remote MCP authorization prototype through login.
// It does not authenticate OpenSVC users or issue OAuth codes or tokens yet.
package oauth

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const Scope = "mcp:access"

// Config deliberately contains no credentials. Cluster fields identify the
// target displayed in the prototype; daemon transport configuration comes later.
type Config struct {
	PublicURL   string
	ClusterRef  string
	ClusterName string
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
	if len(c.ClusterRef) == 0 || len(c.ClusterRef) > 64 {
		return fmt.Errorf("cluster reference must contain 1 to 64 ASCII letters, digits, hyphens or underscores")
	}
	for _, r := range c.ClusterRef {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return fmt.Errorf("cluster reference must contain 1 to 64 ASCII letters, digits, hyphens or underscores")
		}
	}
	if !validDisplayName(c.ClusterName) {
		return fmt.Errorf("cluster name must contain 1 to 128 bytes of text without control characters")
	}
	return nil
}

func validDisplayName(name string) bool {
	return len(name) > 0 && len(name) <= 128 && strings.TrimSpace(name) == name && utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl)
}
