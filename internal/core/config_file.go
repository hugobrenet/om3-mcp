package core

import (
	"context"
	"fmt"
	"net/url"
	"unicode/utf8"
)

const maxConfigFileOutputBytes = 64 << 10

// configFile is a daemon configuration file read with secret redaction and
// bounded for the MCP output. The cluster and node config results share it.
type configFile struct {
	content   string
	sizeBytes int
	truncated bool
}

// readConfigFile requests the configuration file at endpoint with daemon-side
// secret redaction, and bounds it to valid UTF-8 within the output limit.
func (s *Service) readConfigFile(ctx context.Context, endpoint string) (configFile, error) {
	payload, err := s.getRedactedConfigFile(ctx, endpoint)
	if err != nil {
		return configFile{}, err
	}
	content, truncated, err := boundConfigFile(payload)
	if err != nil {
		return configFile{}, err
	}
	return configFile{content: content, sizeBytes: len(payload), truncated: truncated}, nil
}

func (s *Service) getRedactedConfigFile(ctx context.Context, endpoint string) ([]byte, error) {
	getter, ok := s.client.(FileGetter)
	if !ok {
		return nil, fmt.Errorf("OpenSVC daemon client does not support file requests")
	}
	return getter.GetFile(ctx, endpoint, url.Values{"redact-secrets": {"true"}})
}

func boundConfigFile(payload []byte) (string, bool, error) {
	if !utf8.Valid(payload) {
		return "", false, fmt.Errorf("OpenSVC daemon returned a configuration file that is not valid UTF-8")
	}
	if len(payload) <= maxConfigFileOutputBytes {
		return string(payload), false, nil
	}
	end := maxConfigFileOutputBytes
	for end > 0 && !utf8.Valid(payload[:end]) {
		end--
	}
	return string(payload[:end]), true, nil
}
