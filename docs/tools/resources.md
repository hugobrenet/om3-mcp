---
domain: resource
tools:
  - get_container_logs
  - list_cluster_ip_resources
  - list_object_resources
  - list_resource_info
stability: experimental
---

# Resource Tools

This document describes tools that inventory cluster IP resources, inspect
resource status and cached driver information, and read bounded container
output for an OpenSVC object.

Implementation:

- business logic: `internal/core/resource.go`, `internal/core/resource_info.go`,
  and `internal/core/container_logs.go`;
- MCP definitions: `internal/tools/resource.go`.

## Tools

### `get_container_logs`

Returns bounded recent stdout and stderr output for one exact OpenSVC container
resource. Use it after `list_object_resources` identifies a `container.*`
resource whose workload output is needed for diagnosis.

Do not use this tool for OpenSVC daemon, monitor, or orchestration records; use
`get_instance_logs` for those. The tool performs a finite historical read and
never follows the stream.

#### OpenSVC API and stream behavior

```text
GET /api/node/name/<node>/instance/path/<namespace>/<kind>/<name>/container/log
  ?rid=<container#name>&follow=false&lines=<lines>
Accept: text/event-stream
```

The OpenSVC endpoint declares `text/event-stream`, but its response body is raw
container log bytes rather than SSE `data:` records. The MCP therefore consumes
it as a bounded opaque stream, normalizes it to safe UTF-8 text, and returns one
ordinary structured result.

The endpoint executes the container runtime's finite log command. It does not
run status drivers or change object state. The MCP tool is annotated read-only,
non-destructive, and closed-world. These annotations are client hints; OpenSVC
authorization remains authoritative.

This endpoint requires the global `root` grant.

Container output is application-controlled and may contain credentials,
personal data, or other sensitive values. Request only the smallest useful
line count and do not copy results into unrelated contexts.

#### Input

| Field | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `path` | Yes | — | Exact object path | Canonical OpenSVC object path |
| `node` | Yes | — | 255 characters | Exact node hosting the container |
| `resource_id` | Yes | — | Exact `container#…`, 255 characters | Container RID returned by `list_object_resources` |
| `lines` | No | 50 | 1..200 | Maximum recent records requested from the runtime |

Selectors such as `container#*` are rejected. `lines=0` is the omitted Go zero
value and selects 50; it never enables an unbounded read. The daemon may omit
older records according to `lines`, independently of the MCP output bound.

Example input:

```json
{
  "path": "prod/svc/redis",
  "node": "node-a",
  "resource_id": "container#redis",
  "lines": 10
}
```

#### Example output

```json
{
  "provenance": {
    "source": "opensvc_daemon",
    "observed_at": "2026-07-15T05:40:00Z"
  },
  "object": {
    "path": "prod/svc/redis",
    "namespace": "prod",
    "kind": "svc",
    "name": "redis"
  },
  "node": "node-a",
  "resource_id": "container#redis",
  "lines": 10,
  "line_count": 3,
  "content": "1:M * Running mode=standalone, port=6379.\n1:M * Server initialized\n1:M * Ready to accept connections tcp",
  "truncated": false
}
```

#### Output and bounds

| Field | Meaning |
|---|---|
| `object` | Canonical object reference |
| `node` | Exact queried node |
| `resource_id` | Exact queried container RID |
| `lines` | Effective maximum requested from OpenSVC |
| `line_count` | Normalized text lines present in `content` |
| `content` | Combined recent container stdout and stderr text |
| `truncated` | Whether the MCP shortened content to its output bound |

Content is valid UTF-8 and limited to 65,536 Unicode code points. NUL, terminal
control, and formatting characters are replaced; line feeds and tabs are
preserved. `truncated=false` does not mean the complete lifetime log was
returned: `lines` still limits the daemon request.

Invalid paths, nodes, RIDs, or line counts fail before daemon access.
Authorization failures, unexpected content types, oversized transport
responses, interrupted streams, and caller cancellation become MCP tool
errors. Daemon RFC 7807 errors remain bounded and never expose the delegated
JWT.

