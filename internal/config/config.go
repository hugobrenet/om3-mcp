package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opensvc/om3-mcp/internal/clusterconfig"
)

const defaultListenAddress = "127.0.0.1:8443"

// Config contains the runtime configuration of the HTTPS MCP server process.
type Config struct {
	ListenAddress     string
	TLSCertFile       string
	TLSKeyFile        string
	Clusters          *clusterconfig.Catalog
	ClusterConfigFile string
}

// Load reads and validates process configuration from environment variables.
func Load() (Config, error) {
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
	clusterFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE"))
	catalog, err := clusterconfig.Load(clusterFile)
	if err != nil {
		return Config{}, fmt.Errorf("OPENSVC_MCP_CLUSTER_CONFIG_FILE: %w", err)
	}
	return Config{ListenAddress: listenAddress, TLSCertFile: certFile, TLSKeyFile: keyFile, Clusters: catalog, ClusterConfigFile: clusterFile}, nil
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
