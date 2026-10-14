package testutil

import (
	"go.yaml.in/yaml/v2"
	"os"
	"path/filepath"
	"testing"
)

func WriteYAML(t testing.TB, doc any) string {
	t.Helper()
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
