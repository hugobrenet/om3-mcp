package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/auth"
)

func TestGetText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/node/name/_/metrics" {
			t.Errorf("got %s %s, want GET /api/node/name/_/metrics", request.Method, request.URL.Path)
		}
		if got := request.URL.Query().Get("view"); got != "daemon" {
			t.Errorf("got view %q, want daemon", got)
		}
		if got := request.Header.Get("Accept"); got != "text/plain" {
			t.Errorf("got Accept %q, want text/plain", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer delegated-token" {
			t.Errorf("got Authorization %q, want delegated token", got)
		}
		response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(response, "process_cpu_seconds_total 12.5\n")
	}))
	defer server.Close()

	apiClient, err := New(server.URL, server.Client())
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	payload, err := apiClient.GetText(ctx, "/api/node/name/_/metrics", url.Values{"view": {"daemon"}})
	if err != nil {
		t.Fatalf("GET text: %v", err)
	}
	if got := string(payload); got != "process_cpu_seconds_total 12.5\n" {
		t.Errorf("got payload %q", got)
	}
}

func TestGetTextRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		fmt.Fprint(response, `{}`)
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetText(ctx, "/api/node/name/_/metrics", nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected content type") {
		t.Fatalf("got error %v, want content type error", err)
	}
}

func TestGetTextBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(response, strings.Repeat("x", maxTextResponseBodySize+1))
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetText(ctx, "/api/node/name/_/metrics", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got error %v, want oversized response error", err)
	}
}

func TestGetTextPreservesDaemonError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(http.StatusForbidden)
		fmt.Fprint(response, `{"title":"Forbidden","detail":"access denied"}`)
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetText(ctx, "/api/node/name/_/metrics", nil)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusForbidden || apiError.Detail != "access denied" {
		t.Fatalf("got error %#v, want bounded APIError", err)
	}
}
