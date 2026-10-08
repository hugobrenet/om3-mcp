# om3-mcp

A Go Model Context Protocol server providing AI agents with typed tools for the
OpenSVC v3 daemon API: cluster state, nodes, objects, instances, resources and logs.

Tools are mostly read-only, with an explicit instance status refresh.
The OpenSVC daemon enforces the caller's grants.

## Status

The MCP has two listeners, each with one authentication model:

- **HTTPS `/mcp`** for external agents: OAuth discovery and local JWT
  verification, with no business scopes. For clusters configured with `auth`
  and confidential SSO profiles, daemon tools require `cluster_id` and exchange
  the MCP token for a target-specific daemon token. `list_clusters` discovers
  those clusters. Without exchange configuration, daemon calls remain blocked.
  MCP-audience tokens are never forwarded directly to a daemon.
- **Local Unix socket** for OpenSVC components such as the AI agent behind
  `om ai`: a daemon-issued token and an `X-OpenSVC-Cluster-ID` header are
  forwarded unchanged to the cluster VIP, whose daemon verifies them. The
  socket also serves the `whoami` identity bridge.

See the [tool documentation](docs/tools/README.md) for inputs, outputs and usage.

## Requirements

- Go 1.25.5 or later to build from source.
- For HTTPS: an OAuth/OIDC issuer publishing discovery metadata and public
  signing keys, access JWTs containing the configured MCP resource URL in their
  audience, and a server certificate and private key.
- For the Unix socket: a cluster catalogue and a socket directory shared with
  the agent account.

A cluster catalogue is required for daemon calls, plus a confidential client
secret for token exchange. HTTPS discovery alone needs neither.

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
- [Token delegation over the Unix socket](docs/delegation.md)
- [Tools and shared contracts](docs/tools/README.md)

## Development

```bash
go fmt ./...
go test ./...
go vet ./...
```

## License

[Apache License 2.0](LICENSE).
