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

func TestGetFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/cluster/config/file" {
			t.Errorf("got %s %s, want GET /api/cluster/config/file", request.Method, request.URL.Path)
		}
		if got := request.URL.Query().Get("redact-secrets"); got != "true" {
			t.Errorf("got redact-secrets %q, want true", got)
		}
		if got := request.Header.Get("Accept"); got != "application/octet-stream" {
			t.Errorf("got Accept %q, want application/octet-stream", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer delegated-token" {
			t.Errorf("got Authorization %q, want delegated token", got)
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(response, "[cluster]\nname = prod\n")
	}))
	defer server.Close()

	apiClient, err := New(server.URL, server.Client())
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	payload, err := apiClient.GetFile(ctx, "/api/cluster/config/file", url.Values{"redact-secrets": {"true"}})
	if err != nil {
		t.Fatalf("GET file: %v", err)
	}
	if got := string(payload); got != "[cluster]\nname = prod\n" {
		t.Errorf("got payload %q", got)
	}
}

func TestGetFileRejectsUnexpectedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(response, "config")
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetFile(ctx, "/api/cluster/config/file", nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected content type") {
		t.Fatalf("got error %v, want content type error", err)
	}
}

func TestGetFileBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(response, strings.Repeat("x", maxFileResponseBodySize+1))
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetFile(ctx, "/api/cluster/config/file", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got error %v, want oversized response error", err)
	}
}

func TestGetFilePreservesDaemonError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(http.StatusForbidden)
		fmt.Fprint(response, `{"title":"Forbidden","detail":"need one of [root] grant"}`)
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, server.Client())
	ctx := auth.WithBearerToken(context.Background(), "delegated-token")
	_, err := apiClient.GetFile(ctx, "/api/cluster/config/file", nil)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusForbidden || apiError.Detail != "need one of [root] grant" {
		t.Fatalf("got error %#v, want bounded APIError", err)
	}
}
