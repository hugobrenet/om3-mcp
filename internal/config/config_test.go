package config

import (
	"strings"
	"testing"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/client"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/oauth"
)

func TestLoadDefaults(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_DAEMON_URL", "")
	t.Setenv("OPENSVC_MCP_SOCKET_PATH", "")
	t.Setenv("OPENSVC_MCP_JWT_VERIFY_KEY_FILE", "")
	t.Setenv("OPENSVC_DAEMON_TLS_CA_FILE", "")
	t.Setenv("OPENSVC_DAEMON_TLS_INSECURE", "")
	t.Setenv("OPENSVC_DAEMON_REQUEST_TIMEOUT", "")

	got, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	want := Config{
		Transport:        defaultTransport,
		DaemonURL:        defaultDaemonURL,
		SocketPath:       defaultSocketPath,
		JWTVerifyKeyFile: defaultJWTVerifyKeyFile,
		HTTP: client.HTTPOptions{
			TLSInsecure: defaultTLSInsecure,
			Timeout:     client.DefaultRequestTimeout,
		},
	}
	if got != want {
		t.Errorf("got config %#v, want %#v", got, want)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_DAEMON_URL", "https://node-a.example:1215")
	t.Setenv("OPENSVC_MCP_SOCKET_PATH", " /run/opensvc-daemon-mcp/../opensvc-daemon-mcp/custom.sock ")
	t.Setenv("OPENSVC_MCP_JWT_VERIFY_KEY_FILE", "/tmp/cluster-ca.pem")
	t.Setenv("OPENSVC_DAEMON_TLS_CA_FILE", "/tmp/ca.crt")
	t.Setenv("OPENSVC_DAEMON_TLS_INSECURE", "true")
	t.Setenv("OPENSVC_DAEMON_REQUEST_TIMEOUT", "45s")

	got, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	want := Config{
		Transport:        defaultTransport,
		DaemonURL:        "https://node-a.example:1215",
		SocketPath:       "/run/opensvc-daemon-mcp/custom.sock",
		JWTVerifyKeyFile: "/tmp/cluster-ca.pem",
		HTTP: client.HTTPOptions{
			TLSInsecure: true,
			TLSCAFile:   "/tmp/ca.crt",
			Timeout:     45 * time.Second,
		},
	}
	if got != want {
		t.Errorf("got config %#v, want %#v", got, want)
	}
}

func clearListenerEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OPENSVC_MCP_TRANSPORT", "OPENSVC_MCP_LISTEN_ADDR", "OPENSVC_MCP_TLS_CERT_FILE", "OPENSVC_MCP_TLS_KEY_FILE", "OPENSVC_MCP_PUBLIC_URL", "OPENSVC_MCP_CLUSTER_REF", "OPENSVC_MCP_CLUSTER_NAME"} {
		t.Setenv(name, "")
	}
}

func TestLoadRemoteOAuthPrototype(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_TRANSPORT", "https")
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
	t.Setenv("OPENSVC_MCP_LISTEN_ADDR", "0.0.0.0:8443")
	t.Setenv("OPENSVC_MCP_PUBLIC_URL", "https://192.0.2.10")
	t.Setenv("OPENSVC_MCP_CLUSTER_REF", "cluster-a")
	t.Setenv("OPENSVC_MCP_CLUSTER_NAME", "Example cluster")
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.OAuth != (oauth.Config{PublicURL: "https://192.0.2.10", ClusterRef: "cluster-a", ClusterName: "Example cluster"}) || got.ListenAddress != "0.0.0.0:8443" {
		t.Fatalf("public origin must be independent of bind address: %+v", got)
	}
}

