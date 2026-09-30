package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/daemonlogin"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/oauth"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName         = "opensvc-daemon-mcp"
	serverVersion      = "v0.1.0"
	maxHTTPHeaderBytes = 64 << 10
	shutdownTimeout    = 30 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	handler, err := newMCPHandler(cfg)
	if err != nil {
		log.Fatal(err)
	}

	listener, tlsConfig, err := listenMCP(cfg)
	if err != nil {
		log.Fatalf("listen for MCP HTTP API: %v", err)
	}
	defer listener.Close()
	httpServer := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- httpServer.ServeTLS(listener, "", "")
	}()

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	log.Printf("%s %s listening on https://%s/mcp", serverName, serverVersion, listener.Addr())
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

// OAuth authorization resolves a cluster-bound, server-held daemon session.
func newMCPHandler(cfg config.Config) (http.Handler, error) {
	if cfg.OAuth.PublicURL != "" {
		server, err := oauth.New(cfg.OAuth, newSessionMCPHandler)
		if err != nil {
			return nil, fmt.Errorf("configure remote OAuth server: %w", err)
		}
		return server.Handler(), nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Service Unavailable","status":503,"detail":"Remote MCP authorization is not configured."}`))
	})
	return mux, nil
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

func newSessionMCPHandler(cluster clusterconfig.Cluster, session daemonlogin.Session) (http.Handler, error) {
	api, err := client.NewSession(cluster, session)
	if err != nil {
		return nil, err
	}
	service := core.New(api)
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterDaemonTools, tools.RegisterClusterTools, tools.RegisterNodeTools,
		tools.RegisterObjectTools, tools.RegisterInstanceTools, tools.RegisterResourceTools, tools.RegisterScheduleTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	// Tools require no persistent protocol session. Authorization is resolved on
	// every request, and each token owns its fixed cluster/user tool instance.
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}), nil
}
