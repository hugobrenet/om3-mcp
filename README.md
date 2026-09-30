# OpenSVC Daemon MCP

A Go Model Context Protocol server providing AI agents with typed tools for the
OpenSVC v3 daemon API: cluster state, nodes, objects, instances, resources and logs.

Tools are mostly read-only, with an explicit instance status refresh.
The OpenSVC daemon enforces the caller's grants.

## Status

The server uses HTTPS over TCP. Remote agents can discover OAuth metadata,
register with DCR, authenticate an OpenSVC user and authorize access to the
selected cluster. Authorization Code with PKCE returns a separate MCP access
token to the agent, which can then call the tools. Refresh is not supported yet.

See the [tool documentation](docs/tools/README.md) for inputs, outputs and usage.

## Requirements

- Go 1.25.5 or later to build from source.
- Access to an OpenSVC v3 daemon API.
- The target cluster's public CA certificate and an OpenSVC user.
- A server certificate and private key for HTTPS.

## Build

From the repository root:

```bash
go build -o bin/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
```

## Remote HTTPS

Configure the HTTPS listener with its certificate, canonical URL and cluster
catalogue. The [configuration guide](docs/configuration.md#https) provides a
complete example and the [cluster template](deploy/examples/clusters.yaml).

After `/login`, the user authorizes the application and returns to its OAuth
callback. The agent exchanges the code for an MCP token and calls `/mcp`.
See [authentication](docs/authentication.md) for client setup, TLS trust and
current limitations.

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
