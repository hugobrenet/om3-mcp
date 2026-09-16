package core

import "time"

const provenanceSourceOpenSVCDaemon = "opensvc_daemon"

// Provenance identifies the API source and the time the MCP finished collecting
// a successful result. ObservedAt does not assert that the underlying data is fresh.
type Provenance struct {
	Source     string `json:"source" jsonschema:"the API source of this result; opensvc_daemon identifies the OpenSVC daemon API, not necessarily the original data store"`
	ObservedAt string `json:"observed_at" jsonschema:"UTC time when the MCP finished collecting this result; this is not the OpenSVC status update time"`
}

func (s *Service) newProvenance() Provenance {
	return Provenance{
		Source:     provenanceSourceOpenSVCDaemon,
		ObservedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
}