func TestLoadRejectsIncompleteOrLocalOAuthConfig(t *testing.T) {
	for _, tc := range []struct{ variable, value string }{
		{"OPENSVC_MCP_PUBLIC_URL", ""},
		{"OPENSVC_MCP_CLUSTER_REF", ""},
		{"OPENSVC_MCP_CLUSTER_NAME", ""},
		{"OPENSVC_MCP_TRANSPORT", "unix"},
	} {
		t.Run(tc.variable, func(t *testing.T) {
			clearListenerEnvironment(t)
			t.Setenv("OPENSVC_MCP_TRANSPORT", "https")
			t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
			t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
			t.Setenv("OPENSVC_MCP_PUBLIC_URL", "https://192.0.2.10")
			t.Setenv("OPENSVC_MCP_CLUSTER_REF", "cluster-a")
			t.Setenv("OPENSVC_MCP_CLUSTER_NAME", "Example cluster")
			if tc.value == "unix" {
				t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "")
				t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "")
			}
			t.Setenv(tc.variable, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENSVC_MCP_PUBLIC_URL") {
				t.Fatalf("got error %v", err)
			}
		})
	}
}

func TestLoadHTTPS(t *testing.T) {
	clearListenerEnvironment(t)
	t.Setenv("OPENSVC_MCP_TRANSPORT", "https")
	t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/etc/opensvc-mcp/tls/server.crt")
	t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/etc/opensvc-mcp/tls/server.key")
	// HTTPS does not depend on the Unix socket or its directory.
	t.Setenv("OPENSVC_MCP_SOCKET_PATH", "unused-relative-path")
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
			if got.Transport != "https" || got.ListenAddress != wantAddress || got.SocketPath != "" || got.TLSCertFile != "/etc/opensvc-mcp/tls/server.crt" || got.TLSKeyFile != "/etc/opensvc-mcp/tls/server.key" {
				t.Fatalf("unexpected HTTPS configuration: %#v", got)
			}
		})
	}
}

func TestLoadRejectsInvalidListenerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, variable, value, wantError string
	}{
		{"plaintext", "OPENSVC_MCP_TRANSPORT", "http", "OPENSVC_MCP_TRANSPORT"},
		{"unknown transport", "OPENSVC_MCP_TRANSPORT", "tcp", "OPENSVC_MCP_TRANSPORT"},
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
			t.Setenv("OPENSVC_MCP_TRANSPORT", "https")
			t.Setenv("OPENSVC_MCP_TLS_CERT_FILE", "/tmp/server.crt")
			t.Setenv("OPENSVC_MCP_TLS_KEY_FILE", "/tmp/server.key")
			t.Setenv(tc.variable, tc.value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("Load() error = %v, want %s", err, tc.wantError)
			}
		})
	}
}

func TestLoadRejectsHTTPSSettingsInUnixMode(t *testing.T) {
	for _, name := range []string{"OPENSVC_MCP_LISTEN_ADDR", "OPENSVC_MCP_TLS_CERT_FILE", "OPENSVC_MCP_TLS_KEY_FILE"} {
		t.Run(name, func(t *testing.T) {
			clearListenerEnvironment(t)
			t.Setenv(name, "configured")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENSVC_MCP_TRANSPORT=https") {
				t.Fatalf("Load() error = %v, want explicit HTTPS transport requirement", err)
			}
		})
	}
}

func TestLoadRejectsInvalidUnixSocketPath(t *testing.T) {
	for _, socketPath := range []string{"mcp.sock", "/", "/" + strings.Repeat("a", maximumUnixPathBytes)} {
		t.Run(socketPath, func(t *testing.T) {
			t.Setenv("OPENSVC_MCP_SOCKET_PATH", socketPath)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENSVC_MCP_SOCKET_PATH") {
				t.Fatalf("Load() error = %v, want Unix socket path error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidTLSInsecure(t *testing.T) {
	t.Setenv("OPENSVC_DAEMON_TLS_INSECURE", "sometimes")

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "OPENSVC_DAEMON_TLS_INSECURE") {
		t.Fatalf("got error %q, want variable name", err)
	}
}

func TestLoadRejectsInvalidDaemonRequestTimeout(t *testing.T) {
	for _, value := range []string{"invalid", "0s", "500ms", "2m1s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("OPENSVC_DAEMON_REQUEST_TIMEOUT", value)
			_, err := Load()
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			if !strings.Contains(err.Error(), "OPENSVC_DAEMON_REQUEST_TIMEOUT") {
				t.Fatalf("got error %q, want variable name", err)
			}
		})
	}
}