In the current OpenSVC implementation, the daemon sends HTTP `200` and flushes
headers before starting the local container-log command. A runtime failure
after that point can therefore appear as empty or partial successful content;
the MCP cannot reconstruct an HTTP error that the daemon did not send.

### `list_cluster_ip_resources`

Returns the visible OpenSVC IP resources from the daemon's last-known cluster
resource status. Use it to answer which addresses are declared as resources,
which object and instance own them, and what address facts the IP driver
reported.

The tool does not decide whether an address is floating, conflicting, reachable,
or correctly attached. It does not read `/api/network/ip` or host interfaces.
Those conclusions require other evidence and remain the agent's responsibility.

#### OpenSVC API and filtering

```text
GET /api/resource?resource=ip#*
  &path=*/svc/*,*/vol/*
  [&node=<exact-node>]

# When the caller supplies an exact object:
GET /api/resource?resource=ip#*&path=<exact-path>[&node=<exact-node>]
```

The MCP asks the daemon to select `ip#*` resource identifiers and also retains
only records whose daemon-reported type begins with `ip.`. Without an exact
object filter, the internal path selector restricts the daemon scan to `svc`
and `vol`, the OpenSVC object kinds that can own resources. It still covers
root and namespaced objects across the cluster while excluding configuration
objects that cannot own IP resources. OpenSVC applies delegated JWT namespace
grants before returning the records.

The endpoint reads last-known instance resource status. It does not execute an
IP driver, inspect live host interfaces, or change state. The tool is annotated
read-only, non-destructive, and closed-world; these annotations are client
hints, while daemon authorization remains authoritative.

#### Input

| Field | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `path` | No | Empty | Exact path, 512 characters | Restrict results to one canonical object |
| `node` | No | Empty | Exact name, 255 characters | Restrict results to one node |
| `limit` | No | 100 | 1..200 | Maximum IP resources in this page |
| `cursor` | No | Empty | 1024 characters | Previous `next_cursor` with unchanged filters |

An empty `path` and `node` lists visible IP resources cluster-wide. Selectors
and glob expressions are rejected for these optional filters.

Example input:

```json
{
  "path": "prod/svc/redis",
  "node": "node-a",
  "limit": 100
}
```

#### Example output

```json
{
  "provenance": {
    "source": "opensvc_daemon",
    "observed_at": "2026-09-22T10:45:00Z"
  },
  "path_filter": "prod/svc/redis",
  "node_filter": "node-a",
  "total": 1,
  "count": 1,
  "resources": [
    {
      "object": {
        "path": "prod/svc/redis",
        "namespace": "prod",
        "kind": "svc",
        "name": "redis"
      },
      "node": "node-a",
      "rid": "ip#0",
      "type": "ip.host",
      "label": "host 192.0.2.10/24 ens3",
      "status": "up",
      "info": {
        "ipaddr": "192.0.2.10",
        "dev": "ens3",
        "netmask": 24,
        "expose": []
      }
    }
  ],
  "truncated": false
}
```

Each resource contains only facts copied or structurally normalized from
`/api/resource`:

| Field | Meaning |
|---|---|
| `object` | Canonical object owning the resource |
| `node` | Instance node reported by OpenSVC |
| `encap_node` | Encapsulated node when the daemon provides one |
| `rid` | IP resource identifier |
| `type` | IP driver type, such as `ip.host` or `ip.netns` |
| `label` | Driver label reported by OpenSVC |
| `status` | Last-known resource availability status |
| `info.ipaddr` | Address reported by the IP driver |
| `info.dev` | Device reported by the IP driver |
| `info.netmask` | Prefix length reported by the IP driver |
| `info.expose` | Exposure declarations reported by the IP driver |
| `info.hostname` | Hostname when the driver reports one |

Missing optional driver facts stay absent. An empty `expose` value is
normalized to `[]`. The MCP does not add health, conflict, floating-address,
network-membership, or reachability conclusions.

