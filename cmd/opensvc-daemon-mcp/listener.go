package main

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
)

// listenMCP selects exactly one transport. TLS material is checked before
// opening the TCP socket; HTTPS startup never falls back to plaintext or Unix.
func listenMCP(cfg config.Config) (net.Listener, *tls.Config, error) {
	switch cfg.Transport {
	case "unix":
		listener, err := listenUnixSocket(cfg.SocketPath)
		return listener, nil, err
	case "https":
		certificate, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("load MCP TLS certificate and private key: %w", err)
		}
		listener, err := net.Listen("tcp", cfg.ListenAddress)
		if err != nil {
			return nil, nil, fmt.Errorf("listen on MCP TCP address %s: %w", cfg.ListenAddress, err)
		}
		return listener, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported MCP transport %q", cfg.Transport)
	}
}
