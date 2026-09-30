package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/oauth"
)

const (
	defaultDaemonURL        = "https://127.0.0.1:1215"
	defaultSocketPath       = "/run/opensvc-daemon-mcp/mcp.sock"
	defaultJWTVerifyKeyFile = "/var/lib/opensvc/certs/ca_certificates"
	defaultTLSInsecure      = false
	defaultTransport        = "unix"
	defaultListenAddress    = "127.0.0.1:8443"
	maximumUnixPathBytes    = 107
	minDaemonRequestTimeout = time.Second
	maxDaemonRequestTimeout = 2 * time.Minute
)

// Config contains the runtime configuration of the MCP server process.
type Config struct {
	Transport         string
	ListenAddress     string
	TLSCertFile       string
	TLSKeyFile        string
	DaemonURL         string
	SocketPath        string
	JWTVerifyKeyFile  string
	HTTP              client.HTTPOptions
	OAuth             oauth.Config
	ClusterConfigFile string
}

// Load reads and validates process configuration from environment variables.
func Load() (Config, error) {
	transport := strings.TrimSpace(getenv("OPENSVC_MCP_TRANSPORT", defaultTransport))
	if transport != "unix" && transport != "https" {
		return Config{}, fmt.Errorf("OPENSVC_MCP_TRANSPORT must be unix or https")
	}
	listenAddress := strings.TrimSpace(os.Getenv("OPENSVC_MCP_LISTEN_ADDR"))
	certFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_TLS_KEY_FILE"))
	if transport == "unix" && (listenAddress != "" || certFile != "" || keyFile != "") {
		return Config{}, fmt.Errorf("OPENSVC_MCP_LISTEN_ADDR and OPENSVC_MCP_TLS_CERT_FILE/KEY_FILE require OPENSVC_MCP_TRANSPORT=https")
	}
	if transport == "https" {
		if listenAddress == "" {
			listenAddress = defaultListenAddress
		}
		if err := validateListenAddress(listenAddress); err != nil {
			return Config{}, fmt.Errorf("parse OPENSVC_MCP_LISTEN_ADDR: %w", err)
		}
		if !filepath.IsAbs(certFile) || !filepath.IsAbs(keyFile) {
			return Config{}, fmt.Errorf("OPENSVC_MCP_TLS_CERT_FILE and OPENSVC_MCP_TLS_KEY_FILE must be absolute file paths in https mode")
		}
	}
	// Reject removed settings instead of silently starting with another target.
	for _, name := range []string{"OPENSVC_MCP_CLUSTER_REF", "OPENSVC_MCP_CLUSTER_NAME"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return Config{}, fmt.Errorf("%s has been removed; configure clusters with OPENSVC_MCP_CLUSTER_CONFIG_FILE", name)
		}
	}
	oauthConfig := oauth.Config{PublicURL: strings.TrimSpace(os.Getenv("OPENSVC_MCP_PUBLIC_URL"))}
	clusterFile := strings.TrimSpace(os.Getenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE"))
	if oauthConfig.PublicURL != "" || clusterFile != "" {
		if transport != "https" {
			return Config{}, fmt.Errorf("OPENSVC_MCP_PUBLIC_URL and OPENSVC_MCP_CLUSTER_CONFIG_FILE require OPENSVC_MCP_TRANSPORT=https")
		}
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
	tlsInsecure, err := strconv.ParseBool(
		getenv("OPENSVC_DAEMON_TLS_INSECURE", strconv.FormatBool(defaultTLSInsecure)),
	)
	if err != nil {
		return Config{}, fmt.Errorf("parse OPENSVC_DAEMON_TLS_INSECURE: %w", err)
	}
	daemonRequestTimeout, err := time.ParseDuration(
		getenv("OPENSVC_DAEMON_REQUEST_TIMEOUT", client.DefaultRequestTimeout.String()),
	)
	if err != nil {
		return Config{}, fmt.Errorf("parse OPENSVC_DAEMON_REQUEST_TIMEOUT: %w", err)
	}
	if daemonRequestTimeout < minDaemonRequestTimeout || daemonRequestTimeout > maxDaemonRequestTimeout {
		return Config{}, fmt.Errorf(
			"OPENSVC_DAEMON_REQUEST_TIMEOUT must be between %s and %s",
			minDaemonRequestTimeout,
			maxDaemonRequestTimeout,
		)
	}
	var socketPath string
	if transport == "unix" {
		socketPath, err = cleanUnixSocketPath(getenv("OPENSVC_MCP_SOCKET_PATH", defaultSocketPath))
		if err != nil {
			return Config{}, fmt.Errorf("parse OPENSVC_MCP_SOCKET_PATH: %w", err)
		}
	}
	return Config{
		Transport:         transport,
		ListenAddress:     listenAddress,
		TLSCertFile:       certFile,
		TLSKeyFile:        keyFile,
		OAuth:             oauthConfig,
		ClusterConfigFile: clusterFile,
		DaemonURL:         getenv("OPENSVC_DAEMON_URL", defaultDaemonURL),
		SocketPath:        socketPath,
		JWTVerifyKeyFile:  getenv("OPENSVC_MCP_JWT_VERIFY_KEY_FILE", defaultJWTVerifyKeyFile),
		HTTP: client.HTTPOptions{
			TLSInsecure: tlsInsecure,
			TLSCAFile:   os.Getenv("OPENSVC_DAEMON_TLS_CA_FILE"),
			Timeout:     daemonRequestTimeout,
		},
	}, nil
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

func cleanUnixSocketPath(value string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(value))
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute")
	}
	if path == string(filepath.Separator) {
		return "", fmt.Errorf("path must name a socket")
	}
	if len([]byte(path)) > maximumUnixPathBytes {
		return "", fmt.Errorf("path exceeds the Linux Unix socket limit of %d bytes", maximumUnixPathBytes)
	}
	return path, nil
}

func getenv(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
