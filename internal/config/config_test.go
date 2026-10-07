package config

import (
	"strings"
	"testing"

	"github.com/opensvc/om3-mcp/internal/testutil"
)

func clearListenerEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OPENSVC_MCP_LISTEN_ADDR", "OPENSVC_MCP_TLS_CERT_FILE", "OPENSVC_MCP_TLS_KEY_FILE", "OPENSVC_MCP_CLUSTER_CONFIG_FILE", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "OPENSVC_MCP_OAUTH_RESOURCE_NAME", "OPENSVC_MCP_OAUTH_ISSUER", "OPENSVC_MCP_OAUTH_CA_FILE"} {
		t.Setenv(name, "")
	}
	t.Setenv("OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.test/mcp")
	t.Setenv("OPENSVC_MCP_OAUTH_ISSUER", "https://sso.example.test/issuer/")
}

func TestLoadOAuthConfiguration(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
	t.Setenv("OPENSVC_MCP_LISTEN_ADDR", "0.0.0.0:8443")
	t.Setenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE", testutil.WriteClusters(t, map[string]string{"cluster-a": "Example cluster"}))
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Clusters.Len() != 1 || got.ListenAddress != "0.0.0.0:8443" {
		t.Fatalf("catalogue must be loaded independently of bind address: %+v", got)
	}
}

func TestLoadOAuthDoesNotRequireCatalogue(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
	if cfg, err := Load(); err != nil || cfg.Clusters != nil {
		t.Fatalf("OAuth bootstrap should not require a daemon catalogue: %v", err)
	}
}

func TestLoadRejectsInvalidOAuthConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, variable, value string }{
		{"missing resource", "OPENSVC_MCP_OAUTH_RESOURCE_URL", ""},
		{"HTTP resource", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "http://mcp.example.test/mcp"},
		{"resource credentials", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://user:password@mcp.example.test/mcp"},
		{"wrong resource path", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.test/other"},
		{"resource query", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.test/mcp?x=1"},
		{"resource fragment", "OPENSVC_MCP_OAUTH_RESOURCE_URL", "https://mcp.example.test/mcp#"},
		{"invalid resource name", "OPENSVC_MCP_OAUTH_RESOURCE_NAME", "MCP\nname"},
		{"missing issuer", "OPENSVC_MCP_OAUTH_ISSUER", ""},
		{"HTTP issuer", "OPENSVC_MCP_OAUTH_ISSUER", "http://sso.example.test"},
		{"issuer credentials", "OPENSVC_MCP_OAUTH_ISSUER", "https://user:password@sso.example.test"},
		{"issuer query", "OPENSVC_MCP_OAUTH_ISSUER", "https://sso.example.test?"},
		{"relative trust file", "OPENSVC_MCP_OAUTH_CA_FILE", "ca.pem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearListenerEnvironment(t)
			t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
			t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
			t.Setenv(tc.variable, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENSVC_MCP_OAUTH") {
				t.Fatalf("invalid OAuth configuration accepted: %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidClusterConfigBeforeListening(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
	for _, path := range []string{"relative.yaml", "/nonexistent/clusters.yaml", t.TempDir()} {
		t.Setenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE", path)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENSVC_MCP_CLUSTER_CONFIG_FILE") {
			t.Fatalf("invalid file %q accepted: %v", path, err)
		}
	}
}

func TestLoadHTTPS(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_CLUSTER_CONFIG_FILE", testutil.WriteClusters(t, map[string]string{"cluster-a": "Example cluster"}))
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-mcp/tls/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-mcp/tls/server.key")
	for _, address := range []string{"", "192.0.2.10:443", "[::1]:8443"} {
		t.Run(address, func(t *testing.T) {
			t.Setenv("OPENSVC_MCP_LISTEN_ADDR", address)
			got, err := Load()
			if err != nil {
				t.Fatalf("load HTTPS configuration: %v", err)
			}
			wantAddress := address
			if wantAddress == "" {
				wantAddress = defaultListenAddress
			}
			if got.ListenAddress != wantAddress || got.TLSCertFile != "/etc/opensvc-mcp/tls/server.crt" || got.TLSKeyFile != "/etc/opensvc-mcp/tls/server.key" {
				t.Fatalf("unexpected HTTPS configuration: %#v", got)
			}
		})
	}
}

func TestLoadRejectsInvalidListenerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, variable, value, wantError string
	}{
		{"missing certificate", "OPENSVC_MCP_TLS_CERT_FILE", "", "OPENSVC_MCP_TLS_CERT_FILE"},
		{"missing key", "OPENSVC_MCP_TLS_KEY_FILE", "", "OPENSVC_MCP_TLS_KEY_FILE"},
		{"relative certificate", "OPENSVC_MCP_TLS_CERT_FILE", "server.crt", "OPENSVC_MCP_TLS_CERT_FILE"},
		{"relative key", "OPENSVC_MCP_TLS_KEY_FILE", "server.key", "OPENSVC_MCP_TLS_KEY_FILE"},
		{"URL as address", "OPENSVC_MCP_LISTEN_ADDR", "https://192.0.2.10:443", "OPENSVC_MCP_LISTEN_ADDR"},
		{"missing port", "OPENSVC_MCP_LISTEN_ADDR", "192.0.2.10", "OPENSVC_MCP_LISTEN_ADDR"},
		{"implicit host", "OPENSVC_MCP_LISTEN_ADDR", ":443", "OPENSVC_MCP_LISTEN_ADDR"},
		{"hostname", "OPENSVC_MCP_LISTEN_ADDR", "localhost:443", "OPENSVC_MCP_LISTEN_ADDR"},
		{"ephemeral port", "OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:0", "OPENSVC_MCP_LISTEN_ADDR"},
		{"invalid port", "OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:65536", "OPENSVC_MCP_LISTEN_ADDR"},
		{"named port", "OPENSVC_MCP_LISTEN_ADDR", "127.0.0.1:https", "OPENSVC_MCP_LISTEN_ADDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearListenerEnvironment(t)
			t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
			t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
			t.Setenv(tc.variable, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Load() error = %v, want %s", err, tc.wantError)
			}
		})
	}
}
