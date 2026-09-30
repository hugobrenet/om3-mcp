# OpenSVC Daemon MCP

A Go-based Model Context Protocol server that gives AI agents a controlled, typed interface to the OpenSVC v3 daemon API.

The project is intended to become the low-level operational MCP layer for AI-assisted inspection, diagnosis, and administration of OpenSVC clusters. It exposes carefully designed tools instead of a generic raw API proxy. The existing local mode runs close to an OpenSVC daemon. A remote HTTPS mode over TCP is being built for a dedicated MCP service connecting to remote clusters.

## Tool documentation

The MCP currently exposes mostly read-only tools plus one explicit,
non-destructive instance status refresh.
Every successful tool result includes a minimal `provenance` object naming the
OpenSVC daemon API as its source and recording when the MCP collected the
result; this time does not establish freshness of the underlying status.
Detailed documentation is organized by OpenSVC daemon domain and includes
representative input/output examples:

- [Tool index and shared contracts](docs/tools/README.md)
- [Daemon tools](docs/tools/daemon.md)
- [Cluster tools](docs/tools/cluster.md)
- [Node tools](docs/tools/node.md)
- [Object tools](docs/tools/objects.md)
- [Instance tools](docs/tools/instances.md)
- [Resource tools](docs/tools/resources.md)

## Requirements

- Go 1.25.5 or later
- Access to an OpenSVC v3 daemon API
- The public certificate or RSA public key of the OpenSVC cluster CA
- An OpenSVC access JWT for the MCP client
- Git

The OpenSVC CA and JWT requirements apply to the existing Unix transport.
The HTTPS transport requires a server certificate and private key. Its optional
remote OAuth prototype implements discovery, client registration and the journey
to a disabled login form. Remote authentication and daemon delegation are still
being implemented.

## Installation from source

Clone the repository:

~~~bash
git clone https://github.com/opensvc/om3-mcp.git
cd om3-mcp
~~~

Download dependencies:

~~~bash
go mod download
~~~

Build the server:

~~~bash
mkdir -p bin
go build -o bin/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
~~~

## Configuration

The server supports these environment variables:

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

Example:

~~~bash
export OPENSVC_DAEMON_URL=https://127.0.0.1:1215
export OPENSVC_MCP_SOCKET_PATH=/run/opensvc-daemon-mcp/mcp.sock
export OPENSVC_MCP_JWT_VERIFY_KEY_FILE=/var/lib/opensvc/certs/ca_certificates
~~~

For a local development daemon using a self-signed certificate, verification can be explicitly disabled:

~~~bash
export OPENSVC_DAEMON_TLS_INSECURE=true
~~~

This disables certificate-chain and hostname verification. Never enable it when connecting to a daemon over an untrusted network. The default remains secure.

The configured verification file contains public material only, but it must be readable by the MCP process. Never expose or mount `/var/lib/opensvc/certs/ca_private_key` into the MCP server.

In Unix mode, each MCP HTTP request must contain:

~~~text
Authorization: Bearer <jwt>
~~~

The middleware accepts only JWTs signed with RS256 by the configured cluster CA. It requires valid `exp`, `sub`, `iss`, and `token_use=access` claims. The authenticated subject is bound to the MCP session to prevent session hijacking. The raw JWT remains request-scoped and is forwarded to the daemon, which independently validates it and applies its `grant` claims.

The local mode has no Basic Auth, X.509 client-authentication, local token file, unauthenticated mode, or fallback service credential.

In HTTPS mode, omitting all three prototype variables retains the transport-only
listener: `/mcp` returns `503`, and OAuth routes are not exposed. Supplying any
of these variables requires all three to be valid and `https` to be selected.
With the prototype enabled, `/mcp` returns an OAuth `401` challenge and the
routes described below are available. Neither mode accepts or forwards client
Bearer tokens, calls a daemon, or requires the local OpenSVC JWT verification
key. The planned remote mode will use separate MCP OAuth tokens and per-user
OpenSVC credentials.

