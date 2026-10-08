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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/client"
	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/core"
	"github.com/opensvc/om3-mcp/internal/tools"
)

const (
	serverName         = "om3-mcp"
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

// OAuth authenticates the external client locally. Configured exchange profiles
// enable per-call daemon credentials and catalogue-bound VIP routing.
func newMCPHandler(cfg config.Config) (http.Handler, error) {
	verifier, err := auth.NewOAuthVerifier(cfg.OAuth)
	if err != nil {
		return nil, fmt.Errorf("configure MCP OAuth: %w", err)
	}
	var handler http.Handler
	if cfg.Exchange != nil {
		api, e := client.NewExchange(cfg.Clusters, cfg.Exchange)
		if e != nil {
			return nil, e
		}
		handler, err = newConfiguredToolsHandler(api, cfg.Clusters, api)
	} else {
		handler, err = newToolsHandler(nil)
	}
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", verifier.Metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", verifier.Metadata)
	mux.Handle("/mcp", verifier.Middleware(handler))
	mux.Handle("GET /mcp/auth/whoami", verifier.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "The legacy daemon identity bridge is disabled for external OAuth clients.", http.StatusNotImplemented)
	})))
	// Previous om ai/webapp delegation wiring intentionally disabled. Reusing it
	// here would forward an MCP-audience credential directly to a daemon.
	// checker, err := auth.NewChecker(cfg.Clusters)
	// api, err := client.NewRouted(cfg.Clusters)
	// handler, err := newToolsHandler(api)
	// mux.Handle("/mcp", checker.Middleware(handler))
	// mux.Handle("GET /mcp/auth/whoami", checker.Middleware(serveWhoAmI(api)))
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

func newToolsHandler(api core.JSONGetter) (http.Handler, error) {
	return newConfiguredToolsHandler(api, nil, nil)
}

func newConfiguredToolsHandler(api core.JSONGetter, catalog *clusterconfig.Catalog, router tools.ClusterRouter) (http.Handler, error) {
	service := core.New(api)
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, nil)
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	if router != nil {
		registrar.SetClusterRouter(router)
		server.AddReceivingMiddleware(auditToolCalls)
		if err := tools.RegisterCatalogTool(registrar, catalog); err != nil {
			return nil, err
		}
	}
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterDaemonTools, tools.RegisterClusterTools, tools.RegisterNodeTools,
		tools.RegisterObjectTools, tools.RegisterInstanceTools, tools.RegisterResourceTools, tools.RegisterScheduleTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	if api == nil {
		// Without exchange configuration, tools remain discoverable but blocked.
		// A protocol-level guard also covers active probes and future tools.
		server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
				if method == "tools/call" {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Daemon calls require token exchange configuration and a version 3 catalogue."}}}, nil
				}
				return next(ctx, method, request)
			}
		})
	}
	// Tools require no persistent protocol session. Authentication is checked on
	// every request; the shared service never holds a user's credentials.
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		CrossOriginProtection: &http.CrossOriginProtection{},
	}), nil
}
