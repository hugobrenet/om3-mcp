package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const maxEnableResources = 50

// exactResourceID is one exact resource id, such as fs#1: the daemon reads
// the rid parameter as a selector expression, of lists, groups and
// dependencies, which the MCP does not pass.
var exactResourceID = regexp.MustCompile(`^[a-z]+#[A-Za-z0-9_.-]+$`)

type ObjectEnableResult struct {
	Provenance Provenance             `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     ClusterObjectReference `json:"object" jsonschema:"the canonical OpenSVC object reference"`
	Action     string                 `json:"action" jsonschema:"enable or disable"`
	// Resources are the resources the action applies to; empty, the whole
	// service.
	Resources []string `json:"resources" jsonschema:"the resource ids the action applies to; empty for the whole service"`
	// Keywords are the disable keywords of the sections touched, read again.
	Keywords      []ObjectConfigKeyword `json:"keywords" jsonschema:"the disable keyword of each section touched, DEFAULT for the whole service, read again after the write: true when disabled, empty when enabled"`
	KeywordsError string                `json:"keywords_error,omitempty" jsonschema:"why the disable keywords could not be read again; the write itself is done"`
}

// SetObjectEnabled enables or disables one svc, or some of its resources by
// their exact ids: a write of the disable keyword of DEFAULT or of each
// section, which the daemon answers at once, without an orchestration.
func (s *Service) SetObjectEnabled(ctx context.Context, path string, enable bool, resources []string) (ObjectEnableResult, error) {
	action := "disable"
	if enable {
		action = "enable"
	}
	reference, err := validateConfigWritePath(path)
	if err != nil {
		return ObjectEnableResult{}, err
	}
	if reference.Kind != "svc" {
		return ObjectEnableResult{}, fmt.Errorf("the %s action applies to svc objects only", action)
	}
	if len(resources) > maxEnableResources {
		return ObjectEnableResult{}, fmt.Errorf("at most %d resource ids are accepted", maxEnableResources)
	}
	for _, resource := range resources {
		if !exactResourceID.MatchString(resource) {
			return ObjectEnableResult{}, fmt.Errorf("resource %q must be one exact resource id, such as fs#1", resource)
		}
	}
	poster, ok := s.client.(JSONPoster)
	if !ok {
		return ObjectEnableResult{}, fmt.Errorf("OpenSVC daemon client does not support POST requests")
	}
	if err := s.refuseUnknownResources(ctx, reference, action, resources); err != nil {
		return ObjectEnableResult{}, err
	}
	query := url.Values{}
	if len(resources) > 0 {
		query.Set("rid", strings.Join(resources, ","))
	}
	endpoint := fmt.Sprintf("/api/object/path/%s/svc/%s/%s", reference.Namespace, reference.Name, action)
	// The daemon answers 204, without a body.
	if err := poster.PostJSON(ctx, endpoint, query, nil, nil); err != nil {
		return ObjectEnableResult{}, fmt.Errorf("%s object %s: %w", action, reference.Path, err)
	}
	result := ObjectEnableResult{Object: reference, Action: action, Resources: append([]string{}, resources...), Keywords: []ObjectConfigKeyword{}}
	keywords := []string{"disable"}
	if len(resources) > 0 {
		keywords = keywords[:0]
		for _, resource := range resources {
			keywords = append(keywords, resource+".disable")
		}
	}
	config, err := s.GetObjectConfig(ctx, GetObjectConfigOptions{Path: reference.Path, Keywords: keywords, Limit: maxGetObjectConfigLimit})
	if err != nil {
		result.KeywordsError = err.Error()
	} else {
		result.Keywords = config.Items
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// refuseUnknownResources refuses the ids that are not resources of the
// service: the daemon selects the resources it has among them, and answers
// 204 for one it has not, with nothing written. The <group>.type keyword of
// each group names the sections of that group the configuration has, where
// <rid>.* would answer the keywords of the driver for a section it has not.
func (s *Service) refuseUnknownResources(ctx context.Context, reference ClusterObjectReference, action string, resources []string) error {
	if len(resources) == 0 {
		return nil
	}
	groups := make([]string, 0, len(resources))
	for _, resource := range resources {
		group, _, _ := strings.Cut(resource, "#")
		if !slices.Contains(groups, group+".type") {
			groups = append(groups, group+".type")
		}
	}
	config, err := s.GetObjectConfig(ctx, GetObjectConfigOptions{Path: reference.Path, Keywords: groups, Limit: maxGetObjectConfigLimit})
	if err != nil {
		return fmt.Errorf("%s object %s: read its resources: %w", action, reference.Path, err)
	}
	existing := make([]string, 0, len(config.Items))
	for _, item := range config.Items {
		if section, _, ok := strings.Cut(item.Keyword, "."); ok {
			existing = append(existing, section)
		}
	}
	for _, resource := range resources {
		if !slices.Contains(existing, resource) {
			known := "none of its group"
			if len(existing) > 0 {
				known = strings.Join(existing, ", ")
			}
			return fmt.Errorf("%s object %s: %s is not a resource of the service, which has %s", action, reference.Path, resource, known)
		}
	}
	return nil
}
