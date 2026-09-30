package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/config"
	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

func TestHTTPSBinaryDiscoveryRegistrationAndLogin(t *testing.T) {
	certFile, keyFile, roots := writeListenerCertificate(t)
	var daemonCalls atomic.Int32
	var daemonConnections atomic.Int32
	daemon := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		daemonCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	daemon.Listener = &countingListener{Listener: daemon.Listener, connections: &daemonConnections}
	daemon.StartTLS()
	t.Cleanup(daemon.Close)
	clusterFile := testutil.WriteClusters(t, map[string]string{"cluster-a": "Example cluster", "cluster-b": "Second example cluster"})
	clusterData, err := os.ReadFile(clusterFile)
	if err != nil {
		t.Fatal(err)
	}
	clusterData = []byte(strings.ReplaceAll(string(clusterData), "https://192.0.2.20:1215", daemon.URL))
	if err := os.WriteFile(clusterFile, clusterData, 0o600); err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	t.Cleanup(tr.CloseIdleConnections)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Transport: tr, Jar: jar, Timeout: 2 * time.Second}
	origin := startHTTPSBinary(t, c, certFile, keyFile, clusterFile)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, bearer := range []string{"", "Bearer synthetic-daemon-token"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", bearer)
		response, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || !strings.Contains(response.Header.Get("WWW-Authenticate"), origin+"/.well-known/oauth-protected-resource/mcp") {
			t.Fatal("HTTPS binary did not require OAuth authorization")
		}
	}
	resource, err := oauthex.GetProtectedResourceMetadata(ctx, origin+"/.well-known/oauth-protected-resource/mcp", origin+"/mcp", c)
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.AuthorizationServers) != 1 || resource.AuthorizationServers[0] != origin {
		t.Fatalf("resource metadata: %+v", resource)
	}
	meta, err := oauthex.GetAuthServerMeta(ctx, origin+"/.well-known/oauth-authorization-server", origin, c)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := oauthex.RegisterClient(ctx, meta.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{ClientName: "Example agent", RedirectURIs: []string{"http://127.0.0.1/callback/client-a"}, TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}}, c)
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"client_id": {registration.ClientID}, "redirect_uri": {"http://127.0.0.1:5432/callback/client-a"}, "response_type": {"code"}, "resource": {resource.Resource}, "scope": {"mcp:access"}, "state": {"example-state"}, "code_challenge_method": {"S256"}, "code_challenge": {"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}
	response, err := c.Get(meta.AuthorizationEndpoint + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || response.Request.URL.String() != origin+"/login" || !strings.Contains(string(page), "Example agent") || !strings.Contains(string(page), "Example cluster") || !strings.Contains(string(page), "<fieldset>") {
		t.Fatalf("TLS login journey failed: status=%d error=%v", response.StatusCode, err)
	}
	if !strings.Contains(string(page), `<select id="cluster" name="cluster_ref" required>`) || !strings.Contains(string(page), `<option value="cluster-b">Second example cluster</option>`) {
		t.Fatal("TLS login journey did not expose the configured cluster choices")
	}
	if daemonCalls.Load() != 0 || daemonConnections.Load() != 0 {
		t.Fatal("catalogue loading or login form contacted the daemon")
	}
}

type countingListener struct {
	net.Listener
	connections *atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.connections.Add(1)
	}
	return conn, err
}

