# Token exchange and multi-cluster calls

The external client authenticates to MCP as before. For each daemon tool call,
the registrar validates the input schema, selects the administrator-configured
cluster, and exchanges the caller's access token once. Only the exchanged token
is sent to the configured cluster VIP. No daemon token is returned to the client.

## Lab deployment

The implementation does not modify authentik, daemons or remote files. Install
the following configuration when deploying the compiled MCP.

1. Adapt [clusters-v3.yaml](../deploy/examples/clusters-v3.yaml) into
   `/etc/opensvc-mcp/clusters.yaml`. This replaces the version 2 node catalogue
   with `cluster_id`, one VIP `endpoint`, and `auth.profile` / `auth.audience`.
2. Adapt [auth.yaml](../deploy/examples/auth.yaml) into
   `/etc/opensvc-mcp/auth.yaml`.
3. Store only the confidential client's secret in
   `/etc/opensvc-mcp/secrets/om3-mcp-exchange`, owned by the MCP runtime account,
   mode `0600` or `0400`. A final newline is accepted. The process reads the
   secret at startup; restart after rotating it. Do not put it in Git, tool
   arguments, a chat, or shell command-line arguments.
4. Add `OPENSVC_MCP_AUTH_CONFIG_FILE=/etc/opensvc-mcp/auth.yaml` to the MCP
   process environment. Keep the incoming resource and issuer unchanged:

   ```text
   OPENSVC_MCP_OAUTH_RESOURCE_URL=https://dev5-vip.opensvc.com:8443/mcp
   OPENSVC_MCP_OAUTH_ISSUER=https://labauthentik.opensvc.com/application/o/om3-mcp/
   OPENSVC_MCP_CLUSTER_CONFIG_FILE=/etc/opensvc-mcp/clusters.yaml
   OPENSVC_MCP_AUTH_CONFIG_FILE=/etc/opensvc-mcp/auth.yaml
   ```

5. The operator must configure the dev5 daemon to trust the target provider:

   ```ini
   [listener]
   openid_issuer = https://labauthentik.opensvc.com/application/o/osvc-cluster-dev5/
   openid_client_id = om3-dev5
   ```

   This replaces the previous trusted provider; tokens from that old provider
   will no longer authenticate. Follow the daemon's configuration reload
   procedure. The MCP does not change `cluster.conf`.

## Authentik configuration

There are three separate provider roles:

| Provider / client ID | Purpose |
|---|---|
| `om3-mcp` (public) | Codex Authorization Code + PKCE login; token audience includes the MCP URL |
| `om3-mcp-exchange` (confidential) | Server-side RFC 8693 requests using its client secret |
| `osvc-cluster-dev5` / `om3-dev5` | Issues daemon tokens with its own issuer, signing key and audience |

The operator reported creating the exchange application and provider, enabling
the Token exchange grant, selecting a signing key and configuring these trusts:

- `om3-mcp-exchange` → Federated OAuth2/OpenID Providers includes `om3-mcp`.
- `osvc-cluster-dev5` → Federated OAuth2/OpenID Providers includes
  `om3-mcp-exchange`.

On the target provider, attach the `opensvc-om3-root` and `opensvc-om3-guest`
scope mappings with Scope name `om3-mcp` for the validated lab configuration. They obtain entitlements from
`user.app_entitlements(provider.application)`, filtering the OpenSVC grant names.
Assign the application entitlements to the intended user or group. Keep claims
enabled. Request `om3-mcp` in the exchange profile. Additional `openid` or
`profile` scopes can be requested when their mappings are attached; `profile`
can supply `preferred_username`. These technical scopes are not MCP business
permissions. The target application policies also apply.

The operator validated real daemon calls on dev5 and dev3, including a single
Codex prompt retrieving dev5n1/dev5 and dev3n1/dev3 with the same MCP credential.
Each target uses its own provider (`osvc-cluster-dev5` or `osvc-cluster-dev3`),
audience (`om3-dev5` or `om3-dev3`), and catalogue VIP. Each target provider trusts
`om3-mcp-exchange`. The MCP audience mapping remains attached only to `om3-mcp`;
its scope name also matches the `om3-mcp` scope requested by Codex.

The MCP uses the confidential provider's discovery document, verifies its issuer,
then posts a form to the discovered HTTPS token endpoint:

```text
grant_type=urn:ietf:params:oauth:grant-type:token-exchange
subject_token=<authenticated incoming MCP access token>
subject_token_type=urn:ietf:params:oauth:token-type:access_token
requested_token_type=urn:ietf:params:oauth:token-type:access_token
audience=om3-dev5
scope=om3-mcp
```

The configured confidential client authenticates using `client_secret_basic`
(default) or `client_secret_post`. No browser redirects, `resource`, actor token,
client-credentials grant, or service-account fallback is used for the exchange.