Results are sorted by node, object path, encapsulated node, and RID before
pagination. Pagination is not snapshot-based; callers must preserve `path`,
`node`, and `limit` between pages.

Invalid exact filters, limits, or cursors fail before daemon access. Malformed
object paths returned by the daemon, authorization failures, transport
failures, and malformed responses are MCP tool errors. Errors preserve bounded
RFC 7807 details and never include the delegated JWT.

### `list_object_resources`

Returns sorted, paginated resource status records for one exact object.

Use it after `list_object_instances` to identify the resource responsible for
an unhealthy instance. The tool returns status, provisioning, monitoring flags,
restart state, tags, and bounded status messages. It does not expose resource
configuration values, container output, or execute resource actions. Continue
with `get_container_logs` only for one exact container RID when workload output
is necessary.

#### OpenSVC API and freshness

```text
GET /api/resource?path=<exact-path>[&node=<node>][&resource=<rid>]
```

Resource data comes from the last-known instance status and does not trigger a
driver probe. Use the selected instance's `updated_at` in
`list_object_instances`, or `status.updated_at` in `get_instance_status`, to
assess freshness. Refresh that exact instance first when necessary.

OpenSVC filters resources according to delegated JWT namespace grants. This
tool is read-only, non-destructive, closed-world, and has no side effects.

#### Input

| Field | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `path` | Yes | — | 512 characters | Exact canonical object path |
| `node` | No | Empty | 255 characters | Exact node filter |
| `rid` | No | Empty | 255 characters | Resource id or OpenSVC resource match expression |
| `limit` | No | 100 | 1..200 | Maximum resources in this page |
| `cursor` | No | Empty | 1024 characters | Previous `next_cursor` with unchanged filters |

Example input:

```json
{
  "path": "prod/svc/redis",
  "node": "node-a",
  "limit": 100
}
```

#### Example output

```json
{
  "provenance": {
    "source": "opensvc_daemon",
    "observed_at": "2026-07-15T05:31:03Z"
  },
  "count": 1,
  "node_filter": "node-a",
  "object": {
    "kind": "svc",
    "name": "redis",
    "namespace": "prod",
    "path": "prod/svc/redis"
  },
  "resources": [
    {
      "config_flags": {
        "is_disabled": false,
        "is_monitored": true,
        "is_standby": false
      },
      "label": "docker redis:7-alpine",
      "logs": [],
      "logs_truncated": false,
      "node": "node-a",
      "provisioned": "n/a",
      "provisioned_at": "0001-01-01T00:00:00Z",
      "restart_remaining": 0,
      "rid": "container#redis",
      "status": "up",
      "status_flags": {
        "disable": false,
        "monitor": true,
        "optional": false,
        "standby": false,
        "encap": false
      },
      "tags": ["mcp-test"],
      "type": "container.docker"
    }
  ],
  "total": 1,
  "truncated": false
}
```

Resources are sorted by node, encapsulated node, and resource id. At most 20
status messages are returned per resource; `logs_truncated=true` signals that
additional messages were omitted. Pagination is not snapshot-based, so callers
must preserve all filters between pages.

`config_flags` preserves `is_disabled`, `is_monitored`, and `is_standby` from
the daemon resource `config` section. `status_flags` separately preserves
`disable`, `monitor`, `optional`, `standby`, and `encap` from the daemon
resource `status` section. Either object is `null` when its source section is
absent. The MCP does not combine these values with a logical OR and does not
let a status value overwrite configuration intent. A difference between the
two sources remains visible for agent analysis.

#### Errors

Invalid paths, filters, limits, or cursors fail before daemon access. Missing
visibility, daemon authorization, transport failures, and malformed responses
are MCP tool errors. Errors preserve bounded RFC 7807 details and never include
the delegated JWT.

### `list_resource_info`

Returns bounded cached resource information key-value entries for one exact
`svc` or `vol`. Use it after `list_object_resources` identifies a resource
whose driver facts are needed. For example, an `app` resource can report its
effective commands and timeouts, while storage drivers can report devices,
sizes, arrays, or backend identifiers.

