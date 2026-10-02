package main

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
)

// listenMCP validates TLS material before opening the TCP listener.
func listenMCP(cfg config.Config) (net.Listener, *tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load MCP TLS certificate and private key: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("listen on MCP TCP address %s: %w", cfg.ListenAddress, err)
	}
	return listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, nil
}
