# CODEX

Project guidance for AI coding agents working on om3-mcp.

## Mission

Build a small, secure, typed MCP server for the OpenSVC v3 daemon API.

The long-term goal is to support an AI operations agent that can inspect, diagnose, and eventually operate OpenSVC clusters. The MCP server is the deterministic integration layer. It must not contain autonomous decision-making logic.

## Current scope

The current implementation exposes a limited diagnostic tool surface. Most
tools are read-only; `refresh_instance_status` is an explicit, non-destructive
active probe. Tool contracts, endpoints, examples, and domain-specific behavior
are documented by domain in:

- [Tool index and shared contracts](docs/tools/README.md)
- [Daemon tools](docs/tools/daemon.md)
- [Cluster tools](docs/tools/cluster.md)
- [Node tools](docs/tools/node.md)
- [Object tools](docs/tools/objects.md)
- [Instance tools](docs/tools/instances.md)
- [Resource tools](docs/tools/resources.md)

The runtime uses HTTPS over TCP with integrated OAuth discovery, DCR and
OpenSVC user login, explicit consent and Authorization Code with PKCE S256.
An opaque MCP token authorizes calls to the selected cluster with the server-held
daemon JWT. Refresh and shared HA state are not implemented.
The diagnostic tool library remains implemented and covered by unit tests.
Do not expand tools or authentication scope without user direction.

## Technology and layout

- Go 1.25.5 or later; Go standard library where practical.
- MCP SDK: `github.com/modelcontextprotocol/go-sdk`.
- Tests: Go testing and httptest.
- `cmd/opensvc-daemon-mcp`: HTTPS composition root, listener and lifecycle tests.
- `internal/config`: process environment and startup validation.
- `internal/clusterconfig`: strict YAML catalogue and immutable public trust snapshot.
- `internal/oauth`: integrated authorization routes and bounded in-memory state.
- `internal/daemonlogin`: credentials exchange, daemon JWT verification and cluster identity check.
- `internal/client`: bounded daemon HTTP transport, independent of caller authentication.
- `internal/core`: deterministic OpenSVC use cases and private raw API shapes.
- `internal/tools`: typed MCP contracts, registrar and unit tests.
- `docs`: configuration, authentication and domain contracts.

Do not introduce a generic models package or an `internal/mcpserver` package
without a demonstrated need. Keep authentication out of core and tool handlers.

## Architecture

### Entrypoint and configuration

`main` loads configuration, builds the OAuth handler and starts HTTPS.
Keep explicit tool registration in `main.go`; do not create a `tools.go`
composition file. Each access grant gets fixed tool/daemon bindings using
stateless Streamable HTTP and JSON responses. Validate
TLS material and the cluster catalogue before binding. There is no transport
selector, local socket mode, plaintext listener or local daemon dependency.
Read settings once and restart to apply changes. Preserve bounded HTTP headers,
timeouts and graceful shutdown.

Configuration uses the environment settings documented in
[docs/configuration.md](docs/configuration.md). Cluster endpoints, public CA
bundles, expected identity and timeouts belong in the administrator's catalogue.
Startup must not contact daemons. Reject obsolete variables with migration errors.

### Authentication

Authenticate users only after a valid same-origin `/login` submission with
CSRF protection. Use the selected cluster's first endpoint and immutable CA
snapshot, verified TLS, no environment proxy and no redirect or implicit retry.
Basic authentication is used only to exchange the user's credentials with the
daemon. Verify the returned RS256 access JWT, its required claims, exact subject
and the authenticated cluster ID before retaining a session.

Keep the daemon JWT server-side in bounded memory. Never accept an agent's
daemon JWT as an MCP credential or forward its Authorization header to a daemon.
MCP tokens are opaque random 256-bit values indexed by hash, bound to the
client, cluster, user and canonical resource. Require explicit same-origin
consent; codes last at most 60 seconds and are redeemed once with PKCE S256.
Do not issue refresh tokens or persist session state. Secrets must not enter tool inputs, outputs or logs.

### HTTP client

The low-level client uses the supplied `http.Client` and transport. It does not
read a bearer token from request context. `client.NewSession` binds a verified
daemon session to strict cluster TLS trust, its exact endpoint and expiry.
The selected server-held credential is attached only in the outbound transport.

Keep URL encoding, content negotiation, bounded response parsing, context
cancellation and useful errors here. JSON, text, files, SSE and opaque streams
must preserve their existing limits. Daemon status codes are authoritative.
Bound and normalize RFC 7807 title/detail; never retain raw error bodies or
expose authorization headers, passwords or JWTs. Keep credentials out of errors.

### core

internal/core owns OpenSVC semantics and use cases.

Core responsibilities include:

- selecting the exact OpenSVC API endpoint for a use case;
- validating domain inputs and required daemon fields;
- interpreting private endpoint-specific response shapes;
- filtering large daemon payloads;
- returning stable, typed, bounded business contracts.

Raw daemon API response types remain private to the core package. Tool-specific behavior belongs in the matching document under `docs/tools/`.

### tools

