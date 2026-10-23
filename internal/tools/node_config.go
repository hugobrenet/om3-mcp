package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type GetNodeConfigKeywordsInput struct {
	Node     string   `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	Keywords []string `json:"keywords,omitempty" jsonschema:"optional exact keywords, [<section>.]<option>, such as node.env or hb#1.timeout, at most 50; omitted, the keys node.conf sets"`
	Limit    int      `json:"limit,omitempty" jsonschema:"optional number of records, between 1 and 200; defaults to 100"`
}

type ListNodeConfigKeywordsInput struct {
	Node    string   `json:"node,omitempty" jsonschema:"optional: the catalog is the same on every node; an exact node name resolves a section filter on its configuration, omitted the daemon answering serves it"`
	Driver  string   `json:"driver,omitempty" jsonschema:"optional exact driver filter, such as hb.unicast or arbitrator; mutually exclusive with section"`
	Section string   `json:"section,omitempty" jsonschema:"optional exact section filter, such as node or hb#1; mutually exclusive with driver"`
	Option  string   `json:"option,omitempty" jsonschema:"optional exact option-name filter; alone it returns every matching definition variant"`
	Options []string `json:"options,omitempty" jsonschema:"optional exact option names, at most 30: only the definitions of these options, by name or alias, and missing_options names those no definition holds; mutually exclusive with option"`
	Limit   int      `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 25"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same node and filters"`
}

type UpdateNodeConfigInput struct {
	Node   string   `json:"node" jsonschema:"the exact name of the node; no wildcard, selector or the _ alias"`
	Set    []string `json:"set,omitempty" jsonschema:"optional set operations, each <section>.<keyword><operator><value>, as update_object_config takes them, such as node.env=PRD or hb#1.timeout=20s"`
	Unset  []string `json:"unset,omitempty" jsonschema:"optional keywords to remove, each <section>.<keyword>, such as node.comment"`
	Delete []string `json:"delete,omitempty" jsonschema:"optional sections to remove with their keywords, each a section name such as hb#2"`
}

type GetNodeConfigKeywordsOutput = core.NodeConfigKeywords

type ListNodeConfigKeywordsOutput = core.NodeConfigKeywordDefinitionList

type NodeConfigUpdateOutput = core.NodeConfigUpdate

// RegisterNodeConfigTools registers the tools that read the keywords of the
// configuration of a node and its keyword catalog.
func RegisterNodeConfigTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "get_node_config_keywords", Title: "Get node config keywords",
		Description: "Read keywords of the configuration of one OpenSVC node, raw and non-evaluated, through the daemon, which relays the read to the node: the keys node.conf sets, or the keywords asked, a keyword not set with an empty value. " +
			"node.uuid is the secret key the node authenticates to the collector with, not its identifier: the collector knows the node by its name, which answers a question about the identifier of the node in the collector; node.uuid is never returned, secrets_omitted counts it, set but not returned. Values may hold sensitive operational data; ask exact keywords when possible. " +
			"get_node_config reads the whole file instead, redacted. The cluster configuration is read with get_object_config, path cluster. Requires root.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeConfigKeywordsInput) (*mcp.CallToolResult, GetNodeConfigKeywordsOutput, error) {
		result, err := service.GetNodeConfigKeywords(ctx, core.GetNodeConfigKeywordsOptions{Node: input.Node, Keywords: input.Keywords, Limit: input.Limit})
		if err != nil {
			return nil, GetNodeConfigKeywordsOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "list_node_config_keywords", Title: "List node config keywords",
		Description: "List the keywords the configuration of an OpenSVC node supports, with their documentation, defaults, candidates and scoping: the sections node, cluster, hb, arbitrator, pool, network and the others. " +
			"The catalog is the same on every node: node is optional; a section filter is resolved on the configuration of the node answering. Use options to check the keywords of a write in one call. The cluster configuration has its own catalog: list_object_config_keywords, path cluster.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeConfigKeywordsInput) (*mcp.CallToolResult, ListNodeConfigKeywordsOutput, error) {
		result, err := service.ListNodeConfigKeywords(ctx, core.ListNodeConfigKeywordsOptions{
			Node: input.Node, Driver: input.Driver, Section: input.Section, Option: input.Option, Options: input.Options, Limit: input.Limit, Cursor: input.Cursor,
		})
		if err != nil {
			return nil, ListNodeConfigKeywordsOutput{}, err
		}
		return nil, result, nil
	})
}

// RegisterNodeConfigWriteTools registers the tool that writes the
// configuration of a node. It is registered only when actions are enabled.
func RegisterNodeConfigWriteTools(registrar *Registrar, service *core.Service) error {
	return addTool(registrar, &mcp.Tool{
		Name: "update_node_config", Title: "Update node config",
		Description: "Change the configuration of one OpenSVC node: delete sections, unset keywords, then apply set operations, as update_object_config does for an object; the daemon relays the write to the node. Check the keywords with list_node_config_keywords first. " +
			"Unlike an object, the configuration of a node does not propagate: there is no wait and no last_modified. The daemon refuses with 400 a configuration that does not validate, and writes a keyword it does not know with a warning it does not return. " +
			"Returns is_changed and the keywords named, read again; node.uuid is not read back, secrets_omitted counts it. The cluster configuration is written with update_object_config, path cluster. Requires root.",
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateNodeConfigInput) (*mcp.CallToolResult, NodeConfigUpdateOutput, error) {
		result, err := service.UpdateNodeConfig(ctx, core.UpdateNodeConfigOptions{Node: input.Node, Set: input.Set, Unset: input.Unset, Delete: input.Delete})
		if err != nil {
			return nil, NodeConfigUpdateOutput{}, err
		}
		return nil, result, nil
	})
}
