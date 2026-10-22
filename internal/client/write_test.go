package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestPatchJSONSendsTheQueryAndGivesTheHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPatch || request.URL.Path != "/api/object/path/test/svc/web/config" ||
			request.URL.Query()["set"][1] != "env.b+=2" || request.ContentLength > 0 {
			t.Errorf("got %s %s, %d bytes", request.Method, request.URL, request.ContentLength)
		}
		response.Header().Set("OM-Last-Modified", "2026-10-22T12:00:00Z")
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"is_changed":true}`)
	}))
	defer server.Close()
	api, err := New(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		IsChanged bool `json:"is_changed"`
	}
	header, err := api.PatchJSON(context.Background(), "/api/object/path/test/svc/web/config", url.Values{"set": {"env.a=1", "env.b+=2"}}, &output)
	if err != nil || !output.IsChanged || header.Get("OM-Last-Modified") != "2026-10-22T12:00:00Z" {
		t.Fatalf("got %+v %v %v", output, header, err)
	}
}

func TestPatchJSONGivesTheProblemAndItsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("OM-Last-Modified", "2026-10-22T12:00:00Z")
		response.Header().Set("Content-Type", "application/problem+json")
		response.WriteHeader(http.StatusRequestTimeout)
		_, _ = io.WriteString(response, `{"title":"Configuration still propagating","detail":"has not reached n2 before the wait expired"}`)
	}))
	defer server.Close()
	api, _ := New(server.URL, server.Client())
	header, err := api.PatchJSON(context.Background(), "/config", url.Values{"wait": {"5s"}}, nil)
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.HTTPStatus() != http.StatusRequestTimeout || apiError.ProblemDetail() != "has not reached n2 before the wait expired" ||
		header.Get("OM-Last-Modified") == "" {
		t.Fatalf("got %v %v", header, err)
	}
}

// The wait of a request is held under the timeout of the client, so that
// the answer of a done write arrives before the client gives up on it.
func TestWaitIsHeldUnderTheTimeoutOfTheClient(t *testing.T) {
	for _, tc := range []struct {
		timeout time.Duration
		wait    string
		want    string
	}{
		{20 * time.Second, "5s", "5s"},
		{20 * time.Second, "120s", "17s"},
		{3 * time.Second, "10s", ""},
		{0, "120s", "120s"},
	} {
		api, _ := New("https://daemon.example", &http.Client{Timeout: tc.timeout})
		if got := api.boundWait(url.Values{"wait": {tc.wait}, "set": {"env.a=1"}}); got.Get("wait") != tc.want || got.Get("set") != "env.a=1" {
			t.Errorf("timeout %s wait %s: %v", tc.timeout, tc.wait, got)
		}
	}
}

func TestPostFileSendsTheFileAndExpectsNoContent(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusOK, http.StatusConflict} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/octet-stream" || string(body) != "[DEFAULT]\n" ||
				request.URL.Query().Get("wait_local") != "true" {
				t.Errorf("got %s %s %q", request.Method, request.Header.Get("Content-Type"), body)
			}
			response.Header().Set("OM-Last-Modified", "2026-10-22T12:00:00Z")
			response.WriteHeader(status)
		}))
		api, _ := New(server.URL, server.Client())
		header, err := api.PostFile(context.Background(), "/api/object/path/test/svc/web/config/file", url.Values{"wait_local": {"true"}}, []byte("[DEFAULT]\n"))
		server.Close()
		if (status == http.StatusNoContent) != (err == nil) || header.Get("OM-Last-Modified") == "" {
			t.Errorf("%d: %v %v", status, header, err)
		}
	}
}
