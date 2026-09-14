package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
)

const (
	defaultDaemonURL        = "https://127.0.0.1:1215"
	defaultSocketPath       = "/run/opensvc-daemon-mcp/mcp.sock"
	defaultJWTVerifyKeyFile = "/var/lib/opensvc/certs/ca_certificates"
	defaultTLSInsecure      = false
	maximumUnixPathBytes    = 107
	minDaemonRequestTimeout = time.Second
	maxDaemonRequestTimeout = 2 * time.Minute
)

// Config contains the runtime configuration of the MCP server process.
type Config struct {
	DaemonURL        string
	SocketPath       string
	JWTVerifyKeyFile string
	HTTP             client.HTTPOptions
}

// Load reads and validates process configuration from environment variables.
func Load() (Config, error) {
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
	socketPath, err := cleanUnixSocketPath(getenv("OPENSVC_MCP_SOCKET_PATH", defaultSocketPath))
	if err != nil {
		return Config{}, fmt.Errorf("parse OPENSVC_MCP_SOCKET_PATH: %w", err)
	}
	return Config{
		DaemonURL:        getenv("OPENSVC_DAEMON_URL", defaultDaemonURL),
		SocketPath:       socketPath,
		JWTVerifyKeyFile: getenv("OPENSVC_MCP_JWT_VERIFY_KEY_FILE", defaultJWTVerifyKeyFile),
		HTTP: client.HTTPOptions{
			TLSInsecure: tlsInsecure,
			TLSCAFile:   os.Getenv("OPENSVC_DAEMON_TLS_CA_FILE"),
			Timeout:     daemonRequestTimeout,
		},
	}, nil
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
