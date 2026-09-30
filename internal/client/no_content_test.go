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
)

func TestGetNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/node/name/node-b/ping" {
			t.Errorf("got %s %s, want GET /api/node/name/node-b/ping", request.Method, request.URL.Path)
		}
		if got := request.URL.Query().Get("probe"); got != "true" {
			t.Errorf("got probe query %q, want true", got)
		}
		if got := request.Header.Get("Accept"); got != "*/*" {
			t.Errorf("got Accept %q, want */*", got)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-daemon-token" {
			t.Errorf("got Authorization %q, want daemon token", got)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	apiClient, err := New(server.URL, daemonTestHTTPClient(server.Client(), "test-daemon-token"))
	if err != nil {
		t.Fatalf("create API client: %v", err)
	}
	ctx := context.Background()
	if err := apiClient.GetNoContent(ctx, "/api/node/name/node-b/ping", url.Values{"probe": {"true"}}); err != nil {
		t.Fatalf("GET no content: %v", err)
	}
}

func TestGetNoContentRejectsUnexpectedSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, daemonTestHTTPClient(server.Client(), "test-daemon-token"))
	ctx := context.Background()
	err := apiClient.GetNoContent(ctx, "/api/node/name/node-b/ping", nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 200") {
		t.Fatalf("got error %v, want unexpected status error", err)
	}
}

func TestGetNoContentPreservesDaemonError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(response, `{"title":"Request peer","detail":"node-b: connection refused"}`)
	}))
	defer server.Close()

	apiClient, _ := New(server.URL, daemonTestHTTPClient(server.Client(), "test-daemon-token"))
	ctx := context.Background()
	err := apiClient.GetNoContent(ctx, "/api/node/name/node-b/ping", nil)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusInternalServerError || apiError.Title != "Request peer" || apiError.Detail != "node-b: connection refused" {
		t.Fatalf("got error %#v, want bounded APIError", err)
	}
}