func TestHTTPSListenerRequiresTrustedTLSAndBlocksRemoteMCP(t *testing.T) {
	certFile, keyFile, roots := writeListenerCertificate(t)
	cfg := config.Config{
		ListenAddress: "127.0.0.1:0",
		TLSCertFile:   certFile, TLSKeyFile: keyFile,
	}
	handler, err := newMCPHandler(cfg)
	if err != nil {
		t.Fatalf("remote handler must not require local OpenSVC credentials: %v", err)
	}
	listener, tlsConfig, err := listenMCP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: handler, TLSConfig: tlsConfig, ReadHeaderTimeout: time.Second,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeTLS(listener, "", "") }()
	t.Cleanup(func() {
		if err := shutdownHTTPServer(server, time.Second); err != nil {
			t.Errorf("shutdown HTTPS server: %v", err)
		}
		if err := <-serveDone; err != http.ErrServerClosed {
			t.Errorf("serve HTTPS: %v", err)
		}
	})
	endpoint := "https://" + listener.Addr().String() + "/mcp"
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for _, bearer := range []string{"", "Bearer untrusted-token"} {
		request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", bearer)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("request over verified TLS: %v", err)
		}
		var problem struct {
			Status int    `json:"status"`
			Detail string `json:"detail"`
		}
		err = json.NewDecoder(response.Body).Decode(&problem)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != 503 || problem.Status != 503 || !strings.Contains(problem.Detail, "authorization") {
			t.Fatalf("remote MCP response: status=%d problem=%+v error=%v", response.StatusCode, problem, err)
		}
		if response.Header.Get("Content-Type") != "application/problem+json" || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected response headers: %v", response.Header)
		}
	}
	for _, tc := range []struct {
		name string
		tls  *tls.Config
	}{
		{"untrusted CA", &tls.Config{MinVersion: tls.VersionTLS12}},
		{"wrong certificate identity", &tls.Config{RootCAs: roots, ServerName: "wrong.example", MinVersion: tls.VersionTLS12}},
		{"TLS 1.1", &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &http.Transport{TLSClientConfig: tc.tls}
			defer tr.CloseIdleConnections()
			c := &http.Client{Transport: tr, Timeout: 2 * time.Second}
			if response, err := c.Get(endpoint); err == nil {
				_ = response.Body.Close()
				t.Fatal("expected TLS rejection")
			}
		})
	}
	plain := &http.Client{Timeout: 2 * time.Second}
	response, err := plain.Post("http://"+listener.Addr().String()+"/mcp", "application/json", strings.NewReader("{}"))
	if err == nil {
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("plaintext request was not rejected: %d", response.StatusCode)
		}
	}
}

func TestHTTPSListenerRejectsInvalidMaterialBeforeBinding(t *testing.T) {
	certFile, keyFile, _ := writeListenerCertificate(t)
	_, otherKey, _ := writeListenerCertificate(t)
	malformed := filepath.Join(t.TempDir(), "malformed.pem")
	if err := os.WriteFile(malformed, []byte("not PEM"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cert, key string }{
		{"missing certificate", certFile + ".missing", keyFile},
		{"missing key", certFile, keyFile + ".missing"},
		{"malformed certificate", malformed, keyFile},
		{"malformed key", certFile, malformed},
		{"mismatched key", certFile, otherKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An occupied address distinguishes a certificate failure from a bind failure.
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			listener, _, err := listenMCP(config.Config{ListenAddress: occupied.Addr().String(), TLSCertFile: tc.cert, TLSKeyFile: tc.key})
			if listener != nil {
				_ = listener.Close()
				t.Fatal("invalid material opened a listener")
			}
			if err == nil || !strings.Contains(err.Error(), "load MCP TLS") {
				t.Fatalf("got error %v, want TLS material error", err)
			}
		})
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	listener, _, err := listenMCP(config.Config{ListenAddress: occupied.Addr().String(), TLSCertFile: certFile, TLSKeyFile: keyFile})
	if listener != nil {
		_ = listener.Close()
		t.Fatal("occupied address opened a listener")
	}
	if err == nil || !strings.Contains(err.Error(), "listen on MCP TCP") {
		t.Fatalf("got error %v, want bind error", err)
	}
}

func writeListenerCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("load generated certificate into trust store")
	}
	return certFile, keyFile, roots
}

// Start the compiled entrypoint with only the new HTTPS configuration. Keep
// process output in a file so failed-start diagnostics do not race with writes.
func startHTTPSBinary(t *testing.T, c *http.Client, certFile, keyFile, clusterFile string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	binary := filepath.Join(t.TempDir(), "opensvc-daemon-mcp")
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build MCP: %v\n%s", err, output)
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	origin := "https://" + address
	command := exec.CommandContext(ctx, binary)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "OPENSVC_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env,
		"OPENSVC_MCP_LISTEN_ADDR="+address,
		"OPENSVC_MCP_TLS_CERT_FILE="+certFile,
		"OPENSVC_MCP_TLS_KEY_FILE="+keyFile,
		"OPENSVC_MCP_PUBLIC_URL="+origin,
		"OPENSVC_MCP_CLUSTER_CONFIG_FILE="+clusterFile,
	)
	logFile, err := os.Create(filepath.Join(t.TempDir(), "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("MCP exit: %v", err)
			}
		case <-time.After(3 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("MCP did not shut down on SIGTERM")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := c.Get(origin + "/.well-known/oauth-authorization-server")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return origin
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	output, _ := os.ReadFile(logFile.Name())
	t.Fatalf("HTTPS binary did not start: %s", output)
	return ""
}
