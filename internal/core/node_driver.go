package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	defaultNodeDriverLimit     = 100
	maxNodeDriverLimit         = 200
	maxNodeDriverCursorLength  = 1024
	maxNodeDriverNameRunes     = 1024
	maxNodeDriverPageTextRunes = 64 << 10
)

type ListNodeDriversOptions struct {
	Node   string
	Limit  int
	Cursor string
}

type NodeDriverList struct {
	Provenance    Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node          string     `json:"node" jsonschema:"the OpenSVC node reported by driver entries, or the local-node alias underscore when an empty local result cannot resolve it"`
	ReportedTotal int        `json:"reported_total" jsonschema:"number of driver entries returned by OpenSVC before exact duplicate removal"`
	Total         int        `json:"total" jsonschema:"number of distinct driver names returned by OpenSVC before MCP pagination"`
	Count         int        `json:"count" jsonschema:"number of distinct driver names returned in this page"`
	Drivers       []string   `json:"drivers" jsonschema:"exact registered driver names sorted lexicographically"`
	NextCursor    string     `json:"next_cursor,omitempty" jsonschema:"exact driver name to pass unchanged for the next page"`
	Truncated     bool       `json:"truncated" jsonschema:"whether distinct driver names remain after this page"`
}

type daemonNodeDriverList struct {
	Kind  string                 `json:"kind"`
	Items []daemonNodeDriverItem `json:"items"`
}

type daemonNodeDriverItem struct {
	Kind string `json:"kind"`
	Meta struct {
		Node string `json:"node"`
	} `json:"meta"`
	Data struct {
		Name string `json:"name"`
	} `json:"data"`
}

func (s *Service) ListNodeDrivers(ctx context.Context, options ListNodeDriversOptions) (NodeDriverList, error) {
	node, limit, cursor, err := validateNodeDriverOptions(options)
	if err != nil {
		return NodeDriverList{}, err
	}

	var response daemonNodeDriverList
	endpoint := fmt.Sprintf("/api/node/name/%s/drivers", node)
	if err := s.client.GetJSON(ctx, endpoint, url.Values{}, &response); err != nil {
		return NodeDriverList{}, fmt.Errorf("list node drivers: %w", err)
	}
	if response.Kind != "DriverList" {
		return NodeDriverList{}, fmt.Errorf("list node drivers: unexpected response kind %q", response.Kind)
	}

	resolvedNode := node
	unique := make(map[string]struct{}, len(response.Items))
	for index, item := range response.Items {
		if item.Kind != "DriverItem" {
			return NodeDriverList{}, fmt.Errorf("list node drivers: item %d has unexpected kind %q", index, item.Kind)
		}
		if !validExactNodeName(item.Meta.Node) {
			return NodeDriverList{}, fmt.Errorf("list node drivers: item %d reports invalid node %q", index, item.Meta.Node)
		}
		if node != localDaemonNodeAlias && item.Meta.Node != node {
			return NodeDriverList{}, fmt.Errorf("list node drivers: item %d reports unexpected node %q", index, item.Meta.Node)
		}
		if node == localDaemonNodeAlias {
			if resolvedNode == localDaemonNodeAlias {
				resolvedNode = item.Meta.Node
			} else if item.Meta.Node != resolvedNode {
				return NodeDriverList{}, fmt.Errorf("list node drivers: item %d reports inconsistent node %q", index, item.Meta.Node)
			}
		}
		name := item.Data.Name
		if name == "" || len([]rune(name)) > maxNodeDriverNameRunes || strings.ContainsAny(name, "\r\n\x00") {
			return NodeDriverList{}, fmt.Errorf("list node drivers: item %d has an empty, oversized, or control-containing name", index)
		}
		unique[name] = struct{}{}
	}

	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	start := 0
	if cursor != "" {
		index := sort.SearchStrings(names, cursor)
		if index == len(names) || names[index] != cursor {
			return NodeDriverList{}, fmt.Errorf("node driver cursor is no longer present in the result")
		}
		start = index + 1
	}

	items := make([]string, 0, min(limit, len(names)-start))
	textRunes := 0
	end := start
	for end < len(names) && len(items) < limit {
		nameRunes := len([]rune(names[end]))
		if len(items) > 0 && textRunes+nameRunes > maxNodeDriverPageTextRunes {
			break
		}
		items = append(items, names[end])
		textRunes += nameRunes
		end++
	}
	if items == nil {
		items = []string{}
	}
	result := NodeDriverList{
		Provenance:    s.newProvenance(),
		Node:          resolvedNode,
		ReportedTotal: len(response.Items),
		Total:         len(names),
		Count:         len(items),
		Drivers:       items,
		Truncated:     end < len(names),
	}
	if result.Truncated {
		result.NextCursor = items[len(items)-1]
	}
	return result, nil
}

func validateNodeDriverOptions(options ListNodeDriversOptions) (string, int, string, error) {
	node := options.Node
	if node == "" {
		node = localDaemonNodeAlias
	} else if node != localDaemonNodeAlias && !validExactNodeName(node) {
		return "", 0, "", fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}
	limit := options.Limit
	if limit == 0 {
		limit = defaultNodeDriverLimit
	}
	if limit < 1 || limit > maxNodeDriverLimit {
		return "", 0, "", fmt.Errorf("node driver limit must be between 1 and %d", maxNodeDriverLimit)
	}
	if len(options.Cursor) > maxNodeDriverCursorLength || strings.ContainsAny(options.Cursor, "\r\n\x00") {
		return "", 0, "", fmt.Errorf("node driver cursor exceeds %d characters or contains control characters", maxNodeDriverCursorLength)
	}
	return node, limit, options.Cursor, nil
}
