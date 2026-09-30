# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## HTTPS authorization

Remote authentication is integrated into the MCP. The current flow is:

1. The agent discovers OAuth metadata and registers a public client using DCR.
2. `/authorize` validates the request and redirects the browser to `/login`.
3. The user selects a configured cluster and supplies OpenSVC credentials.
4. The MCP validates the daemon JWT and the cluster identity.
5. The user explicitly authorizes or refuses the declared application.
6. The browser returns to the validated callback with `code`, `state` and `iss`
   (or `error=access_denied`).
7. The agent exchanges the code and PKCE verifier on `/token` for a MCP token.
8. The agent uses that token to call the tools on `/mcp`.

The username identifies `system/usr/<username>` on the target cluster. The
object's `password` key supplies the credential and `DEFAULT.grant` supplies
permissions. Ensure the user is synchronized to the configured daemon nodes.
For example, with a fictitious read-only identity:

```bash
sudo om system/usr/reader create --kw 'grant=guest:example' --kw 'nodes=*'
IFS= read -rs -p 'OpenSVC password: ' opensvc_password
printf '%s' "$opensvc_password" | sudo om system/usr/reader key add password --from /dev/stdin
unset opensvc_password
```

The password is used transiently. The verified OpenSVC JWT stays in MCP process
memory, bound to the cluster, user and OAuth request. The browser receives an
opaque Secure, HttpOnly, SameSite=Lax cookie; no JWT or password is sent to the
agent or language model.

The application name and ID are self-declared through DCR. The consent page
shows the selected cluster and authenticated user; authorize only an application
whose connection you just initiated. Consent is required for each request.
It allows the implemented tools with the user's OpenSVC rights, including the
explicit status refresh when the daemon grants it. No prior browser session
silently authorizes a new client or cluster.

### Codes, tokens and cluster binding

Codes and MCP tokens are opaque random 256-bit values encoded as 43 base64url
characters. The server indexes them by SHA-256 hash, without signing keys.
A code lasts at most 60 seconds and is consumed atomically once. Its exchange
requires the same client ID, the exact callback used on `/authorize`, the
canonical `/mcp` resource and a PKCE S256 verifier of 43–128 allowed ASCII
characters. Wrong proofs do not create a token; replay returns `invalid_grant`.

A MCP token is bound to the client, user, selected cluster, canonical resource
and `mcp:access` scope. It expires with the native daemon JWT (requested for
10 minutes), with no refresh token issued. Token validity is checked on every
request. Only `Authorization: Bearer <MCP token>` is accepted; a daemon JWT,
browser cookie, code or query-string token does not authorize the tools.
Expired credentials require a new login.

Each grant has its own tools and HTTPS daemon client, using its server-held JWT
and selected cluster's CA. No inbound Authorization header is forwarded to a
daemon. The connection uses the first configured endpoint, without environment
proxies, redirects or TLS bypasses. The daemon enforces grants and namespace
visibility on its API calls; its JWT semantics determine when changes of rights
take effect. This increment does not re-read the user object on every tool call.

MCP uses Streamable HTTP with JSON responses and no persistent protocol session.
Authorization is carried by the bearer token on every request; a protocol
session ID cannot change the user or target cluster. MCP request bodies are
bounded to 1 MiB, and requests are cancelled at token expiry. Browser requests
with a foreign or null Origin are refused.

### Client setup and TLS trust

With Codex CLI, register the MCP URL and start DCR authentication:

```bash
export CODEX_CA_CERTIFICATE=/path/to/mcp-ca.pem
codex mcp add opensvc --url https://127.0.0.1:8443/mcp
codex mcp login opensvc --oauth-client-registration dcr --no-browser
```

Replace the URL with the configured client-facing origin followed by `/mcp`.
For private CAs, configure trust in Codex and the browser separately. Codex
supports `CODEX_CA_CERTIFICATE`, falling back to `SSL_CERT_FILE` when unset:
[official documentation](https://learn.chatgpt.com/docs/auth#custom-ca-bundles).

Open the complete authorization URL printed by the CLI, authenticate, then
choose **Autoriser et revenir à l’agent**. With `--no-browser`, paste the complete
callback URL if the CLI asks for it, including when the loopback page cannot
load. Do not paste the `/login` URL. The CLI completes its token exchange; the
agent can then reconnect and use the tools.

A process restart or failover loses tokens and pending state; start a new login
afterwards. Shared state and transparent HA are not implemented yet.

### Routes

| Route | Behavior |
|---|---|
| `/mcp` | Protected tools; `401` challenge when the MCP token is missing, invalid or expired |
| `/.well-known/oauth-protected-resource/mcp` | MCP resource, authorization server and `mcp:access` scope; also available without the `/mcp` suffix |
| `/.well-known/oauth-authorization-server` | Integrated issuer, endpoints, PKCE S256 and issuer identification in the callback |
| `/register` | POST DCR for public clients, without a client secret |
| `/authorize` | GET validates client, callback, resource, scope, state and PKCE, then redirects to `/login` |
| `/login` | GET login or consent page; POST OpenSVC authentication |
| `/consent` | POST explicit allow/deny decision with same-origin and CSRF checks |
| `/token` | POST public-client Authorization Code exchange; emits an opaque MCP bearer token |

Only HTTP loopback callbacks are accepted. For literal loopback IPs, the port
may vary, but the host, path and query must match the registered URI.
`localhost` callbacks must match exactly. Callback query strings must be valid
and must not contain reserved OAuth response parameters (`code`, `state`,
`iss`, `error`, `error_description`, `access_token`). HTTPS web callbacks, custom schemes,
confidential clients and CIMD are not supported. The registered grant is
`authorization_code`.

### Daemon authentication and session controls

Login uses the **first configured endpoint only** and the selected cluster's
loaded CA bundle. The transport requires verified TLS, without environment
proxies, redirects or verification bypasses. The MCP requests an access JWT
with `refresh=false` and a 10-minute duration, validates its RS256 signature,
claims and exact subject, then verifies `cluster.config.id` against the
catalogue. Both calls share the configured exchange timeout.

POST `/login` requires a live request cookie, the canonical `Origin`, a CSRF
token and a bounded URL-encoded form. Missing, null or foreign origins are
rejected before reading credentials. Consent applies the same Origin check,
with an independent CSRF token, and consumes its browser session after the
decision. `/login` uses `Referrer-Policy:
same-origin` to preserve the origin of native form POSTs; `/authorize` uses
`no-referrer` to keep its OAuth query out of referrers. The consent page permits
only its validated callback origin in CSP `form-action` so browsers can follow
the native form redirect. Callback responses use `no-referrer` and `no-store`.

The server stores at most 256 clients for 24 hours, 256 authorization requests
for 10 minutes, 256 pending consent sessions for at most 5 minutes, 256 codes
for at most 60 seconds and 256 MCP tokens until JWT expiry. Each request permits five
login attempts, one at a time, with at most 16 concurrent exchanges per process.
These limits do not provide a global account lockout. Expired state is pruned
on subsequent access. Restart or failover loses in-memory state; endpoint
failover and session continuity are not available yet. No revocation or logout
endpoint exists; access expires automatically, and a restart invalidates all tokens.

### Protocol references

This flow follows [OAuth Authorization Code](https://www.rfc-editor.org/rfc/rfc6749#section-4.1),
[PKCE S256](https://www.rfc-editor.org/rfc/rfc7636#section-4.6) and
[MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).
It implements public native clients with DCR and loopback callbacks; it does not
claim support for every OAuth client type or extension.