## Run locally over a Unix socket

Start the Streamable HTTP MCP server:

~~~bash
OPENSVC_DAEMON_URL=https://127.0.0.1:1215 \
OPENSVC_MCP_SOCKET_PATH=/run/opensvc-daemon-mcp/mcp.sock \
OPENSVC_MCP_JWT_VERIFY_KEY_FILE=/var/lib/opensvc/certs/ca_certificates \
OPENSVC_DAEMON_TLS_INSECURE=true \
  ./bin/opensvc-daemon-mcp
~~~

In this mode, the server exposes its `/mcp` HTTP route through the configured Unix
socket. The parent directory must already exist; the supplied systemd unit
creates it with `RuntimeDirectory=opensvc-daemon-mcp`. The server validates the
path, refuses to replace an ordinary file or active socket, removes a proven
stale socket, applies mode `0660`, and cleans the socket up on a graceful stop.
`OPENSVC_DAEMON_TLS_INSECURE` affects only the separate MCP-to-daemon HTTPS
connection.

## Run the HTTPS listener over TCP

Select HTTPS explicitly and supply a certificate and matching private key:

~~~bash
OPENSVC_MCP_TRANSPORT=https \
OPENSVC_MCP_LISTEN_ADDR=127.0.0.1:8443 \
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-mcp/tls/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-mcp/tls/server.key \
  ./bin/opensvc-daemon-mcp
~~~

The certificate is loaded before the TCP socket is opened. Invalid or unreadable
TLS material, a mismatched private key, or a bind failure stops startup.
The listener requires TLS 1.2 or later and does not serve plaintext HTTP.
Certificate files are loaded at startup; restart the process after replacing
them. Shutdown drains active requests with the same 30-second deadline as
the local transport.

Only the selected listener is opened. HTTPS does not create a Unix socket.
Providing TCP or TLS settings without selecting `https` is a configuration
error. TCP ports must be numeric and between 1 and 65535, with an explicit IP
host; use `0.0.0.0` or `[::]` when binding all interfaces intentionally.
`OPENSVC_DAEMON_TLS_INSECURE` affects daemon connections only, never this listener.

For a service using a floating IP, a documentation example bind address is
`192.0.2.10:443`, and its canonical MCP URL is `https://192.0.2.10/mcp`. The floating IP must be present on the
active node before binding. Port 443 requires an appropriate execution context
or `CAP_NET_BIND_SERVICE`. The certificate must include an IP SAN for
`192.0.2.10`, and its issuing CA must be trusted by both the client and browser.
The existing systemd unit below is for the local mode and grants no capabilities.

With a trusted certificate covering the configured address, check the listener:

~~~bash
curl --cacert /path/to/mcp-ca.pem https://127.0.0.1:8443/mcp
~~~

Without the prototype variables, the expected response is `503`; it confirms
HTTPS connectivity, not a completed MCP or OAuth session.

## Remote OAuth journey to the login form

Enable the prototype on the same HTTPS listener:

~~~bash
OPENSVC_MCP_TRANSPORT=https \
OPENSVC_MCP_LISTEN_ADDR=127.0.0.1:8443 \
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-mcp/tls/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-mcp/tls/server.key \
OPENSVC_MCP_PUBLIC_URL=https://127.0.0.1:8443 \
OPENSVC_MCP_CLUSTER_CONFIG_FILE=/etc/opensvc-mcp/clusters.yaml \
  ./bin/opensvc-daemon-mcp
~~~

All example addresses, cluster names and service paths used in tests are
synthetic. `192.0.2.10` is a documentation address, not a deployed endpoint.

`OPENSVC_MCP_PUBLIC_URL` is the origin reachable by the agent and browser on
the customer network or VPN. It need not be Internet-accessible. It is separate
from the listener bind address: binding `0.0.0.0:8443` does not make that address
the issuer. Discovery and redirects always use the configured origin, never
the request Host or forwarded headers.

