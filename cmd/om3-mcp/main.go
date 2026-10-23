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
	"github.com/opensvc/om3-mcp/internal/resources"
	"github.com/opensvc/om3-mcp/internal/tools"
	"github.com/opensvc/osvc-auth-mcp/mcpauth"
)

const (
	serverName         = "om3-mcp"
	resourceName       = "OpenSVC Daemon MCP"
	maxHTTPHeaderBytes = 64 << 10
	// readTimeout bounds the reading of a whole request, its body included:
	// a client sending it slowly (slowloris) is cut off.
	readTimeout = 10 * time.Second
	// writeTimeout bounds the processing and the response of a request, so
	// that a client reading it slowly is cut off. It stays above the longest
	// tool: a daemon request lasts up to 2 minutes (request_timeout) and
	// refresh_instance_status waits up to 120 s.
	writeTimeout    = 5 * time.Minute
	shutdownTimeout = 30 * time.Second
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	warnInsecureDaemonTLS(cfg.Clusters, log.Default())

	var servers []*http.Server
	serveErrors := make(chan error, 2)
	if cfg.HTTPSEnabled() {
		handler, err := newMCPHandler(cfg)
		if err != nil {
			log.Fatal(err)
		}
		listener, tlsConfig, err := listenMCP(cfg)
		if err != nil {
			log.Fatalf("listen for MCP HTTP API: %v", err)
		}
		server := newHTTPServer(handler)
		server.TLSConfig = tlsConfig
		servers = append(servers, server)
		go func() { serveErrors <- server.ServeTLS(listener, "", "") }()
		log.Printf("%s %s listening on https://%s/mcp", serverName, serverVersion, listener.Addr())
	}
	if cfg.DelegatedSocket != "" {
		handler, err := newDelegatedHandler(cfg)
		if err != nil {
			log.Fatal(err)
		}
		listener, err := listenDelegated(cfg.DelegatedSocket)
		if err != nil {
			log.Fatal(err)
		}
		server := newHTTPServer(handler)
		servers = append(servers, server)
		go func() { serveErrors <- server.Serve(listener) }()
		log.Printf("%s %s listening for delegated tokens on unix:%s", serverName, serverVersion, cfg.DelegatedSocket)
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve MCP HTTP API: %v", err)
		}
	case <-signalContext.Done():
		stopSignals()
		log.Printf("%s shutting down with a %s deadline", serverName, shutdownTimeout)
		for _, server := range servers {
			if err := shutdownHTTPServer(server, shutdownTimeout); err != nil {
				log.Printf("force MCP HTTP API shutdown: %v", err)
			}
		}
		for range servers {
			if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("serve MCP HTTP API during shutdown: %v", err)
			}
		}
	}
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
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

// delegationHeaders select the cluster of a delegated token on the Unix
// socket. The HTTPS listener removes them: an external client chooses its
// cluster through tool arguments only.
var delegationHeaders = []string{auth.ClusterIDHeader, auth.NodeHeader}

// newMCPHandler serves the HTTPS listener. OAuth authenticates the external
// client locally; the tool calls of each identity are limited; exchange
// profiles enable per-call daemon credentials and catalogue-bound VIP routing
// for clusters configured with auth.
func newMCPHandler(cfg config.Config) (http.Handler, error) {
	if cfg.OAuth.ResourceName == "" {
		cfg.OAuth.ResourceName = resourceName
	}
	verifier, err := mcpauth.NewOAuthVerifier(cfg.OAuth)
	if err != nil {
		return nil, fmt.Errorf("configure MCP OAuth: %w", err)
	}
	limiter := mcpauth.NewCallLimiter(cfg.MaxConcurrentCalls, cfg.MaxCallsPerMinute)
	var handler http.Handler
	if catalog := cfg.Clusters.WithAuth(); cfg.Exchange != nil && catalog.Len() > 0 {
		api, e := client.NewExchange(catalog, cfg.Exchange)
		if e != nil {
			return nil, e
		}
		handler, err = newConfiguredToolsHandler(api, catalog, api, cfg.Actions, limiter)
	} else {
		handler, err = newConfiguredToolsHandler(nil, nil, nil, cfg.Actions, limiter)
	}
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", getHealth)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", verifier.Metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", verifier.Metadata)
	mux.Handle("/mcp", verifier.Middleware(handler, delegationHeaders...))
	return mux, nil
}