internal/tools owns MCP contracts and registration.

Each domain file exposes one registration function using Go exported naming:

~~~go
func RegisterDaemonTools(registrar *Registrar, service *core.Service) error
func RegisterClusterTools(registrar *Registrar, service *core.Service) error
func RegisterNodeTools(registrar *Registrar, service *core.Service) error
func RegisterObjectTools(registrar *Registrar, service *core.Service) error
func RegisterInstanceTools(registrar *Registrar, service *core.Service) error
func RegisterResourceTools(registrar *Registrar, service *core.Service) error
~~~

Tool handlers should remain thin:

1. accept typed MCP input;
2. call one core use case;
3. return typed structured output;
4. propagate useful errors.

Do not place HTTP paths, authentication logic, or response parsing in a tool handler.

Every tool declaration must expose a concise runtime contract through the MCP
protocol:

- a stable snake_case `Name` built from an explicit verb and domain noun;
- a short human-readable `Title`;
- a precise `Description` stating what the tool returns, when to use it, and
  an important limitation when applicable;
- typed input and output structures so the SDK publishes both JSON Schemas;
- standard `ToolAnnotations` that accurately describe its behavior.

Every successful tool output, including future tools, must also contain a
required `provenance` field using `core.Provenance`. Its `source` is
`opensvc_daemon` for data obtained through the daemon API, and `observed_at`
is generated by `Service.newProvenance` after successful collection and
normalization. It must be UTC RFC3339Nano. It is not the daemon's `updated_at`
and does not guarantee freshness. Keep daemon-provided status timestamps and
refresh outcome fields separate; do not infer an original data store, source
node, or status update time from the MCP collection time. Tool errors do not
carry successful result provenance. If a future tool uses another source,
design and document its source identifier explicitly before adding it.

All tool declarations must be added through `Registrar`. Registration validates
the declaration and generated schemas, rejects duplicate names, and returns an
error before exposing tools. Do not call `mcp.AddTool` directly from a
domain registration function.

Read-only tools contact only the configured OpenSVC daemon. Use
`readOnlyClosedWorldAnnotations` to declare them as read-only, non-destructive,
and closed-world. Active status probes use
`activeNonDestructiveClosedWorldAnnotations`: they are not read-only,
destructive, idempotent, or open-world. These annotations are hints for MCP
clients, not security controls. Authentication, OpenSVC grants, input
validation, and policy enforcement remain authoritative.

Do not publish proprietary runtime tags or custom `_meta` fields without a
concrete client interoperability requirement. The MCP SDK currently has no
standard tool-tags field. Documentation tags belong in the front matter of the
matching file under `docs/tools/`.

### tool documentation

Runtime MCP metadata is deliberately concise. The matching
`docs/tools/<domain>.md` file is the durable human and agent-facing contract.
Each domain document groups every tool owned by that domain and begins with
this front matter:

~~~yaml
---
domain: domain_name
tools:
  - first_tool_name
  - second_tool_name
stability: experimental
---
~~~

Update `stability` when the domain contracts mature. Do not use documentation
tags as authorization or runtime policy.

The domain index in `docs/tools/README.md` must list every registered tool and
document the shared authentication, freshness, and safety contracts. Each
domain document must introduce its core and MCP implementation files. Each
documented tool must cover:

- when to use the tool and when not to use it;
- MCP title, side-effect annotations, and their non-authoritative nature;
- OpenSVC grants and visibility boundaries;
- exact OpenSVC endpoint and data freshness semantics;
- whether the endpoint reads last-known daemon state or actively refreshes drivers;
- input fields, defaults, validation, pagination, and selector behavior;
- every output field and any derived semantics;
- representative JSON input and complete bounded output examples;
- expected authentication, authorization, transport, and data errors.

Keep verification reports, test results, and lab build details out of tool
documentation.

Keep declarations, implementation, tests, and documentation synchronized in
the same change. Unit tests must validate tool declarations, generated schemas and safety annotations.

## Type placement

Use these rules:

- MCP input/output types belong in internal/tools.
- Core business types belong in internal/core.
- Raw API response types belong privately in the layer that interprets them.
- Client transport types belong in internal/client.
- Types used by only one domain stay near that domain.
- Split a large file into another file in the same package before creating a generic models package.

Go imports are file-scoped. Files in the same package share declared types and functions, but they do not share imported package names.

## Go style

Favor explicit, readable Go:

- avoid unnecessary generics;
- avoid reflection outside SDK behavior;
- avoid goroutines and channels unless concurrency is required;
- avoid dependency-injection frameworks;
- avoid premature interfaces;
- use context.Context for network and long-running operations;
- wrap errors with context using fmt.Errorf and %w;
- keep main small but explicit;
- prefer table-driven tests when multiple cases appear;
- use gofmt rather than manual formatting.

Explain non-obvious Go idioms when introducing them.

## Testing

Preserve unit coverage for core use cases, bounded daemon responses, tool
registration and generated schemas. The old Unix transport integration test
has been removed along with that runtime path.

