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

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/auth"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/clusterconfig"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/core"
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
	warnInsecureDaemonTLS(cfg.Clusters, log.Default())

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

// Emit one explicit startup warning for each administrator-selected demo target.
func warnInsecureDaemonTLS(catalog *clusterconfig.Catalog, logger *log.Logger) {
	for _, cluster := range catalog.List() {
		if cluster.TLSInsecure {
			logger.Printf("WARNING: cluster=%s tls.insecure=true disables daemon certificate chain and hostname verification; JWT theft and forged whoami responses are possible; DEMOS ONLY, DO NOT USE IN PRODUCTION", cluster.Ref)
		}
	}
}

// Native delegation selects a daemon from checked, unverified claims on every request.
func newMCPHandler(cfg config.Config) (http.Handler, error) {
	checker, err := auth.NewChecker(cfg.Clusters)
	if err != nil {
		return nil, fmt.Errorf("configure native JWT delegation: %w", err)
	}
	api, err := client.NewRouted(cfg.Clusters)
	if err != nil {
		return nil, err
	}
	handler, err := newToolsHandler(api)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", checker.Middleware(handler))
	mux.Handle("GET /mcp/auth/whoami", checker.Middleware(serveWhoAmI(api)))
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

func newToolsHandler(api *client.RoutedClient) (http.Handler, error) {
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
	// Tools require no persistent protocol session. Delegation is checked on
	// every request; the shared service never holds a user's credentials.
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		CrossOriginProtection: &http.CrossOriginProtection{},
	}), nil
}
