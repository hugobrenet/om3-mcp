package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	maxCallerGrants         = 256
	maxCallerGrantTextRunes = 255
)

type CallerIdentity struct {
	Provenance      Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Name            string        `json:"name" jsonschema:"the user name the daemon recognized for the caller"`
	Grants          []CallerGrant `json:"grants" jsonschema:"the grants of the caller, sorted by role then namespace"`
	GrantsTruncated bool          `json:"grants_truncated" jsonschema:"whether grants were omitted after 256 entries"`
}

type CallerGrant struct {
	Role      string `json:"role" jsonschema:"the role granted, such as root, admin, operator or guest"`
	Namespace string `json:"namespace,omitempty" jsonschema:"the namespace the role is limited to; omitted for a role granted cluster-wide"`
}

type daemonCallerIdentity struct {
	Name     string  `json:"name"`
	RawGrant *string `json:"raw_grant"`
}

// GetCallerIdentity reads the identity and grants the daemon applies to the
// caller's token. The grants are read from their raw list: the daemon map
// merges a role granted cluster-wide with the same role limited to
// namespaces.
func (s *Service) GetCallerIdentity(ctx context.Context) (CallerIdentity, error) {
	var response daemonCallerIdentity
	if err := s.client.GetJSON(ctx, "/api/auth/whoami", url.Values{}, &response); err != nil {
		return CallerIdentity{}, fmt.Errorf("get caller identity: %w", err)
	}
	if response.Name == "" || response.RawGrant == nil {
		return CallerIdentity{}, fmt.Errorf("get caller identity: response has no name or grant list")
	}
	seen := make(map[CallerGrant]bool)
	grants := make([]CallerGrant, 0)
	for _, raw := range strings.Fields(*response.RawGrant) {
		role, namespace, _ := strings.Cut(raw, ":")
		if role == "" {
			return CallerIdentity{}, fmt.Errorf("get caller identity: grant %q has no role", boundedCallerText(raw))
		}
		grant := CallerGrant{Role: boundedCallerText(role), Namespace: boundedCallerText(namespace)}
		if !seen[grant] {
			seen[grant] = true
			grants = append(grants, grant)
		}
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].Role != grants[j].Role {
			return grants[i].Role < grants[j].Role
		}
		return grants[i].Namespace < grants[j].Namespace
	})
	bounded, truncated := grants[:min(len(grants), maxCallerGrants)], len(grants) > maxCallerGrants
	return CallerIdentity{
		Provenance:      s.newProvenance(),
		Name:            boundedCallerText(response.Name),
		Grants:          bounded,
		GrantsTruncated: truncated,
	}, nil
}

func boundedCallerText(value string) string {
	bounded, _ := boundedRunes(value, maxCallerGrantTextRunes)
	return bounded
}
