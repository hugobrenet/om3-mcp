# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8443 | TCP bind address, IPv4:port or [IPv6]:port |
| OPENSVC_MCP_TLS_CERT_FILE | empty | Absolute server certificate PEM path; required |
| OPENSVC_MCP_TLS_KEY_FILE | empty | Absolute server private key PEM path; required |
| OPENSVC_MCP_CLUSTER_CONFIG_FILE | empty | Version 3 catalogue required for exchanged daemon calls; optional for incoming OAuth discovery only |
| OPENSVC_MCP_AUTH_CONFIG_FILE | empty | Version 1 confidential SSO profile file; required with a version 3 catalogue |
| OPENSVC_MCP_OAUTH_RESOURCE_URL | empty | Required public HTTPS MCP URL ending in `/mcp`; expected access-token audience |
| OPENSVC_MCP_OAUTH_RESOURCE_NAME | OpenSVC Daemon MCP | Human-readable `resource_name` in public OAuth metadata |
| OPENSVC_MCP_OAUTH_ISSUER | empty | Required exact trusted issuer for incoming MCP access JWTs |
| OPENSVC_MCP_OAUTH_CA_FILE | empty | Optional absolute PEM trust bundle for SSO HTTPS; replaces system roots |

HTTPS is the only transport. There is no local socket mode, local daemon
dependency or login page. Daemon calls use RFC 8693 token exchange when a
version 3 catalogue and auth profiles are supplied together. Without them,
incoming OAuth and tool discovery work, but daemon calls remain blocked.
See [authentication](authentication.md) for the complete contract.

## HTTPS

Install the listener certificate/key and any SSO TLS CA bundle before starting.
JWT verification uses the SSO public JWKS; no JWT signing private key is needed:

```bash
OPENSVC_MCP_LISTEN_ADDR=0.0.0.0:8443 \
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-mcp/tls/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-mcp/tls/server.key \
OPENSVC_MCP_OAUTH_RESOURCE_URL=https://dev5-vip.opensvc.com:8443/mcp \
OPENSVC_MCP_OAUTH_ISSUER=https://auth.example.test/application/o/opensvc-mcp/ \
  ./bin/om3-mcp
```

The issuer above is illustrative and must be replaced with the actual issuer
of MCP access tokens. It does not select a daemon. No client secret is required
for discovery-only operation. A ready-to-adapt environment template is in
[deploy/examples/oauth.env](../deploy/examples/oauth.env).

The agent connects to `https://<mcp-host>:8443/mcp`. The bind address must
contain an explicit IP and a numeric port from 1 to 65535. Use `0.0.0.0` or
`[::]` only when intentionally listening on all interfaces. The certificate
must cover the agent-facing hostname or IP; its CA must be trusted by the agent.
Include intermediate certificates in the listener certificate PEM as needed.

TLS 1.2 or later is mandatory. Configuration, public trust files and listener
TLS material are validated before binding; startup never contacts a daemon.
Settings and files are snapshotted once. Restart to apply changes.
HTTP headers are bounded to 64 KiB, authenticated MCP request bodies to 1 MiB,
and header reads to 5 seconds. Shutdown drains requests for up to 30 seconds.

## Health check

`GET /health` on the same HTTPS listener returns HTTP 200 with
`{"status":"ok"}`, `Content-Type: application/json` and `Cache-Control: no-store`.
`HEAD /health` also returns HTTP 200, without a body. Other methods return 405.
No bearer token or target headers are required. The route reports local server
liveness only: it does not contact daemons or establish user authentication,
tool permissions or cluster health. An unavailable daemon does not affect it.

For a service check, use the hostname covered by the listener certificate:

```bash
curl --silent --show-error --fail --max-time 3 \
  https://mcp.example.test:8443/health >/dev/null
```

Use `--cacert /path/to/public-ca.pem` when the MCP listener uses a private CA.

## Cluster catalogue and exchange profiles

Use [clusters-v3.yaml](../deploy/examples/clusters-v3.yaml) and
[auth.yaml](../deploy/examples/auth.yaml) for external-agent daemon calls.
The catalogue maps `cluster_id` to a single HTTPS VIP, TLS trust, request timeout,
auth profile and target audience. No node inventory is required. The auth file
contains confidential client configuration and a secret-file reference, never
the secret itself. Unknown profile references prevent startup.

The loader accepts up to 4096 clusters and 4 MiB of YAML. Each CA bundle is
bounded to 1 MiB. Endpoint URLs must be HTTPS origins without credentials, paths,
queries or fragments. IDs are unique and compared exactly; display names need
not be unique. Names or nodes alone never select a target for a daemon call.
TLS uses system roots by default, or `tls.ca_file`, or `tls.insecure: true`;
`ca_file` and `insecure` are mutually exclusive. Request timeouts range from
1 second to 2 minutes.

The profile file accepts 1–64 profiles, HTTPS discovery issuers, a confidential
`client_id`, absolute `client_secret_file`, `client_secret_basic` (default) or
`client_secret_post`, up to 32 technical scopes, a `request_timeout` from 1 second
to 1 minute (default 10 seconds), and optional `tls.ca_file`. SSO TLS verification
cannot be disabled. Secret files must be regular, at most 16 KiB, mode 0600 or
0400, and contain one nonempty line. All files are loaded at startup; restart to
apply changes. Public discovery is lazy and cached for five minutes.

