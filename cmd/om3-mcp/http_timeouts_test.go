package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestHTTPServerCutsSlowClients: a client sending its request slowly
// (slowloris) is cut off once the read timeout expires.
func TestHTTPServerCutsSlowClients(t *testing.T) {
	server := newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	if server.ReadHeaderTimeout == 0 || server.ReadTimeout != readTimeout || server.WriteTimeout != writeTimeout {
		t.Fatalf("timeouts: header=%v read=%v write=%v", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout)
	}
	// The same server with a short read timeout, for the test to stay fast.
	server.ReadTimeout = 300 * time.Millisecond
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Headers announce a body that never comes in full.
	if _, err := fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: mcp\r\nContent-Length: 1000\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = io.ReadAll(conn)
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatal("the server kept the slow connection open")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("slow connection closed after %v", elapsed)
	}
}
