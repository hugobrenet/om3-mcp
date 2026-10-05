# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## OpenSVC delegation

The MCP accepts native OpenSVC and OpenID JWTs on each HTTPS request.
The native flow is:

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
`Authorization: Bearer <JWT>` outside tool arguments and LLM
messages. The MCP delegates that exact token to the configured daemon.

This is an OpenSVC delegation profile for MCP Streamable HTTP, not
the MCP OAuth authorization profile. Generic clients requiring OAuth discovery
and login cannot use it as-is. Clients able to supply the bearer and required
target can call the tools. The webapp chatbot and browser CORS remain separate
client work; this contract supports OpenID agent-to-MCP delegation.

## Verification and target selection

For native JWTs, the required claims are:

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
closed; there is no fallback to the first configured cluster. An optional
`X-OpenSVC-Cluster-ID` header must match the native `cluster_id` and cannot
override the emitting node.

For OpenID, the client sends these headers on whoami and every MCP request:

```http
Authorization: Bearer <OpenID JWT>
X-OpenSVC-Cluster-ID: <cluster.config.id>
```

The cluster ID matches one unique catalogue `expected_cluster_id`. Its
`default_node` must explicitly name an entry in `nodes`. No default is inferred,
even with one node. Provider `iss` and `aud` never select an endpoint; that
daemon must already accept the token's OpenID issuer and client audience.

OpenID requires nonempty `iss/sub/aud/exp`, a future expiry, an accepted
asymmetric algorithm (RS256/384/512, PS256/384/512 or ES256/384/512) and a
nonempty `kid` in the JWT header. Optional `nbf` is checked. Every audience
value must be nonempty and well-formed. Expected issuer/audience values and
the signature are checked by the daemon, not these local prechecks.

A nonempty `cluster_id` or `token_use` selects native checks; an incomplete
native token is refused without falling back to OpenID. A target header must
have exactly one nonempty value of at most 256 bytes, without surrounding
whitespace, control characters or commas. Duplicate/combined headers, unknown
targets and missing OpenID defaults are refused.

Only the daemon verifies the JWT signature, using its existing native/OpenID
authentication. Neither MCP nor agent needs a JWT signing public key. The daemon
HTTPS connection verifies its certificate chain and hostname/IP by default,
using system roots or the cluster's explicit TLS CA bundle.

The [demo-only `tls.insecure` option](configuration.md#demo-only-tls-bypass)
disables these peer checks. It is strongly discouraged in production: an
intermediary could steal the JWT or forge `whoami`, so the returned identity
cannot be trusted without authenticated daemon TLS.

The unchanged JWT carries OpenSVC grants (`grant` for native, `entitlements`
for OpenID). The MCP does not widen or reissue them. The daemon remains
authoritative for API permission checks and namespace
visibility. A valid JWT is not a promise that every tool is permitted.
Native OpenSVC JWT revocation and changes of rights retain the daemon's semantics.

## Identity validation bridge

`GET /mcp/auth/whoami` is a dedicated HTTPS route, not an MCP tool, token
exchange or authorization server. The agent sends the unchanged JWT in
the Bearer header. The MCP uses the catalogue to call exactly
`GET /api/auth/whoami` on the selected daemon with that same JWT. It requires
the strategy matching the checked profile: `jwt` for native, `jwt-openid` for
OpenID. Public/basic or mismatched JWT strategies cannot establish identity.
The returned username must match native `sub`, or for OpenID the first nonempty
`preferred_username`, `email`, then `sub`, matching om3's own selection.

Only after daemon authentication does it return a bounded JSON identity:
`cluster_id`, `issuer`, `subject`, `expires_at`. OpenID `subject` remains the
original opaque JWT `sub`, never the daemon username. `issuer` remains the
provider issuer, not the selected node. It does not forward grants,
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
Every request is checked independently; protected daemon calls authenticate it.
The selected node is distinct from the JWT issuer for OpenID. The HTTP target
header is consumed by MCP and is not forwarded to the daemon. Usernames and
issuer names may be identical across clusters: native routing uses the signed
cluster ID, while OpenID routing uses the explicit target and daemon validation.
A protocol session ID cannot select a different identity or target.

The selected transport sends the bearer only to the exact configured HTTPS
origin for that selected cluster/node. Environment proxies, redirects and
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

The client must obtain a fresh token through its existing daemon/IdP flow when
needed, including subsequent chat turns. OpenID clients provide the target
header on each operation. The MCP does not renew tokens.
Restarting the MCP does not invalidate otherwise valid JWTs; it reloads
the catalogue and trust files. No shared authorization state is needed for
multiple MCP instances with consistent configuration.
