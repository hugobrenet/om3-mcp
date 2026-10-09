package core

import (
	"context"
	"fmt"
)

type ClusterConfig struct {
	Provenance         Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Content            string     `json:"content" jsonschema:"bounded OpenSVC cluster configuration file content returned by the daemon"`
	SizeBytes          int        `json:"size_bytes" jsonschema:"complete redacted configuration file size in bytes before MCP output truncation"`
	ReturnedBytes      int        `json:"returned_bytes" jsonschema:"number of configuration content bytes included in this result"`
	Truncated          bool       `json:"truncated" jsonschema:"whether configuration content was omitted after the 65536-byte output limit"`
	RedactionRequested bool       `json:"redaction_requested" jsonschema:"whether the MCP required daemon-side secret redaction for this request; always true"`
}

func (s *Service) GetClusterConfig(ctx context.Context) (ClusterConfig, error) {
	file, err := s.readConfigFile(ctx, "/api/cluster/config/file")
	if err != nil {
		return ClusterConfig{}, fmt.Errorf("get cluster config: %w", err)
	}
	return ClusterConfig{
		Provenance: s.newProvenance(), Content: file.content, SizeBytes: file.sizeBytes,
		ReturnedBytes: len(file.content), Truncated: file.truncated, RedactionRequested: true,
	}, nil
}
