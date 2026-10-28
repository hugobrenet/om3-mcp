package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v2"
)

// Extension is the MCP extension serving Agent Skills, SEP-2640: the
// files of a skill are resources under skill://<name>/, listed with their
// SHA-256 digest and size by skills/list and skills/get. The go-sdk serves
// neither method yet: they are custom methods until it does.
const Extension = "io.modelcontextprotocol/skills"

const (
	skillScheme = "skill://"
	skillFile   = "SKILL.md"
	// skillsTTLMs is the freshness of a listing: the skills are embedded in
	// the binary, the digests telling a new version.
	skillsTTLMs = 300000
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Entry is a skill as skills/list and skills/get give it: the URI of
// its SKILL.md, its frontmatter as written, and the manifest of its files.
type Entry struct {
	URI         string         `json:"uri"`
	Frontmatter map[string]any `json:"frontmatter"`
	Resources   []File         `json:"resources"`
}

// File is a file of a skill: its URI, the SHA-256 digest of its
// bytes, "sha256:<hex>", and their count.
type File struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// ListParams are the parameters of skills/list; the skills fit one
// page, the cursor is accepted and ignored.
type ListParams struct {
	mcp.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

// ListResult is the result of skills/list.
type ListResult struct {
	mcp.ResultBase
	mcp.Cacheable
	ResultType string  `json:"resultType"`
	Skills     []Entry `json:"skills"`
}

// GetParams are the parameters of skills/get: the URI of a SKILL.md.
type GetParams struct {
	mcp.ParamsBase
	URI string `json:"uri"`
}

// GetResult is the result of skills/get.
type GetResult struct {
	mcp.ResultBase
	mcp.Cacheable
	ResultType string `json:"resultType"`
	Skill      Entry  `json:"skill"`
}

type skillFileContent struct {
	uri, name, description, mimeType string
	data                             []byte
}

// Register serves the skills of catalog, one directory each: each file as a
// resource, and skills/list and skills/get. The digests are computed once,
// here, from the bytes of catalog. The caller declares Extension with the
// resources capability.
func Register(server *mcp.Server, catalog fs.FS) (err error) {
	if server == nil {
		return fmt.Errorf("skill server is nil")
	}
	entries, files, err := loadSkills(catalog)
	if err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register skill resources: %v", recovered)
		}
	}()
	for _, file := range files {
		server.AddResource(&mcp.Resource{
			URI:         file.uri,
			Name:        file.name,
			Description: file.description,
			MIMEType:    file.mimeType,
			Size:        int64(len(file.data)),
		}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
				URI: file.uri, MIMEType: file.mimeType, Text: string(file.data),
			}}}, nil
		})
	}
	cacheable := mcp.Cacheable{TTLMs: skillsTTLMs, CacheScope: "public"}
	if err := mcp.AddReceivingCustomMethod(server, "skills/list", func(context.Context, *mcp.ServerSession, *ListParams) (*ListResult, error) {
		return &ListResult{Cacheable: cacheable, ResultType: "complete", Skills: entries}, nil
	}); err != nil {
		return err
	}
	return mcp.AddReceivingCustomMethod(server, "skills/get", func(_ context.Context, _ *mcp.ServerSession, params *GetParams) (*GetResult, error) {
		index := slices.IndexFunc(entries, func(entry Entry) bool { return params != nil && entry.URI == params.URI })
		if index < 0 {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "unknown skill: give the URI of a SKILL.md skills/list lists"}
		}
		return &GetResult{Cacheable: cacheable, ResultType: "complete", Skill: entries[index]}, nil
	})
}