The configuration remains on the MCP hosts. Install the cluster file and its
trusted public CA files before starting the process; the format is documented
below. `OPENSVC_MCP_PUBLIC_URL` and `OPENSVC_MCP_CLUSTER_CONFIG_FILE` must both be
supplied to enable the prototype. With neither, HTTPS retains its transport-only
`503` response. The former `OPENSVC_MCP_CLUSTER_REF` and
`OPENSVC_MCP_CLUSTER_NAME` settings have been removed: a nonempty value causes
a startup error explaining the migration to the file.

| Route | Prototype behavior |
|---|---|
| `/mcp` | `401` with a Bearer challenge pointing to the protected resource metadata; all tokens are rejected |
| `/.well-known/oauth-protected-resource/mcp` | Canonical MCP resource, authorization server and `mcp:access` scope; also available without `/mcp` suffix |
| `/.well-known/oauth-authorization-server` | Integrated issuer, discovery endpoints and PKCE `S256`; explicitly marked `opensvc_login_prototype` |
| `/register` | POST DCR for public clients, with no client secret |
| `/authorize` | GET validates the registered client, callback, resource, scope, state and S256 challenge, then redirects to `/login` |
| `/login` | GET displays the declared application name and an enabled dropdown of configured clusters; credentials and submit remain disabled, POST returns `405` |
| `/token` | POST returns `503 temporarily_unavailable` without reading credentials or issuing tokens |

Registration accepts only HTTP callbacks on loopback addresses or `localhost`.
For literal loopback IPs, only the port may change from the registered URI;
the host, path and query must match. `localhost` callbacks must match exactly.
HTTPS web callbacks, custom schemes, confidential clients and CIMD are outside
this prototype. The only provisioned grant is `authorization_code`.

The validated request is held on the server and associated with an opaque,
Secure, HttpOnly, SameSite=Lax cookie. The login URL contains no request context.
An absent or expired context is rejected; application and cluster names are
HTML-escaped. Each cluster option displays its `name` and carries its stable
reference. No target is preselected, and `/authorize` does not bind a cluster.
Only names and references appear in the page, never daemon endpoints, IDs,
CA paths or certificates. Binding the selected target to the authorization
request will be implemented with successful OpenSVC login.
Client names are self-declared metadata, not verified identities.
The form clearly identifies itself as a prototype; its username, secret and
submit controls are disabled. No OpenSVC credentials, authorization codes or
OAuth tokens are processed or issued.

State is bounded and held only in process memory: at most 256 registrations
lasting 24 hours and 256 authorization requests lasting 10 minutes. Restarting
the process invalidates both. Persistence, failover continuity and actual
authentication will be implemented in later increments.

### Try with Codex

For the local example above, configure the server and initiate DCR:

~~~bash
codex mcp add opensvc --url https://127.0.0.1:8443/mcp
codex mcp login opensvc --oauth-client-registration dcr --no-browser
~~~

Open the authorization URL printed by Codex in a browser that trusts the server
CA. It should reach `/login` and display the associated form. Codex will remain
waiting for the callback because this increment deliberately stops at the form;
cancel the command after checking the page.

