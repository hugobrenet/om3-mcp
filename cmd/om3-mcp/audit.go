package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/auth"
)

const maxAuditValueBytes = 256

// auditLogger receives one credential-free line per tool call. Tests replace it.
var auditLogger = slog.Default()

// auditToolCalls traces who called which tool on which cluster, through which
// client application, and the exchange and call outcomes. It never logs tokens,
// secrets, raw arguments or tool results.
func auditToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		call, ok := request.(*mcp.CallToolRequest)
		if method != "tools/call" || !ok || call.Params == nil {
			return next(ctx, method, request)
		}
		identity, _, _ := auth.OAuthFromContext(ctx)
		ctx, trace := auth.WithCallTrace(ctx)
		start := time.Now()
		result, err := next(ctx, method, request)

		var target struct {
			ClusterID string `json:"cluster_id"`
		}
		_ = json.Unmarshal(call.Params.Arguments, &target)
		exchange, daemonSubject := trace.Exchange, trace.DaemonSubject
		if exchange == "" {
			exchange = "none"
		}
		// On the Unix socket, the cluster comes from the header and the daemon
		// receives the caller's token unchanged. Its claims are verified by the
		// daemon on each call: a refused call may log unverified claims.
		if delegation, _, ok := auth.DelegationFromContext(ctx); ok {
			identity.Issuer, identity.Subject = delegation.Issuer, delegation.Subject
			target.ClusterID, exchange, daemonSubject = delegation.ClusterID, "delegated", delegation.Subject
		}
		outcome, detail := "ok", ""
		if err != nil {
			outcome, detail = "error", err.Error()
		} else if r, ok := result.(*mcp.CallToolResult); ok && r.IsError {
			outcome = "tool_error"
			for _, content := range r.Content {
				if text, ok := content.(*mcp.TextContent); ok {
					detail = text.Text
					break
				}
			}
		}
		attrs := []slog.Attr{
			slog.String("tool", auditValue(call.Params.Name)),
			slog.String("cluster_id", auditValue(target.ClusterID)),
			slog.String("issuer", identity.Issuer),
			slog.String("subject", identity.Subject),
			slog.String("client_id", identity.ClientID),
			slog.String("exchange", auditValue(exchange)),
			slog.String("daemon_subject", daemonSubject),
			slog.String("outcome", outcome),
			slog.Duration("duration", time.Since(start).Round(time.Millisecond)),
		}
		if detail != "" {
			attrs = append(attrs, slog.String("detail", auditValue(detail)))
		}
		auditLogger.LogAttrs(context.Background(), slog.LevelInfo, "mcp tool call", attrs...)
		return result, err
	}
}

func auditValue(s string) string {
	if len(s) > maxAuditValueBytes {
		return s[:maxAuditValueBytes] + "..."
	}
	return s
}
