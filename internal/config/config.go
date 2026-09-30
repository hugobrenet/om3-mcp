package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/oauth"
)

const defaultListenAddress = "127.0.0.1:8443"

// Config contains the runtime configuration of the HTTPS MCP server process.
type Config struct {
	ListenAddress     string
	TLSCertFile       string
	TLSKeyFile        string
	OAuth             oauth.Config
	ClusterConfigFile string
}

// Reject obsolete settings rather than silently ignoring a deployment's
// authentication or target configuration.
var removedVariables = map[string]string{
	"OPENSVC_MCP_TRANSPORT":           "HTTPS is the only transport; remove this variable",
	"OPENSVC_MCP_SOCKET_PATH":         "configure OPENSVC_MCP_LISTEN_ADDR and TLS files",
	"OPENSVC_MCP_JWT_VERIFY_KEY_FILE": "configure cluster trust in OPENSVC_MCP_CLUSTER_CONFIG_FILE",
	"OPENSVC_DAEMON_URL":              "configure endpoints in OPENSVC_MCP_CLUSTER_CONFIG_FILE",
	"OPENSVC_DAEMON_TLS_CA_FILE":      "configure cluster trust in OPENSVC_MCP_CLUSTER_CONFIG_FILE",
	"OPENSVC_DAEMON_TLS_INSECURE":     "daemon TLS verification is mandatory",
	"OPENSVC_DAEMON_REQUEST_TIMEOUT":  "configure request_timeout in OPENSVC_MCP_CLUSTER_CONFIG_FILE",
	"OPENSVC_MCP_CLUSTER_REF":         "configure clusters with OPENSVC_MCP_CLUSTER_CONFIG_FILE",
	"OPENSVC_MCP_CLUSTER_NAME":        "configure clusters with OPENSVC_MCP_CLUSTER_CONFIG_FILE",
}

// Load reads and validates process configuration from environment variables.
func Load() (Config, error) {
	for name, migration := range removedVariables {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return Config{}, fmt.Errorf("%s has been removed; %s", name, migration)
		}
	}
	listenAddress := strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR"))
	if listenAddress == "" {
		listenAddress = defaultListenAddress
	}
	if err := validateListenAddress(listenAddress); err != nil {
		return Config{}, fmt.Errorf("parse OPENSVC_MCP_LISTEN_ADDR: %w", err)
	}
	certFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE"))
	if !filepath.IsAbs(certFile) || !filepath.IsAbs(keyFile) {
		return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths")
	}
	oauthConfig := oauth.Config{PublicURL: strings.TrimSpace(os.Getenv("OPENSVC_MCP_PUBLIC_URL"))}
	clusterFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE"))
	if oauthConfig.PublicURL != "" || clusterFile != "" {
		if oauthConfig.PublicURL == "" || clusterFile == "" {
			return Config{}, fmt.Errorf("OPENSVC_MCP_PUBLIC_URL and OPENSVC_MCP_CLUSTER_CONFIG_FILE must both be supplied")
		}
		catalog, err := clusterconfig.Load(clusterFile)
		if err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_CLUSTER_CONFIG_FILE: %w", err)
		}
		oauthConfig.Clusters = catalog
		if err := oauthConfig.Validate(); err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_PUBLIC_URL / OPENSVC_MCP_CLUSTER_CONFIG_FILE: %w", err)
		}
	}
	return Config{ListenAddress: listenAddress, TLSCertFile: certFile, TLSKeyFile: keyFile, OAuth: oauthConfig, ClusterConfigFile: clusterFile}, nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("expected IP:port: %w", err)
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("host must be an explicit IPv4 or IPv6 address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}
