package tools

import (
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// coreToolMeta is the _meta key the tool search of an AI client reads, such
// as the OpenSVC AI agent: a client that shows a part of the catalog only
// always shows the tools marked true. A client that does not search ignores
// it.
const coreToolMeta = "opensvc/core"

// coreTools are always shown to the model: the state of the cluster, of its
// objects, instances and nodes, and the identity of the caller. The other
// tools are found by a search.
var coreTools = []string{"get_cluster_status", "list_cluster_objects", "get_object_status", "get_instance_status", "get_node_status", "get_caller_identity"}

// searchMeta is the _meta of a tool for the tool search: none for a tool
// that is not core.
func searchMeta(name string) mcp.Meta {
	if slices.Contains(coreTools, name) {
		return mcp.Meta{coreToolMeta: true}
	}
	return nil
}
