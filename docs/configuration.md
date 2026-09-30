# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_TRANSPORT | unix | Listener mode: `unix` for the existing local MCP or `https` for TLS over TCP |
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8443 in https mode | TCP bind address, written as IPv4:port or [IPv6]:port; requires explicit https mode |
| OPENSVC_MCP_TLS_CERT_FILE | empty | Absolute path to the HTTPS server certificate PEM, followed by intermediate certificates if needed; required in https mode |
| OPENSVC_MCP_TLS_KEY_FILE | empty | Absolute path to the HTTPS server private key PEM; required in https mode |
| OPENSVC_MCP_PUBLIC_URL | empty | Canonical HTTPS origin visible to the agent and browser, without a path or trailing slash; enables the login prototype together with the cluster configuration file |
| OPENSVC_MCP_CLUSTER_CONFIG_FILE | empty | Absolute path to the administrator's YAML cluster catalogue and public CA references; loaded and validated before opening the HTTPS listener |
| OPENSVC_DAEMON_URL | https://127.0.0.1:1215 | Base URL of the local OpenSVC daemon API |
| OPENSVC_DAEMON_REQUEST_TIMEOUT | 20s | Whole-request timeout for daemon JSON, SSE, and bounded stream calls; accepted range 1s to 2m |
| OPENSVC_MCP_SOCKET_PATH | /run/opensvc-daemon-mcp/mcp.sock | Local Unix socket carrying Streamable HTTP |
| OPENSVC_MCP_JWT_VERIFY_KEY_FILE | /var/lib/opensvc/certs/ca_certificates | OpenSVC cluster CA certificate or RSA public key used to verify JWT signatures |
| OPENSVC_DAEMON_TLS_CA_FILE | empty | PEM CA certificates appended to the system trust store |
| OPENSVC_DAEMON_TLS_INSECURE | false | Disable daemon certificate verification. Development only. |

The `OPENSVC_DAEMON_*` settings and `OPENSVC_MCP_JWT_VERIFY_KEY_FILE` configure
local Unix mode. Remote login uses the selected cluster's settings from the
catalogue instead. `OPENSVC_DAEMON_TLS_INSECURE` never affects the MCP listener
or remote login; TLS verification is enabled by default.

## Unix socket

```bash
OPENSVC_DAEMON_URL=https://127.0.0.1:1215 \
OPENSVC_MCP_SOCKET_PATH=/run/opensvc-daemon-mcp/mcp.sock \
OPENSVC_MCP_JWT_VERIFY_KEY_FILE=/var/lib/opensvc/certs/ca_certificates \
OPENSVC_DAEMON_TLS_CA_FILE=/etc/opensvc-mcp/trust/cluster-ca.pem \
  ./bin/opensvc-daemon-mcp
```

The parent directory must exist and be writable by the process. The server
uses socket mode `0660`, refuses to replace an ordinary file or active socket,
removes a stale socket and cleans up on graceful shutdown.

The configured JWT verification file contains public material only and must
be readable by the process. Keep the cluster CA private key on the target
cluster. See [local authentication](authentication.md#unix-socket).

For local development only, `OPENSVC_DAEMON_TLS_INSECURE=true` disables
certificate-chain and hostname verification on the local daemon connection.
Prefer supplying the daemon's public CA through `OPENSVC_DAEMON_TLS_CA_FILE`.

## HTTPS

Install the server certificate, matching private key, cluster catalogue and
public target CA files before starting the listener:

```bash
OPENSVC_MCP_TRANSPORT=https \
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
happen before opening the listener; invalid settings stop startup. Only the
selected transport is opened. TLS files and the catalogue are loaded once:
restart the process to apply changes. Shutdown drains requests for up to
30 seconds.

`OPENSVC_MCP_PUBLIC_URL` and `OPENSVC_MCP_CLUSTER_CONFIG_FILE` must be supplied
together to enable remote login. With neither, the HTTPS listener exposes
`/mcp` with `503` and no OAuth routes. With both, `/mcp` returns an OAuth `401`
challenge; [remote authentication](authentication.md#https-login-prototype)
describes the available flow.

The removed settings `OPENSVC_MCP_CLUSTER_REF` and `OPENSVC_MCP_CLUSTER_NAME`
cause a startup error when nonempty. Define clusters in the catalogue instead.

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

The supplied [systemd unit](../deploy/systemd/opensvc-daemon-mcp.service) is for
local Unix mode. It runs as `opensvc-mcp:opensvc-mcp`, reads
`/etc/opensvc-ai/mcp.env`, and expects the binary at
`/usr/local/libexec/opensvc-daemon-mcp`.

Systemd creates `/run/opensvc-daemon-mcp` with mode `0750`; the MCP creates its
socket with mode `0660`. Local client services need the supplementary
`opensvc-mcp` group to connect. The unit grants no capabilities.
