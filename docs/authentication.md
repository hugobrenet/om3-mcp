# Authentication

[Back to README](../README.md) · [Configuration](configuration.md)

## External-client OAuth: stage 2

The HTTPS `/mcp` endpoint is an OAuth resource server. It accepts access JWTs
issued for its configured resource URL and verifies them locally. No business
scope is required, advertised or used to filter tools. SSO-issued OpenSVC grants
will be enforced by daemons after token exchange is implemented.

This stage supports authenticated MCP initialization, ping and discovery of all
existing tools. Daemon tool calls return `isError=true` with an explicit
"token exchange is not implemented yet" message. They do not contact daemons.
`GET /mcp/auth/whoami` requires OAuth but returns HTTP 501: the old daemon
identity bridge is disconnected. `list_clusters`, catalogue version 3 and
per-tool multi-cluster targets belong to subsequent stages.

The previous `om ai` / webapp delegation middleware is retained in source with
its production wiring commented out in `main.go`. Its regression tests use a
test-only handler. It is never an authentication fallback. Existing daemon
JWTs and the old chatbot identity flow no longer work at this endpoint.
See [legacy contracts](authentication-legacy.md) for those components.

## Resource discovery and login

Configure the public resource URL explicitly; it is not inferred from Host or
forwarding headers. For the lab it is:

```text
https://dev5-vip.opensvc.com:8443/mcp
```

Both `GET /.well-known/oauth-protected-resource/mcp` and
`GET /.well-known/oauth-protected-resource` return public RFC 9728 metadata:

```json
{
  "resource": "https://dev5-vip.opensvc.com:8443/mcp",
  "resource_name": "OpenSVC Daemon MCP",
  "authorization_servers": ["https://auth.example.test/application/o/opensvc-mcp/"],
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
WWW-Authenticate: Bearer resource_metadata="https://dev5-vip.opensvc.com:8443/.well-known/oauth-protected-resource/mcp"
```

Invalid credentials also add `error="invalid_token"`. Error bodies are generic
`application/problem+json`, with `Cache-Control: no-store`; no token or upstream
response appears in them.

The external client discovers the SSO and performs Authorization Code with
PKCE there. Configure its client registration, allowed callbacks and token
claims in the SSO. MCP does not host login, callback, registration or token
endpoints. It does not need an OAuth client secret for this stage.

The SSO must issue an **access token**, whose `aud` contains the exact configured
MCP resource URL. An audience array may additionally contain the confidential
MCP client ID needed by some token-exchange configurations. An ID token intended
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

## Validation before deployment

Local tests exercise a real HTTPS fake issuer, signed JWTs, rotation, rejection,
resource metadata, the MCP SDK and the compiled server. They do not prove that
the customer's SSO is configured correctly. Validate the real issuer, client
registration, callbacks and resource audience, then connect the external client.

On 2026-10-07, Codex CLI 0.160.1 successfully logged in through the lab authentik
provider and discovered the MCP tools. No daemon tool call was requested in that
manual test. The audience mapping must be selected on the provider and use the
scope name `openid`, matching the scope requested by this Codex configuration.
A provider preview and a successful login alone did not establish that the
access token had the MCP audience; this was confirmed by successful tool
discovery after correcting the mapping's scope name.
See the [integration plan](CHANTIER_MCP_AGENTS_EXTERNES.md) for the lab setup.

Token exchange and daemon grants are a separate end-to-end validation in stage 3.

References: [MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization),
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html).