When using a private CA, Codex supports a PEM CA bundle via
`CODEX_CA_CERTIFICATE`, which takes precedence over `SSL_CERT_FILE`, according
to the [official OpenAI documentation](https://learn.chatgpt.com/docs/config-file/environment-variables).
For example, prefix the login command with
`CODEX_CA_CERTIFICATE=/path/to/mcp-ca.pem`. The browser needs to trust the same
CA separately. Keep certificate verification enabled.

This journey was also checked with Codex CLI 0.159.1 against a temporary
localhost listener with a generated test CA and fictitious cluster identity.
Codex performed discovery and DCR; following its authorization URL reached the
disabled form over verified TLS. This verifies the journey to the form, not a
completed OAuth login or access to a real cluster.

## Target cluster configuration

[deploy/examples/clusters.yaml](deploy/examples/clusters.yaml) documents the
supported version 1 format with fictitious names, a synthetic cluster ID and reserved
documentation IP addresses. The deployment file belongs on the MCP hosts at
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
no daemon transport and makes no daemon request. The trusted public CA must
eventually validate the daemon certificate, which must cover the endpoint IP
or hostname. Certificate trust and `expected_cluster_id` will be checked during
the future authenticated exchange. The daemon transport is needed after
cluster selection, before submitting credentials to that daemon; session
binding follows successful authentication. Endpoint selection and failover
remain to be implemented.

Loading the catalogue enables cluster choices on the prototype form; it does
not yet enable remote daemon access or complete OAuth authentication. The
future OpenSVC service must provide consistent configuration and CA files on
whichever host runs the MCP.

## systemd

The canonical unit is
`deploy/systemd/opensvc-daemon-mcp.service`. Systemd creates
`/run/opensvc-daemon-mcp` as `opensvc-mcp:opensvc-mcp` with mode `0750`; the
MCP process creates `mcp.sock` as `0660`. A local client service must receive
the supplementary `opensvc-mcp` group explicitly to connect.

## Development

Format the code:

~~~bash
go fmt ./...
~~~

Run tests:

~~~bash
go test -v ./...
~~~

Run static analysis:

~~~bash
go vet ./...
~~~

Build without writing a binary into the repository root:

~~~bash
go build -o /tmp/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
~~~

The test suite covers:

- generic JSON GET requests;
- bounded finite SSE reads for OpenSVC instance logs;
- bounded opaque stream reads for container stdout and stderr logs;
- RS256 JWT verification and required OpenSVC access-token claims;
- rejection of missing, invalid, expired, and refresh Bearer tokens;
- request-scoped Bearer delegation to the daemon;
- custom server CA loading and TLS verification;
- absence of JWT values from HTTP errors;
- URL and HTTP status handling;
- bounded RFC 7807 error propagation through real MCP tool calls, including malformed and interrupted responses;
- the current core use cases and their bounded response shaping;
- fail-fast validation of tool names, descriptions, annotations, schemas, and duplicate names;
- end-to-end Streamable HTTP MCP calls over a Unix socket to every registered tool using a delegated JWT against a fake OpenSVC daemon.
- HTTPS over TCP with certificate verification, rejection of untrusted certificates, wrong certificate identity, TLS 1.1 and plaintext HTTP;
- startup rejection for missing, malformed or mismatched TLS material and an occupied TCP address;
- blocked remote MCP requests without any daemon call while OAuth is pending;
- OAuth metadata and public DCR, callback validation, resource and PKCE binding;
- invalid or expired requests, bounded concurrent state and HTML escaping;
- the journey from discovery to the associated disabled login form over verified TLS;
- strict cluster catalogue and public CA validation, immutable snapshots and migration errors for removed settings;
- cluster choices and HTML escaping without exposing internal target settings or contacting a configured daemon;
- rejection of credential submission without reading the request body.

## Design principles

- Keep the OpenSVC daemon API client generic and internal.
- Keep endpoint selection and OpenSVC semantics in the core layer.
- Keep MCP schemas and registration in the tools layer.
- Keep user-facing tool documentation in docs/tools.
- Register each tool domain explicitly in main.go.
- Prefer typed, bounded tools over arbitrary API access.
- Do not expose credentials or raw secrets to MCP clients or language models.
- Add authentication and policy enforcement before state-changing tools.
- Verify OpenSVC operations after execution instead of assuming request acceptance means completion.
- Treat status returned by read-only GET tools as the daemon's last-known state; these tools do not implicitly run resource-driver probes.

## License

Licensed under the Apache License, Version 2.0. See the [LICENSE](LICENSE)
file.
