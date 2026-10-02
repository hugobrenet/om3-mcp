# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## Native OpenSVC delegation

The MCP accepts a native OpenSVC access JWT on each HTTPS request:

```text
om ai obtains JWT from its daemon
  -> agent validates JWT through GET /mcp/auth/whoami
  -> MCP selects its emitting daemon and calls GET /api/auth/whoami
  <- authenticated native identity, or refusal
  -> agent forwards the same JWT to /mcp for tools
  -> MCP calls that daemon with the unchanged JWT
  <- tool results, then the agent's answer
```

There is no second credential, token exchange, consent page, user password
handling or authorization-server state in the MCP. The agent supplies
`Authorization: Bearer <native OpenSVC JWT>` outside tool arguments and LLM
messages. The MCP delegates that exact token to the configured daemon.

This is a native OpenSVC authentication profile for MCP Streamable HTTP, not
the MCP OAuth authorization profile. Generic clients requiring OAuth discovery
and login cannot use it as-is. Clients able to supply this native bearer can
call the tools. Webapp/OpenID integration is not implemented in this increment;
an Authentik token is not automatically a native OpenSVC access token.

## Verification and target selection

The required claims are:

| Claim | Meaning |
|---|---|
| `cluster_id` | Exact cluster ID, matching one unique catalogue entry |
| `iss` | Emitting daemon node name, matching a node in that cluster |
| `sub` | Nonempty OpenSVC user identity |
| `exp` | Required future expiry time |
| `token_use` | Must be `access`, not `refresh` |

Only RS256 is accepted. `nbf` is checked when present. A token is bounded to
32 KiB. The MCP checks token structure, required claims and dates, but does
not verify its signature locally. Unverified cluster ID and issuer select only
an administrator-configured daemon, never an authenticated local identity. No token
header URL, issuer URL, client-supplied endpoint or embedded key can add a
trusted authority or target. Missing IDs and unknown clusters/nodes fail
closed; there is no fallback to the first configured cluster.

Only the daemon verifies the JWT signature, using its existing native
authentication. Neither MCP nor agent needs a JWT signing public key. The daemon
HTTPS connection verifies its certificate chain and hostname/IP by default,
using system roots or the cluster's explicit TLS CA bundle.

The [demo-only `tls.insecure` option](configuration.md#demo-only-tls-bypass)
disables these peer checks. It is strongly discouraged in production: an
intermediary could steal the JWT or forge `whoami`, so the returned identity
cannot be trusted without authenticated daemon TLS.

The unchanged JWT carries OpenSVC grants. The MCP does not widen or reissue
them. The daemon remains authoritative for API permission checks and namespace
visibility. A valid JWT is not a promise that every tool is permitted.
Native OpenSVC JWT revocation and changes of rights retain the daemon's semantics.

## Identity validation bridge

`GET /mcp/auth/whoami` is a dedicated HTTPS route, not an MCP tool, token
exchange or authorization server. The agent sends the unchanged native JWT in
the Bearer header. The MCP uses the catalogue to call exactly
`GET /api/auth/whoami` on the selected daemon with that same JWT. It requires
the daemon's native `jwt` strategy and a returned username matching `sub`.

Only after daemon authentication does it return a bounded JSON identity:
`cluster_id`, `issuer`, `subject`, `expires_at`. It does not forward grants,
raw daemon responses or credentials. Queries and request bodies are refused.
Daemon authentication refusals return generic 401/403; daemon outages, malformed
responses and other upstream failures return generic 502.

The agent calls this route before every protected API operation, including
conversation creation, listing, resumption, renaming, deletion and turns.
Authenticated conversation ownership is `cluster_id + issuer + subject`.
There is no identity cache. The agent returns 401 for invalid credentials and
503 if validation is unavailable, without reading SQLite or starting the LLM.

Local MCP checks alone are not authentication: a well-shaped forged JWT can
reach initialization and public tool metadata. It cannot authorize private
daemon data, nor establish an agent conversation owner. Every protected daemon
API call still authenticates the token independently.

## Request lifetime and isolation

Credentials live only in the checked delegation request context, never a persistent
session, catalogue, shared client, connection pool or token database.
Every request is checked independently; protected daemon calls authenticate it. Usernames and issuer names may
be identical across clusters without changing the signed cluster binding.
A protocol session ID cannot select a different identity or target.

The selected transport sends the bearer only to the exact configured HTTPS
origin for that declared cluster/node. Environment proxies, redirects and
cross-origin Host overrides are not allowed. No failover silently sends the
token to another daemon. Shared TLS connection pools do not store credentials.
Requests are cancelled at JWT expiry; expired credentials cannot start a
daemon request.

MCP uses stateless Streamable HTTP with JSON responses. Request bodies are
bounded to 1 MiB, and cross-origin browser requests are refused.
Secrets must stay out of tool arguments, responses, errors and logs.
As with any bearer token, a holder can replay it until it expires; HTTPS and
restricted access to the agent and MCP are essential.

## Errors and renewal

Missing, malformed, expired or unknown-target tokens return a
generic HTTP `401` and `WWW-Authenticate: Bearer`, without authorization-server
discovery metadata. Only one bearer Authorization header is accepted.
Cookies, passwords, refresh tokens and query-string access tokens are not
alternative credentials.

Daemon refusals remain tool errors, with `isError=true`, HTTP status and bounded
RFC 7807 title/detail. Never retry a denied call using stronger credentials.

The client must obtain a fresh native access token from its daemon when
needed, including subsequent chat turns. The MCP does not renew tokens.
Restarting the MCP does not invalidate otherwise valid native JWTs; it reloads
the catalogue and trust files. No shared authorization state is needed for
multiple MCP instances with consistent configuration.
