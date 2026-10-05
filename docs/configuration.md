# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8443 | TCP bind address, IPv4:port or [IPv6]:port |
| OPENSVC_MCP_TLS_CERT_FILE | empty | Absolute server certificate PEM path; required |
| OPENSVC_MCP_TLS_KEY_FILE | empty | Absolute server private key PEM path; required |
| OPENSVC_MCP_CLUSTER_CONFIG_FILE | empty | Absolute YAML catalogue path; required |

HTTPS is the only transport. There is no local socket mode, local daemon
dependency, login page or token exchange. Target configuration is loaded from
the administrator-owned catalogue, not from tool arguments or token URLs.

## HTTPS

Install the listener certificate and key, catalogue and any private TLS CA
bundles before starting. No JWT signing key is installed on the MCP:

```bash
OPENSVC_MCP_LISTEN_ADDR=0.0.0.0:8443 \
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-mcp/tls/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-mcp/tls/server.key \
OPENSVC_MCP_CLUSTER_CONFIG_FILE=/etc/opensvc-mcp/clusters.yaml \
  ./bin/om3-mcp
```

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

## Target cluster catalogue

See [the template](../deploy/examples/clusters.yaml). Install real configuration
outside the repository, for example at `/etc/opensvc-mcp/clusters.yaml`.

```yaml
version: 2
clusters:
  cluster-a:
    name: Example cluster
    expected_cluster_id: 00000000-0000-4000-8000-000000000001
    default_node: node-a
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
- `default_node`: exact key in `nodes`, used for OpenID delegation. Optional
  for native-only clusters. If omitted, OpenID requests to this cluster are
  refused, even when only one node is configured.
- `nodes`: exact daemon node names (native JWT `iss`) mapped to authorized
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
match the native claim and cannot override its node.

For OpenID, the required `X-OpenSVC-Cluster-ID` header selects the cluster's
`expected_cluster_id`; its `default_node` selects exactly one daemon. The
provider's `iss` and `aud` do not select an endpoint. That daemon must already
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

Limits are 256 KiB of YAML, 64 clusters and 200 nodes per cluster. Trust files
must be readable regular files of at most 1 MiB. TLS bundles accept only valid
public CA certificates, not leaf certificates.
Changing these files does not alter the running snapshot.

## Runtime account

Run `om3-mcp` under a dedicated unprivileged account, for example
`opensvc-mcp:opensvc-mcp`. It must be able to read the listener certificate/key,
catalogue and public trust files. Restrict private-key access to that account
and use an unprivileged port such as `8443`.

An OpenSVC `app.simple` resource can manage the process with the environment
variables above. The binary does not load an environment file itself and does
not require systemd, a local OpenSVC daemon, its binary, user objects or private
signing keys.
