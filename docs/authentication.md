# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## Unix socket

Every MCP HTTP request requires:

```text
Authorization: Bearer <OpenSVC access JWT>
```

The MCP verifies the JWT with the configured public cluster CA or RSA key,
using RS256 and valid `exp`, `sub`, `iss` and `token_use=access` claims.
The subject is bound to the MCP session. The JWT remains request-scoped and is
forwarded to the daemon, which validates it and enforces the caller's grants.

Local mode accepts no Basic authentication or fallback service credential.
JWT creation and refresh remain the client's responsibility. See the
[tool contracts](tools/README.md#authentication-and-visibility) for visibility
and authorization details.

## HTTPS login prototype

Remote authentication is integrated into the MCP. The current flow is:

1. The agent discovers OAuth metadata and registers a public client using DCR.
2. `/authorize` validates the request and redirects the browser to `/login`.
3. The user selects a configured cluster and supplies OpenSVC credentials.
4. The MCP authenticates with the selected daemon and displays a confirmation.

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

**The flow stops at browser confirmation.** Authorization codes, MCP tokens,
the OAuth callback, refresh and remote tool access are not available yet.
`/mcp` remains `401` and `/token` returns `503` even after successful login.
The client waits for its callback until the command is cancelled.

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

Open the complete authorization URL printed by the CLI. After the OpenSVC
confirmation, stop the waiting command with Ctrl+C. The `/login` URL is not
an OAuth callback URL. A process restart loses pending requests and sessions;
start a new login afterwards.

### Routes

| Route | Behavior |
|---|---|
| `/mcp` | `401` Bearer challenge with protected resource metadata |
| `/.well-known/oauth-protected-resource/mcp` | MCP resource, authorization server and `mcp:access` scope; also available without the `/mcp` suffix |
| `/.well-known/oauth-authorization-server` | Integrated issuer, endpoints and PKCE S256; marked `opensvc_login_prototype` |
| `/register` | POST DCR for public clients, without a client secret |
| `/authorize` | GET validates client, callback, resource, scope, state and PKCE, then redirects to `/login` |
| `/login` | GET form or confirmation; POST OpenSVC authentication |
| `/token` | POST returns `503 temporarily_unavailable`; no token issuance |

Only HTTP loopback callbacks are accepted. For literal loopback IPs, the port
may vary, but the host, path and query must match the registered URI.
`localhost` callbacks must match exactly. HTTPS web callbacks, custom schemes,
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
rejected before reading credentials. `/login` uses `Referrer-Policy:
same-origin` to preserve the origin of native form POSTs; `/authorize` uses
`no-referrer` to keep its OAuth query out of referrers.

The server stores at most 256 clients for 24 hours, 256 authorization requests
for 10 minutes and 256 sessions until JWT expiry. Each request permits five
login attempts, one at a time, with at most 16 concurrent exchanges per process.
These limits do not provide a global account lockout. Expired state is pruned
on subsequent access. Restart or failover loses in-memory state; endpoint
failover and session continuity are not available yet.
