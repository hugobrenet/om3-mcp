package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolsHandlerRefusesCrossOriginBrowserRequests(t *testing.T) {
	handler, err := newConfiguredToolsHandler(nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for fetchSite, want := range map[string]int{"cross-site": http.StatusForbidden, "same-origin": http.StatusOK, "": http.StatusOK} {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if fetchSite != "" {
			request.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("Sec-Fetch-Site %q: status = %d, want %d", fetchSite, response.Code, want)
		}
	}
}
