package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/auth"
	"github.com/opensvc/om3-mcp/internal/config"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func testOAuthConfig() auth.OAuthConfig {
	return auth.OAuthConfig{ResourceURL: "https://mcp.example.test/mcp", Issuer: "https://sso.example.test/issuer/"}
}

func TestOAuthHTTPSDiscoveryInitializationAndToolBoundary(t *testing.T) {
	for _, binary := range []bool{false, true} {
		name := "in_process"
		if binary {
			name = "compiled_entrypoint"
		}
		t.Run(name, func(t *testing.T) {
			p := testutil.NewOAuthProvider(t)
			cert, key, roots := writeListenerCertificate(t)
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport}
			resource := testOAuthConfig().ResourceURL
			var origin string
			if binary {
				origin = startHTTPSBinary(t, client, cert, key, "", "OPENSVC_MCP_OAUTH_ISSUER="+p.Issuer, "OPENSVC_MCP_OAUTH_CA_FILE="+p.CAFile)
				resource = origin + "/mcp"
			} else {
				handler, err := newMCPHandler(config.Config{OAuth: auth.OAuthConfig{ResourceURL: resource, Issuer: p.Issuer, CAFile: p.CAFile}})
				if err != nil {
					t.Fatal(err)
				}
				pair, err := tls.LoadX509KeyPair(cert, key)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewUnstartedServer(handler)
				server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
				server.StartTLS()
				t.Cleanup(server.Close)
				origin = server.URL
			}
			for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
				r, err := client.Get(origin + path)
				if err != nil {
					t.Fatal(err)
				}
				var metadata map[string]any
				err = json.NewDecoder(r.Body).Decode(&metadata)
				r.Body.Close()
				if err != nil || r.StatusCode != 200 || metadata["resource"] != resource || metadata["resource_name"] != "OpenSVC Daemon MCP" || metadata["resource_documentation"] != nil || metadata["scopes_supported"] != nil {
					t.Fatalf("metadata: status=%d data=%v error=%v", r.StatusCode, metadata, err)
				}
				issuers, _ := metadata["authorization_servers"].([]any)
				if len(issuers) != 1 || issuers[0] != p.Issuer {
					t.Fatal("wrong advertised issuer")
				}
				r, err = client.Head(origin + path)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(r.Body)
				r.Body.Close()
				if r.StatusCode != 200 || len(body) != 0 {
					t.Fatal("HEAD metadata response")
				}
			}
			if p.MetadataCalls.Load() != 0 {
				t.Fatal("public discovery contacted SSO")
			}
			for _, token := range []string{"", testutil.AccessToken(t, p.Key, "cluster-a", "node-a", "alice", nil), p.Token(t, "om3-dev5", "alice", nil)} {
				r, _ := http.NewRequest("POST", origin+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
				if token != "" {
					r.Header.Set("Authorization", "Bearer "+token)
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != 401 || !strings.Contains(response.Header.Get("WWW-Authenticate"), "resource_metadata=") {
					t.Fatal("missing token or daemon-audience token accepted")
				}
			}
			token := p.Token(t, resource, "alice", nil)
			authorized := &http.Client{Transport: mcpBearerTransport{base: transport, token: token}}
			session, err := mcp.NewClient(&mcp.Implementation{Name: "oauth-test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: origin + "/mcp", HTTPClient: authorized, DisableStandaloneSSE: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { session.Close() })
			list, err := session.ListTools(t.Context(), &mcp.ListToolsParams{})
			if err != nil || len(list.Tools) == 0 {
				t.Fatalf("OAuth tool discovery failed: %v", err)
			}
			foundRefresh := false
			for _, tool := range list.Tools {
				if tool.Name == "refresh_instance_status" {
					foundRefresh = true
				}
			}
			if !foundRefresh {
				t.Error("existing tools were filtered")
			}
			for _, tool := range []string{"get_node_status", "refresh_instance_status"} {
				result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: map[string]any{"node": "dev5n1"}})
				if err != nil || !result.IsError {
					t.Fatalf("daemon call was not blocked: %v", err)
				}
				data, _ := json.Marshal(result)
				if !strings.Contains(string(data), "token exchange") || strings.Contains(string(data), token) {
					t.Error("unclear error or leaked credential")
				}
			}
			response, err := authorized.Get(origin + "/mcp/auth/whoami")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 501 {
				t.Error("legacy daemon identity bridge still exposed")
			}
			// Each HTTP request must authenticate again, regardless of prior login.
			response, err = client.Get(origin + "/mcp")
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 401 {
				t.Error("previous session bypassed authentication")
			}
			if p.KeyCalls.Load() != 1 {
				t.Errorf("unexpected JWKS fetch count %d", p.KeyCalls.Load())
			}
		})
	}
}
