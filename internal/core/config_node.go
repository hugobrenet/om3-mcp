package core

import (
	"context"
	"fmt"
)

type NodeConfig struct {
	Provenance         Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node               string     `json:"node" jsonschema:"the exact requested OpenSVC node name"`
	Content            string     `json:"content" jsonschema:"bounded OpenSVC node configuration file content returned by the daemon"`
	SizeBytes          int        `json:"size_bytes" jsonschema:"complete redacted configuration file size in bytes before MCP output truncation"`
	ReturnedBytes      int        `json:"returned_bytes" jsonschema:"number of configuration content bytes included in this result"`
	Truncated          bool       `json:"truncated" jsonschema:"whether configuration content was omitted after the 65536-byte output limit"`
	RedactionRequested bool       `json:"redaction_requested" jsonschema:"whether the MCP required daemon-side secret redaction for this request; always true"`
}

func (s *Service) GetNodeConfig(ctx context.Context, node string) (NodeConfig, error) {
	if err := validateNodeTarget(node); err != nil {
		return NodeConfig{}, err
	}
	file, err := s.readConfigFile(ctx, fmt.Sprintf("/api/node/name/%s/config/file", node))
	if err != nil {
		return NodeConfig{}, fmt.Errorf("get node config: %w", err)
	}
	return NodeConfig{
		Provenance: s.newProvenance(), Node: node, Content: file.content, SizeBytes: file.sizeBytes,
		ReturnedBytes: len(file.content), Truncated: file.truncated, RedactionRequested: true,
	}, nil
}
