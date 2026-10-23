package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxDRBDConfigBytes bounds the text of a DRBD resource file returned.
const maxDRBDConfigBytes = 64 << 10

// drbdSharedSecret is a shared-secret statement, the secret the peers of a
// DRBD resource authenticate with: a file written by hand may hold one.
var drbdSharedSecret = regexp.MustCompile(`(?m)^(\s*shared-secret\s+)("[^"\n]*"|[^\s;]+)`)

type NodeDRBDConfig struct {
	Provenance    Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node          string     `json:"node" jsonschema:"the node read"`
	Name          string     `json:"name" jsonschema:"the DRBD resource, its file /etc/drbd.d/<name>.res"`
	Content       string     `json:"content,omitempty" jsonschema:"the text of the resource file, its shared-secret values masked, at most 64 KiB; untrusted text"`
	Binary        bool       `json:"binary,omitempty" jsonschema:"whether the file is not UTF-8 text: it is not returned, its size is"`
	SizeBytes     int        `json:"size_bytes" jsonschema:"the size of the file, in bytes"`
	Truncated     bool       `json:"truncated" jsonschema:"whether content holds only the beginning of the file"`
	SecretsMasked int        `json:"secrets_masked,omitempty" jsonschema:"the number of shared-secret values replaced by <masked>"`
}

type daemonDRBDConfig struct {
	Data []byte `json:"data"`
}

// GetNodeDRBDConfig reads the file of one DRBD resource on one node,
// /etc/drbd.d/<name>.res, the daemon relaying the read to it. The files om3
// writes hold no shared-secret; one written by hand may, and its value is
// masked.
func (s *Service) GetNodeDRBDConfig(ctx context.Context, node string, name string) (NodeDRBDConfig, error) {
	if err := validateNodeTarget(node); err != nil {
		return NodeDRBDConfig{}, err
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || utf8.RuneCountInString(name) > 255 || containsControl(name) {
		return NodeDRBDConfig{}, fmt.Errorf("name must be one DRBD resource name, the base name of its file in /etc/drbd.d without .res, without / or \\")
	}
	var response daemonDRBDConfig
	if err := s.client.GetJSON(ctx, fmt.Sprintf("/api/node/name/%s/drbd/config", node), url.Values{"name": {name}}, &response); err != nil {
		return NodeDRBDConfig{}, fmt.Errorf("get the DRBD resource %s of %s: %w", name, node, err)
	}
	result := NodeDRBDConfig{Node: node, Name: name, SizeBytes: len(response.Data)}
	if !utf8.Valid(response.Data) {
		result.Binary = true
	} else {
		masked := drbdSharedSecret.ReplaceAllStringFunc(string(response.Data), func(match string) string {
			result.SecretsMasked++
			return drbdSharedSecret.ReplaceAllString(match, `${1}"<masked>"`)
		})
		result.Content, result.Truncated = cutUTF8(masked, maxDRBDConfigBytes)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}
