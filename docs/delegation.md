# Token delegation over the Unix socket

[Back to README](../README.md) · [Configuration](configuration.md) · [Authentication](authentication.md)

OpenSVC components such as the AI agent behind `om ai` reach the MCP through a
local Unix socket. They forward a daemon-issued token unchanged, with the ID of
the cluster that issued it. The daemon of that cluster verifies the token and
enforces its grants. No SSO is involved.

```text
om ai ── HTTPS ──> AI agent ── Unix socket ──> MCP ── HTTPS ──> cluster VIP daemon
```

The socket is a separate listener with its own routes. The HTTPS listener never
accepts daemon-issued tokens, and the socket never accepts OAuth tokens.

## Installation

1. Write the catalogue, see [configuration](configuration.md#cluster-catalogue-and-exchange-profiles).
   The `auth` block is not needed for the socket.
2. Set `OPENSVC_MCP_CLUSTER_CONFIG_FILE` and `OPENSVC_MCP_DELEGATED_SOCKET`, for
   example `/run/opensvc-mcp/delegated.sock`.
3. Run the MCP and the AI agent on the same host, typically as two resources of
   one OpenSVC service, and point the agent's `OPENSVC_AI_MCP_SOCKET` to the
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
Authorization: Bearer <daemon-issued access token>
X-OpenSVC-Cluster-ID: <cluster_id from the catalogue>
```

Both are required. The header selects the catalogue entry, and therefore the
VIP that receives the token. It is not trusted as an identity: a token issued
by another cluster is refused by the selected daemon. `X-OpenSVC-Node` is
accepted and ignored. Tokens in query strings are refused.

The MCP checks the token shape before any daemon call, without verifying its
signature:

- **native token** (`token_use` claim): RS256, `token_use=access`, `iss`,
  `sub` and an unexpired `exp`;
- **OpenID token**: an asymmetric algorithm, `kid`, `aud`, `iss`, `sub` and an
  unexpired `exp`. The expected username is `preferred_username`, then
  `email`, then `sub`, as selected by the daemon.

A missing header, an unknown cluster or a malformed token returns 401 without
contacting a daemon.

## Routes

### `GET /mcp/auth/whoami`

Proves the token by relaying it to the selected daemon's
`GET /api/auth/whoami`. The daemon must authenticate it with the expected
strategy (`jwt` or `jwt-openid`) and username. The response is:

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
never returned.

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
