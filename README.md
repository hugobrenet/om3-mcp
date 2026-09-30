# OpenSVC Daemon MCP

A Go Model Context Protocol server providing AI agents with typed tools for the
OpenSVC v3 daemon API: cluster state, nodes, objects, instances, resources and logs.

Tools are mostly read-only, with an explicit instance status refresh.
The OpenSVC daemon enforces the caller's grants.

## Available transports

| Transport | Authentication | Status |
|---|---|---|
| Unix socket | Delegated OpenSVC access JWT | MCP tools available |
| HTTPS over TCP | Cluster selection and OpenSVC username/password | Login prototype; OAuth callback, MCP tokens and remote tools are not available yet |

See the [tool documentation](docs/tools/README.md) for inputs, outputs and usage.

## Requirements

- Go 1.25.5 or later to build from source.
- Access to an OpenSVC v3 daemon API.
- The target cluster's public CA certificate and an OpenSVC user or access JWT.
- A server certificate and private key for the HTTPS transport.

## Build

From the repository root:

```bash
go build -o bin/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
```

## Run locally

Provide the target cluster's public CA and run the Unix socket listener:

```bash
OPENSVC_DAEMON_URL=https://127.0.0.1:1215 \
OPENSVC_MCP_SOCKET_PATH=/tmp/opensvc-daemon-mcp.sock \
OPENSVC_MCP_JWT_VERIFY_KEY_FILE=/etc/opensvc-mcp/trust/cluster-ca.pem \
OPENSVC_DAEMON_TLS_CA_FILE=/etc/opensvc-mcp/trust/cluster-ca.pem \
  ./bin/opensvc-daemon-mcp
```

The MCP client connects to `/mcp` through this socket and supplies
`Authorization: Bearer <OpenSVC access JWT>` on every HTTP request.

## Remote HTTPS

Configure the HTTPS listener with its certificate, canonical URL and cluster
catalogue. The [configuration guide](docs/configuration.md#https) provides a
complete example and the [cluster template](deploy/examples/clusters.yaml).

The current flow reaches `/login` and validates an OpenSVC user against the
selected cluster. It stops at the browser confirmation; the MCP client remains
waiting for its OAuth callback. See [authentication](docs/authentication.md)
for client setup, TLS trust and current limitations.

## Documentation

- [Configuration reference and deployment](docs/configuration.md)
- [Authentication and client setup](docs/authentication.md)
- [Tools and shared contracts](docs/tools/README.md)
- [Local systemd unit](deploy/systemd/opensvc-daemon-mcp.service)

## Development

```bash
go fmt ./...
go test ./...
go vet ./...
```

## License

[Apache License 2.0](LICENSE).
