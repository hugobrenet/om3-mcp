package auth

import "context"

// CallTrace collects credential-free audit facts about one MCP tool call. It
// is written only by server-side code and never carries tokens or secrets.
type CallTrace struct {
	// Exchange is empty when no exchange was attempted, "ok" on success, or the
	// bounded error message also returned to the client.
	Exchange string
	// DaemonSubject is the exchanged token subject, for daemon log correlation.
	DaemonSubject string
}

type callTraceKey struct{}

func WithCallTrace(ctx context.Context) (context.Context, *CallTrace) {
	trace := &CallTrace{}
	return context.WithValue(ctx, callTraceKey{}, trace), trace
}