See [the deployment guide](token-exchange.md) for lab values, SSO setup, daemon
configuration and test boundaries.

## Legacy version 2 catalogue (not used for external OAuth routing)

The following format remains supported for validation of existing deployments.
It does not enable daemon calls in the OAuth entrypoint. The planned version 3
VIP catalogue will be implemented with multi-cluster routing.

See [the template](../deploy/examples/clusters.yaml). Install real configuration
outside the repository, for example at `/etc/opensvc-mcp/clusters.yaml`.

```yaml
version: 2
clusters:
  cluster-a:
    name: Example cluster
    expected_cluster_id: 00000000-0000-4000-8000-000000000001
    nodes:
      node-a: https://192.0.2.20:1215
      node-b: https://192.0.2.21:1215
    tls:
      ca_file: /etc/opensvc-mcp/trust/cluster-a-tls-ca.pem
    request_timeout: 20s
```

- `clusters`: stable administrative references; these are not JWT identities.
- `name`: human-readable name.
- `expected_cluster_id`: exact native JWT `cluster_id` or OpenID HTTP target
  (`X-OpenSVC-Cluster-ID`) routing key; must be unique.
- `nodes`: exact daemon node names (native JWT `iss` or OpenID HTTP target
  `X-OpenSVC-Node`) mapped to authorized
  HTTPS daemon origins. No credentials, API paths, queries or fragments.
- `tls.ca_file`: optional absolute public CA bundle path for daemon TLS.
  Omit `tls` for the system CA roots, for example with Let's Encrypt.
  An explicit bundle replaces, rather than extends, system roots.
- `tls.insecure`: optional boolean, default `false`. Setting it to `true`
  disables daemon certificate chain and hostname/IP verification for demos only.
  It cannot be combined with `tls.ca_file`.
- `request_timeout`: required duration between `1s` and `2m`.

The daemon verifies native/OpenID JWT signatures itself. Neither JWT public keys
nor daemon private signing keys belong on the MCP host. The optional TLS CA
bundle validates only the daemon HTTPS certificate chain.

Issuer names need not be DNS-resolvable: they are catalogue keys. With TLS
verification enabled, an IP endpoint works if its certificate covers that IP.
A DNS-only certificate requires an endpoint using that DNS name.

### Demo-only TLS bypass

For a demo with an untrusted certificate or a hostname/IP mismatch, set this
inside the target cluster entry, without `ca_file`:

```yaml
tls:
  insecure: true
```

**Strongly discouraged in production.** The connection remains encrypted, but
the daemon is no longer authenticated: an intermediary could steal the JWT,
forge daemon responses and impersonate a user through `whoami`. The MCP logs a
startup warning for each affected cluster. The default is `false`.

This applies to MCP-to-daemon requests, including `whoami`, only. It does not
change listener TLS, agent-to-MCP TLS, HTTPS-only endpoints, origin binding or
JWT checks. Restart the MCP after changing the catalogue.

### Routing and validation

For native JWTs, `cluster_id` selects a configured cluster and `iss` selects
exactly one configured node. An optional `X-OpenSVC-Cluster-ID` header must
match the native claim. An optional `X-OpenSVC-Node` header must match `iss`;
neither header can override the emitting daemon.

For OpenID, the required `X-OpenSVC-Cluster-ID` header selects the cluster's
`expected_cluster_id`; the required `X-OpenSVC-Node` header selects exactly
one entry in that cluster's `nodes`. No node is inferred, even if there is only
one configured. The provider's `iss` and `aud` do not select an endpoint.
That daemon must already
be configured to validate the token's OpenID issuer and audience.

Claims and HTTP targets remain unverified until the daemon authenticates the
token; they cannot establish local identity. There is no first-endpoint
fallback, failover, discovery or retry on a different node, including after
an authentication refusal or outage. Administrators must ensure endpoints
belong to the declared cluster and node. Adding a catalogue entry does not
register or modify a daemon.

### Validation and bounds

The strict loader rejects unknown fields, duplicate keys, multiple documents,
invalid scalar types, duplicate cluster IDs, duplicate node origins within a
cluster, empty catalogues and unsupported versions. Equivalent HTTPS origins
are normalized. Cluster references contain 1–64 ASCII letters, digits, hyphens
or underscores. Names contain 1–128 bytes, IDs and issuer names 1–256 bytes,
without surrounding whitespace or control characters.

Limits are 4 MiB of YAML, 4096 clusters and 200 nodes per legacy cluster. Trust files
must be readable regular files of at most 1 MiB. TLS bundles accept only valid
public CA certificates, not leaf certificates.
Changing these files does not alter the running snapshot.

## Runtime account

Run `om3-mcp` under a dedicated unprivileged account, for example
`opensvc-mcp:opensvc-mcp`. It must be able to read the listener certificate/key,
catalogue, confidential secret file and public trust files. Restrict private-key access to that account
and use an unprivileged port such as `8443`.

An OpenSVC `app.simple` resource can manage the process with the environment
variables above. The binary does not load an environment file itself and does
not require systemd, a local OpenSVC daemon, its binary, user objects or private
signing keys.
