# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8443 | TCP bind address, written as IPv4:port or [IPv6]:port |
| OPENSVC_MCP_TLS_CERT_FILE | empty | Absolute path to the server certificate PEM, followed by intermediate certificates if needed; required |
| OPENSVC_MCP_TLS_KEY_FILE | empty | Absolute path to the server private key PEM; required |
| OPENSVC_MCP_PUBLIC_URL | empty | Canonical HTTPS origin visible to the agent and browser, without a path or trailing slash; enables login together with the cluster catalogue |
| OPENSVC_MCP_CLUSTER_CONFIG_FILE | empty | Absolute path to the administrator's YAML cluster catalogue and public CA references; validated before opening the listener |

HTTPS is the only transport. Daemon endpoints, trust and request timeouts come
from the selected cluster's catalogue entry. TLS verification is mandatory.

## HTTPS

Install the server certificate, matching private key, cluster catalogue and
public target CA files before starting the listener:

```bash
OPENSVC_MCP_LISTEN_ADDR=127.0.0.1:8443 \
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-mcp/tls/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-mcp/tls/server.key \
OPENSVC_MCP_PUBLIC_URL=https://127.0.0.1:8443 \
OPENSVC_MCP_CLUSTER_CONFIG_FILE=/etc/opensvc-mcp/clusters.yaml \
  ./bin/opensvc-daemon-mcp
```

Use an address reachable by the agent and browser for a remote deployment.
The canonical public URL is an HTTPS origin without a path or trailing slash;
it may be reachable only on a private network or VPN. Discovery and redirects
use this configured origin, independently of the bind address and request
headers. The MCP endpoint is this origin followed by `/mcp`.

The bind address must contain an explicit IP and numeric port from 1 to 65535.
Use `0.0.0.0` or `[::]` to bind all interfaces intentionally. Certificate and
key paths must be absolute. The certificate must cover the client-facing IP
or hostname, and its CA must be trusted by the client and browser.
Certificate serial numbers must be unique for their issuer, including across
CA and server certificates. The leaf PEM may include intermediate certificates.

TLS 1.2 or later is required. Certificate loading and catalogue validation
happen before opening the listener; invalid settings stop startup. TLS files
and the catalogue are loaded once: restart the process to apply changes. Shutdown drains requests for up to
30 seconds.

`OPENSVC_MCP_PUBLIC_URL` and `OPENSVC_MCP_CLUSTER_CONFIG_FILE` must be supplied
together to enable remote login. With neither, the HTTPS listener exposes
`/mcp` with `503` and no OAuth routes. With both, `/mcp` returns an OAuth `401`
challenge; [remote authentication](authentication.md#https-login-prototype)
describes the available flow.

### Migration from local mode

Remove `OPENSVC_MCP_TRANSPORT`, `OPENSVC_MCP_SOCKET_PATH`,
`OPENSVC_MCP_JWT_VERIFY_KEY_FILE`, `OPENSVC_DAEMON_URL`,
`OPENSVC_DAEMON_TLS_CA_FILE`, `OPENSVC_DAEMON_TLS_INSECURE`,
`OPENSVC_DAEMON_REQUEST_TIMEOUT`, `OPENSVC_MCP_CLUSTER_REF` and
`OPENSVC_MCP_CLUSTER_NAME`. Any nonempty removed setting stops startup with
an explicit migration error. This also applies to `OPENSVC_MCP_TRANSPORT=https`.
Supply the TLS listener settings and move target configuration to the catalogue.
The MCP no longer accepts a daemon JWT as its caller credential.

For a floating IP deployment, assign the IP to the active host before binding
and include it in the certificate's IP SAN. Port 443 requires an appropriate
execution context or `CAP_NET_BIND_SERVICE`.

## Target cluster catalogue

[deploy/examples/clusters.yaml](../deploy/examples/clusters.yaml) documents the
supported version 1 format with fictitious names, a synthetic cluster ID and reserved
documentation IP addresses. Install the deployment file on the MCP hosts, for example at
`/etc/opensvc-mcp/clusters.yaml`; do not commit your actual cluster configuration.

The format contains:

- `version`: the configuration schema version;
- `clusters`: a map keyed by stable cluster reference, separate from the display name;
- `name`: the cluster name shown to the user;
- `expected_cluster_id`: the OpenSVC cluster ID to check during authenticated exchanges;
- `endpoints`: ordered HTTPS daemon origins, without credentials, API paths, query strings or fragments;
- `tls.ca_file`: an absolute path on the MCP host to the trusted public CA certificate or certificate chain;
- `request_timeout`: the daemon request timeout, for example `20s`.

The file contains no user credentials or private keys. The loader rejects
unknown fields, duplicate keys, additional YAML documents, invalid value types,
unsupported versions and empty catalogues. References contain 1–64 ASCII
letters, digits, hyphens or underscores; names contain 1–128 bytes and IDs
1–256 bytes, with no surrounding whitespace or control characters. Cluster
IDs are required and preserved exactly; they are not assumed to be UUIDs.
Equivalent endpoints are normalized and duplicates rejected; order is preserved.
Timeouts must be between `1s` and `2m`.

Configuration is bounded to 256 KiB, 64 clusters and 8 endpoints per cluster.
Each CA file must be an absolute path to a readable regular file of at most
1 MiB, containing only valid public PEM CA certificates. Private keys, leaf
certificates and extra text are rejected. Both configuration and CA files are
read once into an immutable snapshot; restart the process to apply changes.

Startup performs these checks locally, before opening the listener. It creates
no daemon transport and makes no daemon request. At login, the loaded public
CA validates the daemon certificate, which must cover the endpoint IP or
hostname. The signed access JWT and `expected_cluster_id` are checked before
creating the session. Endpoint failover remains to be implemented.

Loading the catalogue enables cluster choices and login. Remote MCP tool access
is not available yet. For deployment on multiple MCP hosts, provide consistent
configuration and CA files on each host.


## systemd

The supplied [systemd unit](../deploy/systemd/opensvc-daemon-mcp.service) runs
as `opensvc-mcp:opensvc-mcp`, reads `/etc/opensvc-mcp/mcp.env`, and expects the
binary at `/usr/local/libexec/opensvc-daemon-mcp`. Put the HTTPS environment
settings above in that file. The account must be able to read the certificate,
private key, catalogue and public CA files; restrict private-key access to the
service account. The unit grants no capabilities and does not depend on a local
OpenSVC daemon. Use an unprivileged port such as `8443` with this unit.