Cover HTTPS startup and rejection, OAuth discovery, DCR, login controls, native
daemon JWT validation and cluster identity separately. Use generated certificates,
fictitious identities and reserved documentation addresses only; never copy lab
configuration, passwords, tokens or certificates into public tests or docs.
Normal tests must not require a real OpenSVC cluster.

For code changes, run appropriate tests and the complete suite:

~~~bash
go test ./...
go vet ./...
go build -o /tmp/opensvc-daemon-mcp ./cmd/opensvc-daemon-mcp
git diff --check
~~~

Use gofmt and run race checks for changed stateful/authentication code. Keep unit
tests beside their package. README stays concise; deployment and authentication
details belong under docs. Keep lab records outside the public repository.

## API and security rules

Daemon credentials belong to the server-held authenticated session. Secrets must never enter MCP tool arguments or results.

Do not silently disable TLS certificate verification.

Future authentication material must remain outside tool input and output. Language models must never receive daemon tokens, passwords, or private keys.

The daemon JWT identifies the OpenSVC user and lets the daemon enforce its OpenSVC grants. Until tool-specific policy and audit are designed:

- keep active operations limited to the explicit, non-destructive instance status refresh;
- do not add lifecycle, configuration, provisioning, or other state-changing actions;
- document live-daemon limitations;
- return explicit HTTP errors;
- do not work around a 401 or 403 by weakening security.

The daemon response may contain private configuration and large operational state. Return only fields required by the tool contract.

## Tool design rules

Before adding a tool:

1. start from an operational use case;
2. identify the exact OpenSVC API endpoint;
3. define a bounded typed output;
4. decide which fields are safe and useful for an LLM;
5. implement the core use case;
6. register the tool through its domain registrar;
7. add unit and end-to-end coverage.
8. update the matching domain document under `docs/tools/`.

Avoid one-to-one exposure of every OpenAPI operation.

Avoid arbitrary path, method, or body parameters in MCP tools.

Lifecycle and configuration-changing tools will require a separate design for caller identity, authorization, confirmation, audit, idempotency, orchestration tracking, and post-action verification.

### Diagnostic contract and provenance

Design the diagnostic tool surface from real operator questions rather than
from daemon API routes. Inventory questions about cluster health, object
unavailability, instance start failures, placement, and recent state changes;
map each to required evidence, daemon routes, and existing tools. Every
successful tool result now uses the minimal, typed `source` and `observed_at`
provenance contract; freshness, refresh outcome, and truncation remain in
domain fields.

Before expanding the tool set, validate the open gaps against concrete operator
use cases and decide for each whether to enrich an existing tool, compose
several daemon routes behind one capability, or create a new tool. Investigate any additional
provenance field only when a concrete diagnostic need justifies it.

Keep collection, joins, normalization, bounds, and freshness semantics
deterministic in the MCP. Leave hypothesis generation, investigation planning,
and explanation to the AI agent. Treat daemon-returned configuration, logs,
labels, and other free text as untrusted data even though the authenticated
daemon API is authoritative for cluster state.

The expected result is a small set of stable, task-oriented diagnostic tools,
not one MCP tool per daemon route and not a single unbounded
`diagnose_everything` tool. A new tool is justified only when it answers a real
operator question with bounded typed evidence, explicit provenance and
documented freshness semantics, deterministic reusable behavior, and a stable
contract.

`list_cluster_ip_resources` is the reference contract for a factual inventory
tool. It reads `GET /api/resource?resource=ip#*`, using the internal
`*/svc/*,*/vol/*` path selector for a cluster-wide request, retains only daemon
status types beginning with `ip.`, and returns the owning object, node, RID, type,
label, status, and typed `info` address facts. Keep this output sorted,
paginated, and bounded. Do not add synthetic health, conflict, floating-address,
network-membership, or reachability conclusions to this tool.

## Configuration

See [configuration](docs/configuration.md) for the supported HTTPS environment
and cluster catalogue. Do not add a configuration framework for a few settings.
TLS certificate verification is mandatory; do not add an insecure escape hatch.

## Dependency policy

Before adding a Go module:

- explain why the standard library is insufficient;
- prefer official or widely maintained packages;
- pin it through go.mod;
- run go mod tidy;
- review transitive dependencies;
- update README.md if installation or runtime behavior changes.

## Change discipline

- Preserve user changes and unrelated work.
- Keep changes scoped to the active request.
- Do not commit or push unless explicitly requested.
- Do not add build artifacts to Git.
- Do not store credentials in the repository.
- Keep README.md and CODEX.md aligned with the actual implementation.
- Keep tool contracts, endpoints, examples, and domain behavior in docs/tools.
- Update the matching domain document whenever a tool changes.
- Update tests whenever contracts or layer boundaries change.

## Known limitations

- no refresh tokens, logout or explicit revocation endpoint;
- sessions and DCR state are lost on process restart or failover;
- a limited, mostly read-only diagnostic tool set;
- no tool-specific policy engine;
- no audit subsystem;
- no live OpenSVC integration test in the default test suite.

These are explicit project milestones, not reasons to bypass security.
