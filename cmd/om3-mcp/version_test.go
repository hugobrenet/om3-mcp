package main

import "testing"

func TestBuildVersionIsTheOneTheBuildSet(t *testing.T) {
	defer func(previous string) { version = previous }(version)
	if buildVersion() == "" {
		t.Fatal("empty version")
	}
	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Fatalf("version = %q", got)
	}
}
