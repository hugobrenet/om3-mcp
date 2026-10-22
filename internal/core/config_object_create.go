package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxCreateObjectConfigBytes = 256 << 10

type CreateObjectOptions struct {
	Path string
	// Config is the configuration file of the object, as INI text.
	Config string
	// Set are operations applied after the configuration, as for an update.
	Set []string
	// WaitLocal holds the answer until the daemon knows the object; nil
	// means true.
	WaitLocal *bool
}

type CreatedObject struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Created    bool                   `json:"created" jsonschema:"whether the daemon created the object; always true in a result"`
	// LastModified is the time the configuration created carries.
	LastModified string `json:"last_modified,omitempty" jsonschema:"the time the daemon gives the configuration written, as OM-Last-Modified"`
	// FlaggedStopped tells that the daemon flagged the object stopped on the
	// node that created it, which it does for an object it would otherwise
	// start on its own.
	FlaggedStopped bool `json:"flagged_stopped" jsonschema:"whether the object is orchestrated ha or start, which the daemon flags stopped on the node that created it, so that it does not start before anything is asked of it"`
	// Content is the configuration file created, read again with redaction.
	Content            string `json:"content" jsonschema:"the configuration file created, read again, with the secrets redacted by the daemon"`
	SizeBytes          int    `json:"size_bytes" jsonschema:"complete redacted configuration file size in bytes before MCP output truncation"`
	Truncated          bool   `json:"truncated" jsonschema:"whether configuration content was omitted after the 65536-byte output limit"`
	RedactionRequested bool   `json:"redaction_requested" jsonschema:"whether the MCP required daemon-side secret redaction for the read; always true"`
	ContentError       string `json:"content_error,omitempty" jsonschema:"why the configuration created could not be read again; the object itself is created"`
}

// CreateObject creates one object from a configuration file. The daemon
// takes it as copied from another object: the id and what the
// configuration recorded of the things created for that object are left
// behind, and the object gets an id of its own. It refuses an object that
// exists, and validates the configuration as for an update.
func (s *Service) CreateObject(ctx context.Context, options CreateObjectOptions) (CreatedObject, error) {
	reference, err := validateConfigWritePath(options.Path)
	if err != nil {
		return CreatedObject{}, err
	}
	switch {
	case strings.TrimSpace(options.Config) == "":
		return CreatedObject{}, fmt.Errorf("config is required: the configuration file of the object, as INI text")
	case len(options.Config) > maxCreateObjectConfigBytes:
		return CreatedObject{}, fmt.Errorf("config exceeds %d bytes", maxCreateObjectConfigBytes)
	case !utf8.ValidString(options.Config):
		return CreatedObject{}, fmt.Errorf("config is not valid UTF-8")
	case len(options.Set) > maxConfigOperations:
		return CreatedObject{}, fmt.Errorf("at most %d set operations are accepted", maxConfigOperations)
	}
	sets, _, err := validateConfigSets(options.Set)
	if err != nil {
		return CreatedObject{}, err
	}
	poster, ok := s.client.(FilePoster)
	if !ok {
		return CreatedObject{}, fmt.Errorf("OpenSVC daemon client does not support file requests")
	}
	waitLocal := options.WaitLocal == nil || *options.WaitLocal
	query := url.Values{}
	for _, operation := range sets {
		query.Add("set", operation)
	}
	if waitLocal {
		query.Set("wait_local", "true")
	}
	endpoint := fmt.Sprintf("/api/object/path/%s/%s/%s/config/file", reference.Namespace, reference.Kind, reference.Name)
	header, err := poster.PostFile(ctx, endpoint, query, []byte(options.Config))
	if err != nil {
		// The daemon refuses an object that exists, naming the PUT method: the
		// tool that changes its configuration is the update.
		var problem daemonProblem
		if errors.As(err, &problem) && problem.HTTPStatus() == http.StatusConflict && strings.Contains(problem.ProblemDetail(), "PUT method") {
			return CreatedObject{}, fmt.Errorf("create object: %s exists; use update_object_config to change its configuration: %w", reference.Path, err)
		}
		return CreatedObject{}, fmt.Errorf("create object: %w", err)
	}
	result := CreatedObject{Object: reference, Created: true, RedactionRequested: true, LastModified: header.Get(configLastModifiedHeader)}
	file, err := s.readConfigFile(ctx, endpoint)
	if err != nil {
		result.ContentError = err.Error()
	} else {
		result.Content, result.SizeBytes, result.Truncated = file.content, file.sizeBytes, file.truncated
		result.FlaggedStopped = orchestratesOnItsOwn(file.content)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// orchestratesOnItsOwn tells whether a configuration file orchestrates the
// object ha or start, in its DEFAULT section: the daemon would start it on
// its own, and flags it stopped on the node that created it.
func orchestratesOnItsOwn(content string) bool {
	section := "DEFAULT"
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if !strings.EqualFold(section, "DEFAULT") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "orchestrate" {
			continue
		}
		value = strings.TrimSpace(value)
		return value == "ha" || value == "start"
	}
	return false
}
