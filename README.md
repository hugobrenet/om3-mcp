# OpenSVC Daemon MCP

A Go Model Context Protocol server providing AI agents with typed tools for the
OpenSVC v3 daemon API: cluster state, nodes, objects, instances, resources and logs.

Tools are mostly read-only, with an explicit instance status refresh.
The OpenSVC daemon enforces the caller's grants.

## Status

The server uses HTTPS over TCP and accepts native OpenSVC access JWTs.
The same JWT is delegated to its emitting daemon, selected by signed
`cluster_id` and `iss` claims against an administrator-owned cluster catalogue.
The daemon verifies the JWT signature; no JWT verification keys are installed
on MCP or agent. There is no embedded authorization server or token exchange.

See the [tool documentation](docs/tools/README.md) for inputs, outputs and usage.

## Requirements

- Go 1.25.5 or later to build from source.
- Access to an OpenSVC v3 daemon API.
- A native OpenSVC access JWT with a signed `cluster_id` claim.
- An administrator-owned catalogue of trusted daemon HTTPS endpoints.
- A server certificate and private key for HTTPS.

## Build

From the repository root:

```bash
go build -o bin/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
```

## Remote HTTPS

Configure the HTTPS listener with its certificate and cluster catalogue.
The [configuration guide](docs/configuration.md#https) provides a
complete example and the [cluster template](deploy/examples/clusters.yaml).

For demos only, a cluster can set [`tls.insecure: true`](docs/configuration.md#demo-only-tls-bypass).
This disables daemon certificate verification and is strongly discouraged in production.

## Documentation

- [Configuration reference and deployment](docs/configuration.md)
- [Authentication and client setup](docs/authentication.md)
- [Tools and shared contracts](docs/tools/README.md)
- [Systemd unit](deploy/systemd/opensvc-daemon-mcp.service)

## Development

```bash
go fmt ./...
go test ./...
go vet ./...
```

## License

[Apache License 2.0](LICENSE).
