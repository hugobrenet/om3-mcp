package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/auth"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName         = "opensvc-daemon-mcp"
	serverVersion      = "v0.1.0"
	unixSocketMode     = 0o660
	maxHTTPHeaderBytes = 64 << 10
	socketProbeTimeout = 100 * time.Millisecond
	shutdownTimeout    = 30 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	verifier, err := auth.NewJWTVerifier(cfg.JWTVerifyKeyFile)
	if err != nil {
		log.Fatal(err)
	}

	httpClient, err := client.NewHTTPClient(cfg.HTTP)
	if err != nil {
		log.Fatal(err)
	}

	apiClient, err := client.New(cfg.DaemonURL, httpClient)
	if err != nil {
		log.Fatal(err)
	}
	service := core.New(apiClient)

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
		nil,
	)
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		log.Fatal(err)
	}
	if err := tools.RegisterDaemonTools(registrar, service); err != nil {
		log.Fatal(err)
	}
	if err := tools.RegisterClusterTools(registrar, service); err != nil {
		log.Fatal(err)
	}
	if err := tools.RegisterObjectTools(registrar, service); err != nil {
		log.Fatal(err)
	}
	if err := tools.RegisterInstanceTools(registrar, service); err != nil {
		log.Fatal(err)
	}
	if err := tools.RegisterResourceTools(registrar, service); err != nil {
		log.Fatal(err)
	}

	streamHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(verifier.Verify)(streamHandler))

	listener, err := listenUnixSocket(cfg.SocketPath)
	if err != nil {
		log.Fatalf("listen for MCP HTTP API: %v", err)
	}
	httpServer := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.Serve(listener)
	}()

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	log.Printf("%s %s listening on unix://%s (HTTP /mcp)", serverName, serverVersion, cfg.SocketPath)
	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve MCP HTTP API: %v", err)
		}
	case <-signalContext.Done():
		stopSignals()
		log.Printf("%s shutting down with a %s deadline", serverName, shutdownTimeout)
		if err := shutdownHTTPServer(httpServer, shutdownTimeout); err != nil {
			log.Printf("force MCP HTTP API shutdown: %v", err)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve MCP HTTP API during shutdown: %v", err)
		}
	}
}

func listenUnixSocket(path string) (*net.UnixListener, error) {
	if err := removeStaleUnixSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on Unix socket %s: %w", path, err)
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(path, unixSocketMode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set Unix socket %s mode to %04o: %w", path, unixSocketMode, err)
	}
	return listener, nil
}

func removeStaleUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Unix socket path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refuse to remove non-socket path %s", path)
	}

	connection, probeErr := net.DialTimeout("unix", path, socketProbeTimeout)
	if probeErr == nil {
		_ = connection.Close()
		return fmt.Errorf("Unix socket %s is already accepting connections", path)
	}
	if errors.Is(probeErr, os.ErrNotExist) {
		return nil
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("probe existing Unix socket %s: %w", path, probeErr)
	}

	currentInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reinspect stale Unix socket %s: %w", path, err)
	}
	if currentInfo.Mode()&os.ModeSocket == 0 || !os.SameFile(info, currentInfo) {
		return fmt.Errorf("Unix socket path %s changed while checking whether it was stale", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket %s: %w", path, err)
	}
	return nil
}

func shutdownHTTPServer(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		shutdownErr := fmt.Errorf("graceful HTTP shutdown: %w", err)
		if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			return errors.Join(shutdownErr, fmt.Errorf("close HTTP server: %w", closeErr))
		}
		return shutdownErr
	}
	return nil
}
