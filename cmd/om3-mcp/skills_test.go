package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/skills"
	skillfiles "github.com/opensvc/om3-mcp/skills"
	"go.yaml.in/yaml/v2"
)

// The MCP serves its skills as SEP-2640 asks: the extension declared, every
// skill listed with its frontmatter as written and the manifest of its files,
// SKILL.md included, each digest and size those of the bytes read, a skill
// got by the URI of its SKILL.md, and an unknown URI refused with -32602.
func TestSkillsExtension(t *testing.T) {
	handler, err := newConfiguredToolsHandler(nil, nil, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	if err := mcp.AddSendingCustomMethod[*skills.ListParams, *skills.ListResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*skills.GetParams, *skills.GetResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	capabilities := session.InitializeResult().Capabilities
	if capabilities.Resources == nil {
		t.Fatal("the resources capability is not declared")
	}
	extension, ok := capabilities.Extensions[skills.Extension].(map[string]any)
	if !ok || extension["directoryRead"] != false {
		t.Fatalf("extensions = %v", capabilities.Extensions)
	}

	list, err := mcp.CallCustomMethod[*skills.ListParams, *skills.ListResult](ctx, session, "skills/list", &skills.ListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.ResultType != "complete" || list.TTLMs <= 0 || list.CacheScope != "public" {
		t.Fatalf("list = %s %d %q", list.ResultType, list.TTLMs, list.CacheScope)
	}
	listed, err := fs.Glob(skillfiles.FS, "catalog/*/SKILL.md")
	if err != nil || len(list.Skills) != len(listed) || len(listed) == 0 {
		t.Fatalf("%d skills listed, %d in the repository: %v", len(list.Skills), len(listed), err)
	}
	read, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]*mcp.Resource{}
	for _, resource := range read.Resources {
		served[resource.URI] = resource
	}
	for _, entry := range list.Skills {
		name := strings.TrimSuffix(strings.TrimPrefix(entry.URI, "skill://"), "/SKILL.md")
		data, err := fs.ReadFile(skillfiles.FS, "catalog/"+name+"/SKILL.md")
		if err != nil {
			t.Fatalf("%s: %v", entry.URI, err)
		}
		// The frontmatter, read again from the file, is the one listed, field
		// by field, its string values strings.
		header, _, _ := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
		var want struct {
			Name        string            `yaml:"name"`
			Description string            `yaml:"description"`
			Metadata    map[string]string `yaml:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(header), &want); err != nil {
			t.Fatal(err)
		}
		metadata, _ := entry.Frontmatter["metadata"].(map[string]any)
		if entry.Frontmatter["name"] != want.Name || entry.Frontmatter["description"] != want.Description || len(metadata) != len(want.Metadata) {
			t.Errorf("%s frontmatter = %v", entry.URI, entry.Frontmatter)
		}
		for key, value := range want.Metadata {
			if metadata[key] != value {
				t.Errorf("%s metadata %s = %#v, want the string %q", entry.URI, key, metadata[key], value)
			}
		}
		// The manifest names every file, SKILL.md included, each digest and
		// size those of the bytes resources/read gives.
		files, _ := fs.Glob(skillfiles.FS, "catalog/"+name+"/*")
		if len(entry.Resources) < len(files) || entry.Resources[0].URI == "" {
			t.Errorf("%s manifest = %+v", entry.URI, entry.Resources)
		}
		self := false
		for _, file := range entry.Resources {
			self = self || file.URI == entry.URI
			contents, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: file.URI})
			if err != nil {
				t.Fatalf("%s: %v", file.URI, err)
			}
			sum := sha256.Sum256([]byte(contents.Contents[0].Text))
			if file.Digest != "sha256:"+hex.EncodeToString(sum[:]) || file.Size != int64(len(contents.Contents[0].Text)) {
				t.Errorf("%s: digest %s size %d, read %d bytes", file.URI, file.Digest, file.Size, len(contents.Contents[0].Text))
			}
			if served[file.URI] == nil {
				t.Errorf("%s is not listed by resources/list", file.URI)
			}
		}
		if !self {
			t.Errorf("%s manifest lacks SKILL.md itself", entry.URI)
		}
		if resource := served[entry.URI]; resource == nil || resource.Name != want.Name || resource.Description != want.Description || resource.MIMEType != "text/markdown" {
			t.Errorf("%s resource = %+v", entry.URI, resource)
		}

		got, err := mcp.CallCustomMethod[*skills.GetParams, *skills.GetResult](ctx, session, "skills/get", &skills.GetParams{URI: entry.URI})
		if err != nil || got.ResultType != "complete" || got.Skill.URI != entry.URI || len(got.Skill.Resources) != len(entry.Resources) || got.Skill.Resources[0].Digest != entry.Resources[0].Digest {
			t.Errorf("skills/get %s = %+v, %v", entry.URI, got, err)
		}
	}

	_, err = mcp.CallCustomMethod[*skills.GetParams, *skills.GetResult](ctx, session, "skills/get", &skills.GetParams{URI: "skill://no-such-skill/SKILL.md"})
	var wire *jsonrpc.Error
	if !errors.As(err, &wire) || wire.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("skills/get of an unknown skill: %v", err)
	}
}
