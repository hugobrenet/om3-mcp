// Package resources serves the MCP resources of the server: reference texts
// a client reads on demand, such as the format of the configuration of an
// object.
package resources

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// objectConfigReference is the reference of the configuration file of an
// object, read from the daemon code and its keyword catalog, which the
// clients read before writing one.
//
//go:embed object_config.md
var objectConfigReference string

// ObjectConfigURI names the reference of the configuration of an object.
const ObjectConfigURI = "om3://docs/object-config"

// Register registers the resources on server, which an MCP client, such as
// the OpenSVC AI agent, reads on demand.
func Register(server *mcp.Server) (err error) {
	if server == nil {
		return fmt.Errorf("resource server is nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("register resource %q: %v", ObjectConfigURI, recovered)
		}
	}()
	server.AddResource(&mcp.Resource{
		URI:  ObjectConfigURI,
		Name: "object-config", Title: "Object configuration",
		Description: "The configuration file of an OpenSVC v3 object: its sections, the resources and their driver, the free sections, the scopes and references, examples, the update operators, and what follows a creation. Read it before writing a configuration with create_object or update_object_config.",
		MIMEType:    "text/markdown",
		Size:        int64(len(objectConfigReference)),
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: ObjectConfigURI, MIMEType: "text/markdown", Text: objectConfigReference,
		}}}, nil
	})
	return nil
}