// newDelegatedHandler serves the local Unix socket. OpenSVC components such
// as the AI agent forward the OpenID Connect access token of the user
// unchanged. The MCP verifies it against the OpenID provider of the cluster
// named by X-OpenSVC-Cluster-ID, then that cluster's daemon enforces its
// grants. Tools are bound to that cluster and take no cluster_id argument.
func newDelegatedHandler(cfg config.Config) (http.Handler, error) {
	// The OpenID providers of the clusters are trusted with the system roots,
	// or with the OAuth trust bundle of the HTTPS listener when it has one.
	caFile := cfg.DelegatedCAFile
	if caFile == "" {
		caFile = cfg.OAuth.CAFile
	}
	verifier, err := mcpauth.NewJWTVerifier(caFile)
	if err != nil {
		return nil, fmt.Errorf("configure delegated token verification: %w", err)
	}
	delegator, err := auth.NewDelegator(cfg.Clusters, verifier)
	if err != nil {
		return nil, err
	}
	api, err := client.NewDelegated(cfg.Clusters)
	if err != nil {
		return nil, err
	}
	handler, err := newConfiguredToolsHandler(api, nil, nil, cfg.Actions, nil)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", delegator.Middleware(handler))
	mux.Handle("GET /mcp/auth/whoami", delegator.Middleware(serveWhoAmI(api)))
	return withRequestID(mux), nil
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

// newConfiguredToolsHandler serves the daemon tools. Its middlewares, from
// the outermost: the audit line of each call, the limits of the identity
// (HTTPS only, limiter nil on the socket), then the block of calls without
// exchange configuration (api nil, HTTPS only).
func newConfiguredToolsHandler(api core.JSONGetter, catalog *clusterconfig.Catalog, router tools.ClusterRouter, actions bool, limiter *mcpauth.CallLimiter) (http.Handler, error) {
	service := core.New(api)
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: serverVersion}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}, Resources: &mcp.ResourceCapabilities{}},
	})
	registrar, err := tools.NewRegistrar(server)
	if err != nil {
		return nil, err
	}
	// Every call is audited, even when it is limited or blocked.
	middlewares := []mcp.Middleware{auditToolCalls}
	if limiter != nil {
		middlewares = append(middlewares, limitToolCalls(limiter))
	}
	if router != nil {
		registrar.SetClusterRouter(router)
		if err := tools.RegisterCatalogTool(registrar, catalog); err != nil {
			return nil, err
		}
	}
	for _, register := range []func(*tools.Registrar, *core.Service) error{
		tools.RegisterDaemonTools, tools.RegisterClusterTools, tools.RegisterNodeTools,
		tools.RegisterObjectTools, tools.RegisterInstanceTools, tools.RegisterResourceTools, tools.RegisterScheduleTools,
		tools.RegisterPoolTools, tools.RegisterNetworkTools, tools.RegisterAuthTools, tools.RegisterNodeConfigTools,
	} {
		if err := register(registrar, service); err != nil {
			return nil, err
		}
	}
	if actions {
		if err := tools.RegisterObjectActionTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterObjectConfigWriteTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterObjectLifecycleTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterInstanceActionTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterNodeActionTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterObjectDataActionTools(registrar, service); err != nil {
			return nil, err
		}
		if err := tools.RegisterNodeConfigWriteTools(registrar, service); err != nil {
			return nil, err
		}
	}
	if err := resources.Register(server); err != nil {
		return nil, err
	}
	if api == nil {
		// Without exchange configuration, tools remain discoverable but blocked.
		// A protocol-level guard also covers active probes and future tools.
		middlewares = append(middlewares, func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
				if method == "tools/call" {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Daemon calls require token exchange configuration and clusters configured with auth."}}}, nil
				}
				return next(ctx, method, request)
			}
		})
	}
	server.AddReceivingMiddleware(middlewares...)
	// Tools require no persistent protocol session. Authentication is checked on
	// every request; the shared service never holds a user's credentials.
	// Cross-origin browser requests are refused with 403, as the MCP SDK
	// recommends, by the protection of net/http around its handler.
	return http.NewCrossOriginProtection().Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
	})), nil
}

// limitToolCalls refuses the tool calls of an identity beyond its limits,
// before any token exchange or daemon request. The audit records them.
func limitToolCalls(limiter *mcpauth.CallLimiter) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, request)
			}
			release, err := limiter.Acquire(ctx)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			defer release()
			return next(ctx, method, request)
		}
	}
}
