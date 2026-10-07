# om3-mcp

A Go Model Context Protocol server providing AI agents with typed tools for the
OpenSVC v3 daemon API: cluster state, nodes, objects, instances, resources and logs.

Tools are mostly read-only, with an explicit instance status refresh.
The OpenSVC daemon enforces the caller's grants.

## Status

The HTTPS `/mcp` endpoint now implements external-client OAuth discovery and
local JWT verification, with no business scopes. All existing tools are listed.
With a version 3 catalogue and confidential SSO profiles, daemon tools require
`cluster_id` and exchange the MCP token for a target-specific daemon token.
`list_clusters` discovers every configured cluster. Without exchange
configuration, daemon calls remain explicitly blocked.

The previous native/OpenID passthrough middleware and daemon identity bridge
are disconnected. Existing `om ai` / webapp integrations must be adjusted later.
MCP-audience tokens are never forwarded directly to a daemon.

See the [tool documentation](docs/tools/README.md) for inputs, outputs and usage.

## Requirements

- Go 1.25.5 or later to build from source.
- An OAuth/OIDC issuer publishing discovery metadata and public signing keys.
- Access JWTs containing the configured MCP resource URL in their audience.
- A server certificate and private key for HTTPS.

A daemon catalogue and confidential client secret are required for daemon calls.
They can be omitted when testing incoming OAuth and tool discovery alone.

## Build

From the repository root:

```bash
go build -o bin/om3-mcp ./cmd/om3-mcp
```

## Remote HTTPS

Configure the HTTPS listener, public MCP resource URL and trusted OAuth issuer.
The [configuration guide](docs/configuration.md#https) provides a
complete example and the [cluster template](deploy/examples/clusters.yaml).

For demos only, a cluster can set [`tls.insecure: true`](docs/configuration.md#demo-only-tls-bypass).
This disables daemon certificate verification and is strongly discouraged in production.

## Documentation

- [Configuration reference and deployment](docs/configuration.md)
- [Authentication and client setup](docs/authentication.md)
- [Token exchange and multi-cluster deployment](docs/token-exchange.md)
- [External-agent integration plan and lab validation (French)](docs/CHANTIER_MCP_AGENTS_EXTERNES.md)
- [Tools and shared contracts](docs/tools/README.md)

## Development

```bash
go fmt ./...
go test ./...
go vet ./...
```

## License

[Apache License 2.0](LICENSE).
