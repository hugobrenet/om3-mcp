package main

import (
	"net/http"

	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/client"
	"github.com/opensvc/om3-mcp/internal/config"
)

// Retain regression coverage for the disconnected om ai/webapp components.
// This helper is not compiled into the server and cannot enable a fallback.
func newLegacyMCPHandler(cfg config.Config) (http.Handler, error) {
	checker, err := auth.NewChecker(cfg.Clusters)
	if err != nil {
		return nil, err
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
