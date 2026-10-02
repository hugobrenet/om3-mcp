package clusterconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/hugobrenet/opensvc-daemon-mcp/internal/testutil"
)

func TestDaemonTLSInsecureIsExplicitAndStrict(t *testing.T) {
	path := testutil.WriteTarget(t, "Example", "cluster-id", "", map[string]string{"node-a": "https://192.0.2.20:1215"})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base := string(data)
	for _, tc := range []struct {
		name, option string
		wantInsecure bool
		wantError    bool
	}{
		{name: "default"},
		{name: "explicit false", option: "false"},
		{name: "explicit true", option: "true", wantInsecure: true},
		{name: "quoted true", option: `"true"`, wantError: true},
		{name: "quoted false", option: `"false"`, wantError: true},
		{name: "number", option: "1", wantError: true},
		{name: "list", option: "[true]", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := base
			if tc.option != "" {
				config = strings.Replace(config, "    tls:\n", "    tls:\n      insecure: "+tc.option+"\n", 1)
			}
			catalog, err := Load(writeConfig(t, []byte(config)))
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid insecure flag accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := catalog.List()[0]; got.TLSInsecure != tc.wantInsecure || got.CAFile != "" || len(got.CAPEM) != 0 {
				t.Fatalf("unexpected demo TLS configuration: %+v", got)
			}
		})
	}
	withCA := strings.Replace(string(fixture(t)), "    tls:\n", "    tls:\n      insecure: true\n", 1)
	if _, err := Load(writeConfig(t, []byte(withCA))); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("insecure and ca_file accepted together: %v", err)
	}
}
