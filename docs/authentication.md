# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## External-client OAuth and daemon token exchange

The HTTPS `/mcp` endpoint is an OAuth resource server. It accepts access JWTs
issued for its configured resource URL and verifies them locally. No business
scope is required, advertised or used to filter tools. SSO-issued OpenSVC grants
are enforced by daemons using the exchanged tokens.

Authenticated clients can initialize MCP and discover all existing tools.
With a version 3 catalogue and SSO exchange profiles, `list_clusters` discovers
configured cluster identities and each daemon tool requires `cluster_id`.
Before the tool executes, the MCP exchanges the incoming token for a daemon
access token and binds it to the configured VIP for the call lifetime.
See [token exchange](token-exchange.md) for configuration.
Without exchange configuration, daemon calls return an explicit tool error.
`GET /mcp/auth/whoami` still returns HTTP 501 after OAuth authentication: the
legacy identity bridge remains disconnected.

The previous `om ai` / webapp delegation middleware is retained in source with
its production wiring commented out in `main.go`. Its regression tests use a
test-only handler. It is never an authentication fallback. Existing daemon
JWTs and the old chatbot identity flow no longer work at this endpoint.
See [legacy contracts](authentication-legacy.md) for those components.

## Resource discovery and login

Configure the public resource URL explicitly; it is not inferred from Host or
forwarding headers, for example:

```text
https://mcp.example.com:8443/mcp
```

Both `GET /.well-known/oauth-protected-resource/mcp` and
`GET /.well-known/oauth-protected-resource` return public RFC 9728 metadata:

```json
{
  "resource": "https://mcp.example.com:8443/mcp",
  "resource_name": "OpenSVC Daemon MCP",
  "authorization_servers": ["https://sso.example.com/oauth/opensvc-mcp/"],
  "bearer_methods_supported": ["header"]
}
```

`resource_name` defaults to `OpenSVC Daemon MCP` and can be configured through
`OPENSVC_MCP_OAUTH_RESOURCE_NAME`. No `resource_documentation` is published.

The issuer above is illustrative: configure the actual issuer for MCP tokens,
not the daemon provider just because it exists. `scopes_supported` is omitted.
HEAD is supported; other methods are rejected. Health and metadata require no
credentials and perform no SSO or daemon requests.

An unauthenticated `/mcp` request receives HTTP 401 and:

```http
WWW-Authenticate: Bearer resource_metadata="https://mcp.example.com:8443/.well-known/oauth-protected-resource/mcp"
```

Invalid credentials also add `error="invalid_token"`. Error bodies are generic
`application/problem+json`, with `Cache-Control: no-store`; no token or upstream
response appears in them.

The external client discovers the SSO and performs Authorization Code with
PKCE there. Configure its client registration, allowed callbacks and token
claims in the SSO. MCP does not host login, callback, registration or token
endpoints. It does not need an OAuth client secret for this stage.

The SSO must issue an **access token**, whose `aud` contains the exact configured
MCP resource URL. An audience array may additionally contain client IDs needed by the login or
token-exchange configuration; some SSO products require the confidential
exchange client in the subject token audience. An ID token intended
for the external OAuth client is not the credential to send to MCP.

## Incoming-token validation

Every protected HTTP request supplies exactly one `Authorization: Bearer`
header. Cookies, Basic authentication and query-string access tokens are not
alternative authentication methods. Tokens are bounded to 32 KiB.

The verifier checks:

- exact configured `iss` and an `aud` containing the configured resource URL;
- a nonempty bounded `sub`, a required unexpired `exp`, and `nbf` / `iat` when present;
- a nonempty `kid` and an asymmetric signature (RS256/384/512, PS256/384/512,
  ES256/384/512), using the issuer's published signing keys;
- JWK use/key operations and the declared algorithm when present.

RSA keys must be 2048–8192 bits. Supported EC curves are P-256, P-384 and P-521.
There is no HMAC, `none`, opaque-token introspection or scope-based authorization.
Key URLs and embedded keys from JWT headers are never trusted.

Public keys are discovered lazily from the administrator-configured issuer's
`/.well-known/openid-configuration`. If that document returns 404, the verifier
tries RFC 8414 metadata with the well-known path inserted before the issuer
path. The returned `issuer` must exactly match configuration; `jwks_uri` must
use HTTPS. Public documents are bounded to 1 MiB and the JWKS to 128 entries.

SSO HTTPS uses system CA roots or `OPENSVC_MCP_OAUTH_CA_FILE`. No redirects or
environment proxies are followed, and no caller credential is sent on discovery
or JWKS requests. Daemon `tls.insecure` has no effect on SSO TLS.

## Lifetime, rotation and failures

Only public signing keys are cached, for five minutes. An unknown `kid` may
trigger an earlier refresh, at most once per 30 seconds across callers. A new
key published immediately after a fetch can therefore require up to 30 seconds
before acceptance. Concurrent requests share the same refresh.

During an SSO outage, valid cached keys remain usable until their TTL. Expired
keys are not used after a failed refresh. A metadata/JWKS failure returns HTTP
503 with `Retry-After: 30`, without a login challenge. Invalid tokens or unknown
keys after a successful JWKS fetch return 401.

JWTs are cryptographically verified on every request. This is not online token
introspection: user/session revocation and grant changes are not immediate;
a previously issued token can remain usable until expiry. Configure access-token
lifetimes in the SSO accordingly. The external client handles renewal; MCP does
not store refresh tokens or implement renewal.

Validated identity and the incoming token live only in a private request
context, separate from legacy daemon delegation. The request deadline is bounded
by token expiry. Incoming Authorization and legacy target headers are removed
before reaching the MCP protocol handler. Sessions and shared clients do not
store user credentials. Request bodies remain bounded to 1 MiB and cross-origin
browser requests remain refused by the MCP SDK.

## Client setup

MCP does not host a registration endpoint. Register a public client with PKCE
in the SSO, allow the loopback callbacks used by the external agents, and
configure the incoming audience mapping. Give users the MCP URL and that
`client_id`. An agent without a configured `client_id` falls back to the
registration mechanisms advertised by the SSO, if any.

No scope is advertised by MCP. Clients then request the scopes they default to,
or none; the SSO must still add the MCP resource URL to the access-token
audience. If the audience mapping depends on a technical scope, configure that
scope in the client. Request `offline_access` or the SSO equivalent when the
client should renew access without a new interactive login.

Validate the real issuer, client registration, callbacks and resource audience
by connecting an external client and listing the tools. A successful login alone
does not prove the access token carries the MCP audience; an audience mismatch
is reported as an invalid token by MCP.

References: [MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization),
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html).
