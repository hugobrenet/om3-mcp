package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/om3-mcp/internal/core"
)

type GetObjectDataKeyInput struct {
	Path string `json:"path" jsonschema:"the exact canonical OpenSVC cfg object path; no wildcard or selector"`
	Name string `json:"name" jsonschema:"the exact key name, as list_object_data_keys gives it"`
}

type GetObjectDataKeyOutput = core.ObjectDataKeyValue

type SetObjectDataKeyInput struct {
	Path        string  `json:"path" jsonschema:"the exact canonical OpenSVC cfg, sec or usr object path; no wildcard or selector"`
	Name        string  `json:"name" jsonschema:"the key name, such as app.conf or password"`
	Value       *string `json:"value,omitempty" jsonschema:"the value, a text, at most 64 KiB; an empty string is a value; or pass value_base64"`
	ValueBase64 *string `json:"value_base64,omitempty" jsonschema:"the value, binary bytes in standard base64, at most 64 KiB decoded; or pass value"`
	WaitSeconds int     `json:"wait_seconds,omitempty" jsonschema:"optional seconds, at most 120, to wait for the keys written to reach every node holding the object; the nodes not reached are returned"`
}

type DeleteObjectDataKeyInput struct {
	Path        string `json:"path" jsonschema:"the exact canonical OpenSVC cfg, sec or usr object path; no wildcard or selector"`
	Name        string `json:"name" jsonschema:"the exact key name, as list_object_data_keys gives it"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"optional seconds, at most 120, to wait for the change to reach every node holding the object; the nodes not reached are returned"`
}

type ObjectCertificateInput struct {
	Path string `json:"path" jsonschema:"the exact canonical OpenSVC sec object path; no wildcard or selector"`
}

type ObjectDataKeyWriteOutput = core.ObjectDataKeyWrite

type ObjectCertificateCreationOutput = core.ObjectCertificateCreation

type ObjectCertificateRenewalOutput = core.ObjectCertificateRenewal

// dataKeyWriteRights tells the agent the rights a key write requires.
const dataKeyWriteRights = "Requires admin on the object namespace; for a usr, holding every grant of the user too, or root, else the daemon refuses with 403."

// RegisterObjectDataActionTools registers the tools that write the keys of
// cfg, sec and usr objects and the certificates of sec objects. They are
// registered only when actions are enabled.
func RegisterObjectDataActionTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "set_object_data_key", Title: "Set object data key",
		Description: "Add a key to a cfg, sec or usr object, or replace its value: a configuration file of a cfg, a secret of a sec, a password or a key of a usr, whose key password is the password of the user. " +
			"The value is a text, value, or binary bytes, value_base64, at most 64 KiB; it is never read back nor returned, for a sec or a usr as for a cfg. The daemon writes it at once, replacing a value without asking; existed tells whether the key was there. " +
			"Returns last_modified, the time of the configuration written, and with wait_seconds the nodes it had not reached. " + dataKeyWriteRights,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetObjectDataKeyInput) (*mcp.CallToolResult, ObjectDataKeyWriteOutput, error) {
		result, err := service.SetObjectDataKey(ctx, core.SetObjectDataKeyOptions{Path: input.Path, Name: input.Name, Value: input.Value, ValueBase64: input.ValueBase64, WaitSeconds: input.WaitSeconds})
		if err != nil {
			return nil, ObjectDataKeyWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "delete_object_data_key", Title: "Delete object data key",
		Description: "Remove a key from a cfg, sec or usr object. The daemon does not refuse a key absent: existed tells whether it was there. A service projecting the key into a volume loses it there. " +
			"Returns last_modified, and with wait_seconds the nodes the change had not reached. " + dataKeyWriteRights,
		Annotations: actionAnnotations(true, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteObjectDataKeyInput) (*mcp.CallToolResult, ObjectDataKeyWriteOutput, error) {
		result, err := service.DeleteObjectDataKey(ctx, core.DeleteObjectDataKeyOptions{Path: input.Path, Name: input.Name, WaitSeconds: input.WaitSeconds})
		if err != nil {
			return nil, ObjectDataKeyWriteOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	if err := addTool(registrar, &mcp.Tool{
		Name: "create_object_certificate", Title: "Create object certificate",
		Description: "Create the certificate of a sec object, from its cn, alt_names, bits and validity keywords: the daemon keeps its private key, or generates one, then writes the certificate, signed by the sec its ca keyword names, whose private key the node must read, or self-signed without ca. " +
			"It replaces a certificate the sec holds, without asking, with a new serial number. Returns the certificate keys read again, private_key, certificate, certificate_chain, fullpem and serial_number, by name and size, never their values. " +
			"Requires admin on the namespace of the sec, and on the namespace of the ca when it is elsewhere.",
		Annotations: actionAnnotations(true, false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ObjectCertificateInput) (*mcp.CallToolResult, ObjectCertificateCreationOutput, error) {
		result, err := service.CreateObjectCertificate(ctx, input.Path)
		if err != nil {
			return nil, ObjectCertificateCreationOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "renew_object_certificate", Title: "Renew object certificate",
		Description: "Renew the certificate of a sec object when it is due, or when its domains changed: from its ACME directory, or generated again. A certificate not due is kept: renewed false, with the reason. " +
			"The daemon refuses with 409 a certificate it did not issue, installed from elsewhere, with the reason. An ACME renewal may take long: the MCP waits for it within the request timeout of the cluster. " +
			"Requires admin on the namespace of the sec, and root for an ACME renewal through acme.webroot.",
		Annotations: actionAnnotations(false, true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ObjectCertificateInput) (*mcp.CallToolResult, ObjectCertificateRenewalOutput, error) {
		result, err := service.RenewObjectCertificate(ctx, input.Path)
		if err != nil {
			return nil, ObjectCertificateRenewalOutput{}, err
		}
		return nil, result, nil
	})
}