## Tool contracts

`list_clusters(query?, limit?, cursor?)` returns all configured clusters, without
checking grants or contacting the SSO or daemons. Search matches a display-name
substring without case sensitivity. Pages default to 100 entries, maximum 200.
The opaque cursor is bound to the query and public catalogue snapshot.

```json
{
  "items": [{"cluster_id": "3bc5a684-0f37-4504-9f50-4107ff8d1f24", "name": "dev5"}],
  "next_cursor": null,
  "provenance": {"source": "opensvc_mcp_catalog", "observed_at": "<UTC timestamp>"}
}
```

Every daemon tool has an additional required `cluster_id` argument, for example:

```json
{"cluster_id":"3bc5a684-0f37-4504-9f50-4107ff8d1f24","node":"dev5n1"}
```

This target field is added centrally to each typed tool's schema and handled by
the registrar; business inputs and core use cases remain unchanged. An omitted
or unknown cluster fails before exchange. Target headers cannot override it.
The tool's `node` retains its existing logical API semantics and never changes
the configured network endpoint. Existing optional-node defaults still mean the
daemon receiving the request, which may move with the VIP. Precise node-proxy
behavior remains a separate daemon/tool contract.

## Token lifetime and failures

Only public discovery metadata is cached (five minutes). There is no exchanged
token cache or refresh-token storage. One tool call can make several API requests
with its own exchanged token. Its context expires at the earliest of the parent
request deadline, incoming JWT expiry, outgoing JWT expiry and `expires_in`.

The SSO response is bounded, and must return a Bearer access JWT for the selected
audience with a valid lifetime. Returning the incoming token or a token still
carrying the MCP resource audience is rejected. These checks are sanity checks
on a response from the authenticated SSO: the daemon remains responsible for
cryptographic validation of the outgoing signature and issuer and enforcement
of grants. The MCP does not rewrite claims or compare subject strings across
providers, whose subject modes may differ.

Discovery and exchange follow no redirects or environment proxies. Credentials
are sent only to the configured SSO's discovered endpoint, and the daemon token
only to the selected VIP. SSO TLS requires system roots or a configured CA;
daemon `tls.insecure` does not apply to the SSO. TLS and timeouts are per profile
and per cluster. No network contact occurs at startup.

SSO errors expose only recognized OAuth error codes, never response descriptions
or bodies. A refused exchange stops the call; a daemon refusal is returned using
the existing API error contract. Temporary OAuth debug logging remains removed.
Health and tool discovery continue to work while the SSO token endpoint or a
daemon is unavailable, provided the incoming MCP JWT can still be verified.

## Portability and validation status

The exchange implementation has no authentik-specific URL or provider-name
logic. For Keycloak, use standard token exchange in one realm, a confidential
requester enabled for exchange, its authentication method, and the required
audience/scope mappings. In particular, Keycloak requires the requester client
to appear in the incoming subject token's audience in this flow; this may
require adding `om3-mcp-exchange` alongside the MCP URL to the incoming audience.
`audience` filters audiences made available by Keycloak's configured mappings;
it does not create a missing audience by itself. The example lab profile is not
a claim of validated Keycloak compatibility.

Local tests cover actual HTTPS discovery, confidential exchange, safe failures,
MCP SDK calls, concurrent users and clusters, daemon 403 propagation, TLS and
catalogue bounds. A catalogue with 800 entries loads successfully. This is not a
load test of 800 live clusters. On 2026-10-07, the operator confirmed real
authentik exchange and daemon acceptance on dev5 and dev3. Negative grant tests,
cross-cluster permission isolation, renewals and Keycloak remain to be validated.

Two lab failures clarified the deployment requirements:

- A daemon 403 was resolved by aligning the exchange scopes with the target
  entitlement mappings. A provider preview alone does not validate the claims
  of a token issued through exchange.
- On dev3, OpenID discovery initially returned 404 during daemon configuration
  reload, causing its OpenID strategy to be ignored. `/api/auth/info` advertised
  the configured issuer despite that failure. Check successful strategy
  initialization on the node carrying the VIP after the provider is available.

On 2026-10-08, the operator confirmed the incoming `om3-mcp` provider's access
token validity was five minutes. The provider preview showed 24 hours and must
not be used to infer the actual access-token lifetime. Automatic renewal has
not been validated; authentik requires `offline_access` for refresh-token issuance
in the login flow. This scope is independent of the daemon exchange configuration.

References: [authentik token exchange](https://docs.goauthentik.io/add-secure-apps/providers/oauth2/token_exchange/),
[Keycloak standard token exchange](https://www.keycloak.org/securing-apps/token-exchange),
[RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html).
