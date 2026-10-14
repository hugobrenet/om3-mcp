# Token delegation over the Unix socket

[Back to README](../README.md) · [Configuration](configuration.md) · [Authentication](authentication.md)

OpenSVC components such as the AI agent used by the webapp reach the MCP
through a local Unix socket. They forward the OpenID Connect access token of
the user unchanged, with the ID of the cluster it is intended for: the webapp
obtains it by signing in with the OpenID provider the daemons of the cluster
trust. The daemon of that cluster verifies the token and enforces its grants.
Only OpenID Connect access tokens are accepted.

```text
webapp ── HTTPS ──> AI agent ── Unix socket ──> MCP ── HTTPS ──> cluster VIP daemon
```

The socket is a separate listener with its own routes. The HTTPS listener never
accepts delegated tokens, and the socket never accepts the OAuth tokens of the
MCP resource.

## Installation

1. Write the catalogue, see [configuration](configuration.md#cluster-catalogue-and-exchange-profiles).
   The `auth` block is not needed for the socket.
2. Set `OPENSVC_MCP_CLUSTER_CONFIG_FILE` and `OPENSVC_MCP_DELEGATED_SOCKET`, for
   example `/run/opensvc-mcp/delegated.sock`.
3. Run the MCP and the AI agent on the same host, typically as two resources of
   one OpenSVC service, and point the agent's `OPENSVC_AI_OM3_MCP_SOCKET` to the
   socket.

## Socket access

Access is controlled by the filesystem. The MCP creates the socket with mode
`0660`, owned by its runtime account and group. Restrict the parent directory
to the MCP account and a group shared with the agent account, for example:

```bash
install -d -m 2750 -o opensvc-mcp -g opensvc-ai /run/opensvc-mcp
```

With the setgid bit, the socket inherits the `opensvc-ai` group. Any process
that can connect to the socket can submit tokens; the daemon still verifies
them.

At startup, a socket file left by a stopped process is replaced. A socket with
a live listener, or any other file at the path, prevents startup. The socket is
removed on shutdown.

## Request contract

Every request carries:

```http
Authorization: Bearer <OpenID Connect access token>
X-OpenSVC-Cluster-ID: <cluster_id from the catalogue>
```

Both are required. The header selects the catalogue entry, and therefore the
VIP that receives the token. It is not trusted as an identity: a token
intended for another cluster is refused by the selected daemon. `X-OpenSVC-Node` is
accepted and ignored. Tokens in query strings are refused. An optional
`X-Request-ID`, the request ID of the AI agent, is kept in the
[audit lines](token-exchange.md#audit-trail) when it is 32 lowercase
hexadecimal characters; it is never an identity.

The MCP verifies the token before any daemon request, as the daemon itself
does: an asymmetric algorithm, `kid`, `iss`, `sub`, an unexpired `exp`, and a
signature by a key of the OpenID provider the daemon of the selected cluster
accepts, for its client ID as audience. The expected username is
`preferred_username`, then `email`, then `sub`, as selected by the daemon.

The MCP learns that provider from the public `GET /api/auth/info` of the
daemon, which names its OpenID issuer and client ID: no trust is configured
in the MCP. It reads it, without any token, the first time a token of the
cluster needs it, then keeps it 15 minutes; concurrent tokens share one read,
and a failed read is retried after 10 seconds at the earliest, the last answer
serving meanwhile. The keys of the issuer are discovered and cached as on the
HTTPS listener, with the system roots or `OPENSVC_MCP_OAUTH_CA_FILE`. However
many tokens arrive, the daemon is read at most once per cluster and period.

A missing header, an unknown cluster, a malformed token, or a token the
provider of the cluster did not sign for that cluster returns 401 without
reaching the daemon. A daemon that accepts no OpenID token accepts no token of
the socket. When the provider of the cluster cannot be read, or its keys
loaded, the socket returns 503 with `Retry-After`.

## Routes

### `GET /mcp/auth/whoami`

Proves the token by relaying it to the selected daemon's
`GET /api/auth/whoami`. The daemon must authenticate it as an OpenID Connect
token (`jwt-openid`) with the expected username. The response is:

```json
{
  "cluster_id": "<X-OpenSVC-Cluster-ID>",
  "issuer": "<JWT iss>",
  "subject": "<JWT sub>",
  "expires_at": "<JWT exp as RFC3339>"
}
```

A daemon refusal returns 401 or 403; an unavailable daemon or an unexpected
response returns 502. Query strings and request bodies are refused. Grants are
never returned by this route; the `get_caller_identity` tool reports the
grants of the caller's own token.

### `/mcp`

Stateless Streamable HTTP MCP, like the HTTPS listener. Every daemon request of
a tool call carries the caller's token to the selected cluster VIP. Tools take
no `cluster_id` argument and `list_clusters` is not offered: the header binds
each request to one cluster. Each request is authenticated again.

## Trust and limits

- Tokens live only in the request context and are never logged, stored or
  returned. The request deadline is bounded by the token expiry.
- Daemon requests follow no redirects or proxies and are bound to the
  configured VIP origin.
- Each tool call emits the [audit line](token-exchange.md#audit-trail) with
  `exchange=delegated`. Its `issuer` and `subject` come from the token claims,
  verified by the daemon on each call; a refused call may log unverified claims.
- Each `GET /mcp/auth/whoami` emits one `mcp whoami`
  [audit line](token-exchange.md#audit-trail) with its outcome.
