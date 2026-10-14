package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testRequestID has the format of the request IDs of the AI agent.
const testRequestID = "0123456789abcdef0123456789abcdef"

func TestWithRequestIDKeepsOnlyOneWellFormedID(t *testing.T) {
	for name, tc := range map[string]struct {
		values []string
		want   string
	}{
		"absent":    {nil, ""},
		"valid":     {[]string{testRequestID}, testRequestID},
		"uppercase": {[]string{strings.ToUpper(testRequestID)}, ""},
		"short":     {[]string{testRequestID[:31]}, ""},
		"injection": {[]string{testRequestID[:30] + "\n x"}, ""},
		"duplicate": {[]string{testRequestID, testRequestID}, ""},
	} {
		var got string
		handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = requestIDFromContext(r.Context())
		}))
		request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		for _, value := range tc.values {
			request.Header.Add(requestIDHeader, value)
		}
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if got != tc.want {
			t.Fatalf("%s: request ID = %q, want %q", name, got, tc.want)
		}
	}
}
