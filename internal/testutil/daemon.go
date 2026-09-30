package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.yaml.in/yaml/v2"
)

type Daemon struct {
	Server *httptest.Server
	CAFile string
	CAPEM  []byte
	CAKey  *rsa.PrivateKey
}

// NewDaemon creates TLS and JWT trust material in memory for a fake daemon.
// Only its public CA is written to a temporary file.
func NewDaemon(t testing.TB, handler http.Handler) Daemon {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Example CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Example daemon"}, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: leafKey}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	path := filepath.Join(t.TempDir(), "example-ca.pem")
	if err := os.WriteFile(path, public, 0o600); err != nil {
		t.Fatal(err)
	}
	return Daemon{Server: server, CAFile: path, CAPEM: public, CAKey: key}
}

func WriteTarget(t testing.TB, name, id, ca string, endpoints []string) string {
	t.Helper()
	data, err := yaml.Marshal(map[string]any{"version": 1, "clusters": map[string]any{"cluster-a": map[string]any{"name": name, "expected_cluster_id": id, "endpoints": endpoints, "tls": map[string]string{"ca_file": ca}, "request_timeout": "2s"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clusters.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
