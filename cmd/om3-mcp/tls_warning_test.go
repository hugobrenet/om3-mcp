package main

import (
	"log"
	"strings"
	"testing"

	"github.com/opensvc/om3-mcp/internal/clusterconfig"
	"github.com/opensvc/om3-mcp/internal/testutil"
)

func TestStartupWarnsOnlyForExplicitInsecureTargets(t *testing.T) {
	path := testutil.WriteCatalog(t, map[string]any{
		"secure": map[string]any{"name": "Secure", "cluster_id": "secure-id", "endpoint": "https://192.0.2.20:1215", "request_timeout": "2s"},
		"demo":   map[string]any{"name": "Demo", "cluster_id": "demo-id", "endpoint": "https://192.0.2.21:1215", "tls": map[string]any{"insecure": true}, "request_timeout": "2s"},
	})
	catalog, err := clusterconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	warnInsecureDaemonTLS(catalog, log.New(&output, "", 0))
	warning := output.String()
	for _, message := range []string{"WARNING", "cluster=demo", "tls.insecure=true", "hostname", "JWT theft", "forged whoami", "DO NOT USE IN PRODUCTION"} {
		if !strings.Contains(warning, message) {
			t.Fatalf("missing warning %q", message)
		}
	}
	if strings.Contains(warning, "cluster=secure") || strings.Count(warning, "WARNING") != 1 {
		t.Fatal("warning not scoped to the insecure cluster")
	}
}
