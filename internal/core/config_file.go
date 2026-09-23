package core

import (
	"context"
	"fmt"
	"net/url"
	"unicode/utf8"
)

const maxConfigFileOutputBytes = 64 << 10

type ClusterConfig struct {
	Provenance         Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Content            string     `json:"content" jsonschema:"bounded OpenSVC cluster configuration file content returned by the daemon"`
	SizeBytes          int        `json:"size_bytes" jsonschema:"complete redacted configuration file size in bytes before MCP output truncation"`
	ReturnedBytes      int        `json:"returned_bytes" jsonschema:"number of configuration content bytes included in this result"`
	Truncated          bool       `json:"truncated" jsonschema:"whether configuration content was omitted after the 65536-byte output limit"`
	RedactionRequested bool       `json:"redaction_requested" jsonschema:"whether the MCP required daemon-side secret redaction for this request; always true"`
}

func (s *Service) GetClusterConfig(ctx context.Context) (ClusterConfig, error) {
	payload, err := s.getRedactedConfigFile(ctx, "/api/cluster/config/file")
	if err != nil {
		return ClusterConfig{}, fmt.Errorf("get cluster config: %w", err)
	}
	content, truncated, err := boundConfigFile(payload)
	if err != nil {
		return ClusterConfig{}, fmt.Errorf("get cluster config: %w", err)
	}
	return ClusterConfig{
		Provenance: s.newProvenance(), Content: content, SizeBytes: len(payload),
		ReturnedBytes: len(content), Truncated: truncated, RedactionRequested: true,
	}, nil
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
