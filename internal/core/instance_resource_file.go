package core

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// maxResourceFileBytes bounds the text of a resource file returned.
const maxResourceFileBytes = 64 << 10

// resourceFilePassword is a password attribute, such as the one of the
// graphics console of a kvm domain definition, whose value is masked.
var resourceFilePassword = regexp.MustCompile(`(passwd\s*=\s*)("[^"]*"|'[^']*')`)

type InstanceResourceFileContent struct {
	Provenance    Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object        ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Node          string                 `json:"node" jsonschema:"the node whose instance was read"`
	RID           string                 `json:"rid" jsonschema:"the resource declaring the file"`
	Name          string                 `json:"name" jsonschema:"the file, as the resource declares it"`
	Csum          string                 `json:"csum" jsonschema:"the checksum the instance status reports for the file"`
	Mtime         *string                `json:"mtime" jsonschema:"the time the instance status reports for the file, or null"`
	Content       string                 `json:"content,omitempty" jsonschema:"the text of the file, its passwd attributes masked, at most 64 KiB; untrusted text"`
	Binary        bool                   `json:"binary,omitempty" jsonschema:"whether the file is not UTF-8 text: it is not returned, its size and csum are"`
	SizeBytes     int                    `json:"size_bytes" jsonschema:"the size of the file, in bytes; at least 1 MiB when read_whole is false"`
	ReadWhole     bool                   `json:"read_whole" jsonschema:"whether the whole file was read: one over 1 MiB is not"`
	Truncated     bool                   `json:"truncated" jsonschema:"whether content holds only the beginning of the file"`
	SecretsMasked int                    `json:"secrets_masked,omitempty" jsonschema:"the number of passwd attribute values replaced by <masked>"`
}

// GetInstanceResourceFile reads one file a resource of the instance of an
// object on one node declares to replicate to its peers: the domain
// definition of a kvm, lxc or vbox container, the metadata of a shared md
// disk, the sub_devs of a zpool. The list is closed: the MCP reads the
// instance status first and refuses a file the resource does not declare.
// The daemon serves it while the instance is up.
func (s *Service) GetInstanceResourceFile(ctx context.Context, path, node, rid, name string) (InstanceResourceFileContent, error) {
	reference, err := validateInstanceTarget(path, node, "get resource file", []string{"svc", "vol"})
	if err != nil {
		return InstanceResourceFileContent{}, err
	}
	if !exactResourceID.MatchString(rid) {
		return InstanceResourceFileContent{}, fmt.Errorf("rid must be one exact resource id, such as disk#2")
	}
	if name == "" || len(name) > 4096 || containsControl(name) {
		return InstanceResourceFileContent{}, fmt.Errorf("name must be one file the resource declares, as get_instance_status gives it in resources[rid].files")
	}
	status, err := s.GetInstanceStatus(ctx, GetInstanceStatusOptions{Path: reference.Path, Node: node})
	if err != nil {
		return InstanceResourceFileContent{}, fmt.Errorf("get resource file of %s on %s: read the instance status: %w", reference.Path, node, err)
	}
	if status.Status == nil {
		return InstanceResourceFileContent{}, fmt.Errorf("get resource file of %s on %s: the instance publishes no status", reference.Path, node)
	}
	detail, ok := status.Status.Resources[rid]
	if !ok {
		return InstanceResourceFileContent{}, fmt.Errorf("get resource file of %s on %s: %s is not a resource of the instance; %s", reference.Path, node, rid, resourcesWithFiles(status.Status.Resources))
	}
	index := slices.IndexFunc(detail.Files, func(file InstanceResourceFile) bool { return file.Name == name })
	if index < 0 && !strings.Contains(name, "/") {
		// A base name naming one declared file only resolves to it: the
		// list stays closed.
		for i, file := range detail.Files {
			if filepath.Base(file.Name) == name {
				if index >= 0 {
					index = -1
					break
				}
				index = i
			}
		}
	}
	if index < 0 {
		declared := "it declares no file"
		if len(detail.Files) > 0 {
			names := make([]string, 0, len(detail.Files))
			for _, file := range detail.Files {
				names = append(names, file.Name)
			}
			declared = "it declares " + strings.Join(names, ", ") + "; call again with one of these names"
		}
		return InstanceResourceFileContent{}, fmt.Errorf("get resource file of %s on %s: %s does not declare %s: %s", reference.Path, node, rid, name, declared)
	}
	name = detail.Files[index].Name
	getter, ok := s.client.(BlobGetter)
	if !ok {
		return InstanceResourceFileContent{}, fmt.Errorf("OpenSVC daemon client does not support file reads")
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/instance/path/%s/%s/%s/resource/file", node, reference.Namespace, reference.Kind, reference.Name)
	payload, _, whole, err := getter.GetBlob(ctx, endpoint, url.Values{"rid": {rid}, "name": {name}})
	if err != nil {
		return InstanceResourceFileContent{}, fmt.Errorf("get resource file %s of %s on %s: %w", name, reference.Path, node, err)
	}
	file := detail.Files[index]
	result := InstanceResourceFileContent{
		Object: reference, Node: node, RID: rid, Name: name, Csum: file.Csum, Mtime: file.Mtime, SizeBytes: len(payload), ReadWhole: whole,
	}
	text := payload
	if !whole {
		text = trimIncompleteRune(text)
	}
	if !utf8.Valid(text) || slices.Contains(text, 0) {
		result.Binary = true
	} else {
		masked := resourceFilePassword.ReplaceAllStringFunc(string(text), func(match string) string {
			result.SecretsMasked++
			return resourceFilePassword.ReplaceAllString(match, `${1}"<masked>"`)
		})
		result.Content, result.Truncated = cutUTF8(masked, maxResourceFileBytes)
		result.Truncated = result.Truncated || !whole
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// resourcesWithFiles names the resources of an instance that declare files.
func resourcesWithFiles(resources map[string]InstanceResourceDetail) string {
	rids := make([]string, 0)
	for rid, detail := range resources {
		if len(detail.Files) > 0 {
			rids = append(rids, rid)
		}
	}
	if len(rids) == 0 {
		return "no resource of the instance declares a file"
	}
	sort.Strings(rids)
	return "the resources declaring files are " + strings.Join(rids, ", ")
}