// loadSkills reads every directory of skillFS, each a skill with its
// SKILL.md: its frontmatter, checked as the Agent Skills specification asks,
// and the manifest of its files, text files only. Anything else, a file
// beside the directories or a directory without SKILL.md, such as a
// skill.md, is refused: it would be in the repository and not served.
func loadSkills(skillFS fs.FS) ([]Entry, []skillFileContent, error) {
	dirs, err := fs.ReadDir(skillFS, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("read skills: %w", err)
	}
	var entries []Entry
	var files []skillFileContent
	for _, dir := range dirs {
		name := dir.Name()
		if !dir.IsDir() {
			return nil, nil, fmt.Errorf("%s is not a skill directory", name)
		}
		content, err := fs.ReadFile(skillFS, path.Join(name, skillFile))
		if err != nil {
			return nil, nil, fmt.Errorf("skill %s: %w", name, err)
		}
		frontmatter, err := parseSkillFrontmatter(name, content)
		if err != nil {
			return nil, nil, fmt.Errorf("skill %s: %w", name, err)
		}
		description, _ := frontmatter["description"].(string)
		entry := Entry{URI: skillScheme + name + "/" + skillFile, Frontmatter: frontmatter}
		err = fs.WalkDir(skillFS, name, func(file string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(skillFS, file)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			uri := skillScheme + file
			entry.Resources = append(entry.Resources, File{URI: uri, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(data))})
			// The resources are served as text: a file of another type would
			// be served broken.
			mimeType := skillMIMEType(file)
			if !strings.HasPrefix(mimeType, "text/") || !utf8.Valid(data) {
				return fmt.Errorf("%s is no UTF-8 text but %s: a skill serves text files only", file, mimeType)
			}
			resource := skillFileContent{uri: uri, name: strings.TrimPrefix(file, name+"/"), mimeType: mimeType, data: data}
			if path.Base(file) == skillFile && path.Dir(file) == name {
				resource.name, resource.description = name, description
			}
			files = append(files, resource)
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("skill %s: %w", name, err)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("no skill found")
	}
	return entries, files, nil
}

// parseSkillFrontmatter reads the YAML frontmatter of a SKILL.md, every
// field kept as written, its string values strings, and checks the fields
// the specification requires: name, the directory name, and description.
// Each value is a string or a map of strings, the form a client reads.
func parseSkillFrontmatter(dir string, content []byte) (map[string]any, error) {
	rest, ok := bytes.CutPrefix(content, []byte("---\n"))
	if !ok {
		return nil, fmt.Errorf("%s does not begin with a frontmatter", skillFile)
	}
	header, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return nil, fmt.Errorf("the frontmatter of %s is not closed", skillFile)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(header, &raw); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	frontmatter, err := jsonValue(raw)
	if err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	fields := frontmatter.(map[string]any)
	// The agent reads strings, and maps of strings on one level, only: a
	// list, a number or a deeper map would be served, then refused at its
	// loading.
	for key, value := range fields {
		switch value := value.(type) {
		case string:
		case map[string]any:
			for nested, item := range value {
				if _, ok := item.(string); !ok {
					return nil, fmt.Errorf("frontmatter %s.%s is no string", key, nested)
				}
			}
		default:
			return nil, fmt.Errorf("frontmatter %s is neither a string nor a map of strings", key)
		}
	}
	name, _ := fields["name"].(string)
	if name != dir || len(name) > 64 || !skillNamePattern.MatchString(name) {
		return nil, fmt.Errorf("name %q must be the directory name, at most 64 lowercase letters, digits and single hyphens", name)
	}
	if description, _ := fields["description"].(string); description == "" || utf8.RuneCountInString(description) > 1024 {
		return nil, fmt.Errorf("description must hold 1 to 1024 characters")
	}
	return fields, nil
}

// jsonValue turns a YAML value into its JSON form: the nested maps YAML
// keys by any value keyed by strings.
func jsonValue(value any) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			converted, err := jsonValue(item)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("key %v is not a string", key)
			}
			converted, err := jsonValue(item)
			if err != nil {
				return nil, err
			}
			out[name] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(value))
		for index, item := range value {
			converted, err := jsonValue(item)
			if err != nil {
				return nil, err
			}
			out[index] = converted
		}
		return out, nil
	default:
		return value, nil
	}
}

func skillMIMEType(file string) string {
	if path.Ext(file) == ".md" {
		return "text/markdown"
	}
	if mimeType := mime.TypeByExtension(path.Ext(file)); mimeType != "" {
		return mimeType
	}
	return "text/plain"
}
