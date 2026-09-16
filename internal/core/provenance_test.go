package core

import (
	"testing"
	"time"
)

func TestNewProvenanceUsesUTCCollectionTime(t *testing.T) {
	service := New(nil)
	service.now = func() time.Time {
		return time.Date(2026, time.September, 16, 12, 34, 56, 123456789, time.FixedZone("CEST", 2*60*60))
	}

	got := service.newProvenance()
	if got.Source != "opensvc_daemon" {
		t.Errorf("source = %q, want opensvc_daemon", got.Source)
	}
	if got.ObservedAt != "2026-09-16T10:34:56.123456789Z" {
		t.Errorf("observed_at = %q, want UTC collection time", got.ObservedAt)
	}
}