This data is distinct from resource status and from raw object configuration.
The daemon reports every value as a string; the MCP deliberately does not
coerce values such as `"false"` or `"1073741824"` to booleans or numbers.

#### OpenSVC API and freshness

Object scope aggregates every visible instance:

```text
GET /api/object/path/<namespace>/<kind>/<name>/resource/info
```

Instance scope reads one exact node:

```text
GET /api/node/name/<node>/instance/path/<namespace>/<kind>/<name>/resource/info
```

Both endpoints read the daemon's persisted `resinfo.json` cache. They do not
run resource drivers or refresh the cache. OpenSVC does not include the cache
timestamp in this response, so `provenance.observed_at` dates only the MCP
collection. It must not be interpreted as resource information freshness.

The daemon requires `guest` or higher on the object namespace. The tool is
read-only, non-destructive, closed-world, and has no side effects.

Resource information can contain commands, paths, storage identifiers, or
arbitrary output produced by an application resource's `info` command. Treat
values as potentially sensitive and untrusted even though OpenSVC authorized
the request.

#### Input

| Field | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `scope` | Yes | — | `object` or `instance` | Whether to aggregate visible instances or read one exact node |
| `path` | Yes | — | Exact `svc` or `vol` path | Canonical OpenSVC object path |
| `node` | Instance only | Empty | Exact name, 255 characters | Node to read; must be omitted for object scope |
| `rid` | No | Empty | Exact value, 255 characters | Local exact resource-id filter |
| `key` | No | Empty | Exact value, 255 characters | Local exact information-key filter |
| `limit` | No | 100 | 1..200 | Maximum entries in this page |
| `cursor` | No | Empty | Opaque, 64 characters | Previous `next_cursor` with every other input unchanged |

`rid` and `key` are applied by the MCP after it receives and validates the
complete daemon response. They reduce the result, not the upstream transfer.

Example input:

```json
{
  "scope": "instance",
  "path": "prod/svc/app",
  "node": "node-a",
  "rid": "app#worker",
  "limit": 2
}
```

#### Example output

```json
{
  "provenance": {
    "source": "opensvc_daemon",
    "observed_at": "2026-09-28T14:40:00Z"
  },
  "scope": "instance",
  "object": {
    "kind": "svc",
    "name": "app",
    "namespace": "prod",
    "path": "prod/svc/app"
  },
  "node": "node-a",
  "filters": {
    "rid": "app#worker"
  },
  "reported_total": 28,
  "total": 19,
  "count": 2,
  "entries": [
    {
      "node": "node-a",
      "object": "prod/svc/app",
      "rid": "app#worker",
      "key": "driver",
      "value": "app.forking",
      "value_truncated": false
    },
    {
      "node": "node-a",
      "object": "prod/svc/app",
      "rid": "app#worker",
      "key": "start",
      "value": "/bin/true",
      "value_truncated": false
    }
  ],
  "values_truncated": 0,
  "next_cursor": "opaque-cursor-returned-by-the-tool",
  "truncated": true
}
```

`reported_total` is the daemon item count before local filters. `total` is the
filtered count before pagination, and `count` is the current page size.
Entries are sorted by node, object, RID, key, and raw value. Duplicate entries
remain distinct and are paginated with occurrence-aware opaque cursors.

The MCP accepts at most 10,000 daemon entries. Raw values above 1 Mi Unicode
code points are rejected. Returned values are limited to 4,096 Unicode code
points and flagged with `value_truncated=true`; each page also has a 128 Ki
Unicode-code-point aggregate budget. `values_truncated` counts bounded values
in the current page and is independent from collection `truncated`.

The daemon currently returns `200` with an empty array for an unknown object in
object scope, so an empty result means no visible cached information and does
not prove that the object exists. Instance scope returns `404` when the local
instance or its cache is absent. Authorization, non-2xx responses, unexpected
objects or nodes, invalid identifiers, control characters, oversized payloads,
and stale cursors become MCP tool errors.
