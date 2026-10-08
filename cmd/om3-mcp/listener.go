package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/opensvc/om3-mcp/internal/config"
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

// listenDelegated opens the delegated-token Unix socket. A stale socket left
// by a stopped process is replaced; a live socket or any other file at the
// path is refused. Access is controlled by the directory permissions and the
// socket group; the socket itself is restricted to owner and group.
func listenDelegated(path string) (net.Listener, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode().Type() != fs.ModeSocket:
		return nil, fmt.Errorf("delegated socket path %s exists and is not a socket", path)
	case err == nil:
		if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
			conn.Close()
			return nil, fmt.Errorf("delegated socket %s is already in use", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale delegated socket %s: %w", path, err)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("inspect delegated socket path %s: %w", path, err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on delegated socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		listener.Close()
		return nil, fmt.Errorf("restrict delegated socket %s: %w", path, err)
	}
	return listener, nil
}
