package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

const (
	defaultListenAddress = "127.0.0.1:8443"
	// maxUnixSocketPathBytes is the portable sun_path limit, including the NUL.
	maxUnixSocketPathBytes = 103
)

// Config contains the runtime configuration of the MCP server process. It has
// two optional listeners, at least one of which is enabled: HTTPS for OAuth
// external agents, and a local Unix socket for delegated OpenSVC tokens.
type Config struct {
	// HTTPS listener, enabled when TLSCertFile is set. It requires OAuth.
	ListenAddress string
	TLSCertFile   string
	TLSKeyFile    string
	OAuth         auth.OAuthConfig
	Exchange      *auth.ExchangeProfiles
	// DelegatedSocket enables the Unix socket listener. It requires a catalogue.
	DelegatedSocket   string
	Clusters          *clusterconfig.Catalog
	ClusterConfigFile string
	// Actions registers the tools that change the cluster state, on both
	// listeners. They are left out unless OPENSVC_MCP_ACTIONS is enabled.
	Actions bool
}

func (c Config) HTTPSEnabled() bool { return c.TLSCertFile != "" }

// Load reads and validates process configuration from environment variables.
func Load() (Config, error) {
	var cfg Config
	cfg.TLSCertFile = strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE"))
	cfg.TLSKeyFile = strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE"))
	listenAddress := strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR"))
	oauth := auth.OAuthConfig{
		ResourceURL:  strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_RESOURCE_URL")),
		ResourceName: strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_RESOURCE_NAME")),
		Issuer:       strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_ISSUER")),
		CAFile:       strings.TrimSpace(os.Getenv("OPENSVC_MCP_OAUTH_CA_FILE")),
	}
	exchangeFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_AUTH_CONFIG_FILE"))
	if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		if !filepath.IsAbs(cfg.TLSCertFile) || !filepath.IsAbs(cfg.TLSKeyFile) {
			return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths")
		}
		if listenAddress == "" {
			listenAddress = defaultListenAddress
		}
		if err := validateListenAddress(listenAddress); err != nil {
			return Config{}, fmt.Errorf("parse OPENSVC_MCP_LISTEN_ADDR: %w", err)
		}
		if err := oauth.Validate(); err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_OAUTH configuration: %w", err)
		}
		cfg.ListenAddress, cfg.OAuth = listenAddress, oauth
	} else if listenAddress != "" || oauth != (auth.OAuthConfig{}) || exchangeFile != "" {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR, OPENSVC_MCP_OAUTH_* and OPENSVC_MCP_AUTH_CONFIG_FILE require the HTTPS listener (OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE)")
	}
	if socket := strings.TrimSpace(os.Getenv("OPENSVC_MCP_DELEGATED_SOCKET")); socket != "" {
		path := filepath.Clean(socket)
		if !filepath.IsAbs(path) || path == string(filepath.Separator) || len(path) > maxUnixSocketPathBytes {
			return Config{}, fmt.Errorf("OPENSVC_MCP_DELEGATED_SOCKET must be an absolute socket path of at most %d bytes", maxUnixSocketPathBytes)
		}
		cfg.DelegatedSocket = path
	}
	if !cfg.HTTPSEnabled() && cfg.DelegatedSocket == "" {
		return Config{}, fmt.Errorf("configure the HTTPS listener (OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE), OPENSVC_MCP_DELEGATED_SOCKET, or both")
	}
	cfg.ClusterConfigFile = strings.TrimSpace(os.Getenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE"))
	if cfg.ClusterConfigFile != "" {
		var err error
		cfg.Clusters, err = clusterconfig.Load(cfg.ClusterConfigFile)
		if err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_CLUSTER_CONFIG_FILE: %w", err)
		}
	}
	if cfg.DelegatedSocket != "" && cfg.Clusters == nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_DELEGATED_SOCKET requires OPENSVC_MCP_CLUSTER_CONFIG_FILE")
	}
	if exchangeFile != "" {
		if cfg.Clusters == nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_AUTH_CONFIG_FILE requires OPENSVC_MCP_CLUSTER_CONFIG_FILE")
		}
		var err error
		cfg.Exchange, err = auth.LoadExchangeProfiles(exchangeFile)
		if err != nil {
			return Config{}, fmt.Errorf("OPENSVC_MCP_AUTH_CONFIG_FILE: %w", err)
		}
	}
	switch actions := strings.TrimSpace(os.Getenv("OPENSVC_MCP_ACTIONS")); actions {
	case "", "disabled":
	case "enabled":
		cfg.Actions = true
	default:
		return Config{}, fmt.Errorf("OPENSVC_MCP_ACTIONS must be enabled or disabled")
	}
	for _, c := range cfg.Clusters.List() {
		if c.AuthProfile == "" {
			continue
		}
		if !cfg.HTTPSEnabled() {
			return Config{}, fmt.Errorf("cluster %s: auth requires the HTTPS listener", c.Ref)
		}
		if !cfg.Exchange.Has(c.AuthProfile) {
			return Config{}, fmt.Errorf("cluster %s references an auth profile missing from OPENSVC_MCP_AUTH_CONFIG_FILE", c.Ref)
		}
	}
	return cfg, nil
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
