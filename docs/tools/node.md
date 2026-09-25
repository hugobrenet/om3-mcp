---
domain: node
tools:
  - get_node_config
  - get_node_status
  - get_node_logs
  - list_node_capabilities
  - list_node_drivers
  - get_node_daemon_metrics
  - probe_node_reachability
stability: experimental
---

# Node Tools

This document describes tools that read bounded node configuration evidence,
inspect the last-known state and cached capabilities of one exact node, read
its recent OpenSVC journal entries, expose advanced daemon-process metrics,
and actively verify the proxy path to one daemon.

Implementation:

- business logic: `internal/core/node.go`, `internal/core/node_capability.go`,
  `internal/core/node_driver.go`, `internal/core/node_daemon_metric.go`, and
  `internal/core/node_reachability.go`;
- MCP definitions: `internal/tools/node.go`.

## Tools

### `get_node_config`

Returns the node configuration file for one exact node as bounded UTF-8 text.
Use it when a diagnosis depends on node-local declarations such as listener,
arbitrator, asset, or other node settings. Use `get_node_status` for runtime
state; this tool does not compare the file with live status or interpret its
keywords.

#### OpenSVC API, authorization, and freshness

```text
GET /api/node/name/<node>/config/file?redact-secrets=true
Accept: application/octet-stream
```

The MCP always sends `redact-secrets=true` and exposes no option to disable it.
The daemon performs redaction and requires the global `root` grant. For the
local node, the handler reads the current `node.conf` file. For another cluster
node, the contacted daemon proxies the same request to that node. The call does
not read the cluster status cache, refresh drivers, or change configuration.

#### Input and output

`node` is optional and defaults to `_`, the daemon API alias for the local
node. A supplied value must be one exact OpenSVC node name of at most 255
characters using letters, digits, dot, underscore, or hyphen. Leading and
trailing whitespace, paths, wildcards, and selectors are rejected before any
daemon request.

The output has the same `provenance`, `content`, `size_bytes`,
`returned_bytes`, `truncated`, and `redaction_requested` fields as
`get_cluster_config`, plus `node` containing the requested node or `_` when the
local default was used. The 65,536-byte MCP output limit, 1 MiB transport ceiling, UTF-8 requirement, and
`application/octet-stream` check are identical.

#### MCP properties

| Property | Value |
|---|---|
| Title | Get node configuration |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon and its OpenSVC proxy path are used |
| Side effects | None |

Annotations are client hints; OpenSVC enforces access using the delegated JWT.

#### Example

Input:

```json
{}
```

Output:

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-23T10:00:01Z"},
  "node": "_",
  "content": "[node]\nsshkey = ********\n",
  "size_bytes": 27,
  "returned_bytes": 27,
  "truncated": false,
  "redaction_requested": true
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Missing global `root` grant | Tool error containing daemon HTTP `403` |
| Oversized or non-exact non-empty `node` | Tool error before the daemon request |
| Node or node configuration file missing | Tool error containing daemon HTTP `404` |
| Response above 1 MiB | Tool error before any content is returned |
| Invalid UTF-8 or unexpected media type | Tool error; no partial configuration is returned |
| Daemon or remote-node proxy unavailable | Tool error with daemon transport context |

### `get_node_status`

Returns the last-known status of one exact node after `get_cluster_status`
provides the cluster-wide facts. It exposes the reported agent
and compatibility versions, leader and overload flags, freeze and boot times,
monitor state and targets, capacity statistics, memory and swap policy
thresholds, factual membership context, and bounded heartbeat data. Use
`get_cluster_status` for a factual cluster-wide snapshot. This node-focused
tool does not classify heartbeat health or freshness.

#### OpenSVC API and freshness

```text
GET /api/cluster/status
```

The request has no query parameters. The MCP selects the exact `node` key from
`cluster.node` after the daemon returns its last-known cluster view. The call
does not contact the selected node directly or run status drivers. The endpoint
accepts a global `guest` or higher grant; the delegated JWT and the daemon's
authorization remain authoritative. The result excludes other nodes, objects,
hooks, keys, and other node configuration fields.

`provenance.observed_at` dates the MCP collection, while `monitor.updated_at`
dates the monitor state and `heartbeat.updated_at` preserves the published
heartbeat timestamp. The MCP does not compare those values or apply an age
threshold. `stats`, `policy`, or `heartbeat` is `null` when that section is
absent from the daemon response. An unreported scalar field in `status` or
`monitor` appears as its JSON zero value.

#### Input and output

`node` is required and must be one exact name of at most 255 characters.
Leading or trailing spaces and wildcard/selector characters are rejected. No
default node, selector, or pagination is used.

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `node` | Selected OpenSVC node name |
| `membership` | Whether the node is configured and a bounded list of distinct configured peer names |
| `status` | Agent/API/compatibility versions, leader and overload flags, boot and freeze times |
| `monitor` | State, global and local targets, orchestration ID/completion, and update time |
| `stats` | Fifteen-minute load, available and total memory/swap, and OpenSVC capacity score; `null` if absent |
| `policy` | Minimum available memory and swap percentages; `null` if absent; zero disables the corresponding check |
| `heartbeat` | Publication time, messages, secret version numbers, streams, alerts, peers, and their exact values; `null` if absent |

`membership.configured_peers` contains distinct configured node names other
than the selected node, sorted by exact name and limited to 200. It returns
`total`, `count`, `items`, and `truncated`. This context lets the agent compare
configured peers with reported heartbeat links without the MCP declaring a
peer missing or stale.

The heartbeat projection is shared with `get_cluster_status`:

| Collection | Limit | Ordering |
|---|---:|---|
| Last messages | 100 | Daemon-provided order |
| Streams | 50 | Exact stream identifier |
| Alerts per stream | 50 | Daemon-provided order |
| Peers per stream | 100 | Exact peer name |

Every bounded heartbeat collection returns `total`, `count`, `items`, and
`truncated`. Alert messages and peer descriptions are limited to 1024 Unicode
characters with an explicit truncation boolean. Unknown states, zero or old
timestamps, `is_beating=false`, and daemon-provided alerts remain source facts.
The MCP does not emit `healthy`, `degraded`, `unknown`, `not_applicable`, or
synthetic heartbeat issues.

#### MCP properties

| Property | Value |
|---|---|
| Title | Get node status |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon is contacted |
| Side effects | None |

Annotations are client hints; OpenSVC enforces access using the delegated JWT.

#### Example

Input:

```json
{"node":"node1"}
```

Illustrative output using the two-node lab. Timestamps and capacity values are
representative:

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-18T10:00:30Z"},
  "node": "node1",
  "membership": {
    "is_configured": true,
    "configured_peers": {
      "total": 1,
      "count": 1,
      "items": ["node2"],
      "truncated": false
    }
  },
  "status": {
    "agent_version": "v3.0.0-rc30",
    "api_version": 0,
    "compat_version": 0,
    "is_leader": true,
    "is_overloaded": false,
    "booted_at": "2026-09-18T09:00:00Z",
    "frozen_at": "0001-01-01T00:00:00Z"
  },
  "monitor": {
    "state": "idle",
    "global_expect": "none",
    "local_expect": "none",
    "orchestration_id": "",
    "orchestration_is_done": true,
    "updated_at": "2026-09-18T10:00:00Z"
  },
  "stats": {
    "load_15m": 0.4,
    "mem_available_pct": 82,
    "mem_total_mb": 4096,
    "score": 67,
    "swap_available_pct": 0,
    "swap_total_mb": 0
  },
  "policy": {"min_avail_mem_pct": 5, "min_avail_swap_pct": 0},
  "heartbeat": {
    "updated_at": "2026-09-18T10:00:00Z",
    "last_message": {"from": "node1", "patch_length": 0, "type": "patch"},
    "last_messages": {
      "total": 2,
      "count": 2,
      "items": [
        {"from": "node1", "patch_length": 0, "type": "patch"},
        {"from": "node2", "patch_length": 0, "type": "patch"}
      ],
      "truncated": false
    },
    "secret_version": {"main": 0, "alternate": 0},
    "streams": {
      "total": 2,
      "count": 2,
      "items": [
        {
          "id": "hb#1.rx",
          "type": "unicast",
          "state": "running",
          "configured_at": "2026-09-18T09:00:00Z",
          "created_at": "2026-09-18T09:00:00Z",
          "updated_at": "0001-01-01T00:00:00Z",
          "alerts": {"total": 0, "count": 0, "items": [], "truncated": false},
          "peers": {
            "total": 1,
            "count": 1,
            "items": [{
              "name": "node2",
              "description": ":10000 ← node2",
              "description_truncated": false,
              "is_beating": true,
              "changed_at": "2026-09-18T09:00:01Z",
              "last_beating_at": "2026-09-18T10:00:00Z"
            }],
            "truncated": false
          }
        },
        {
          "id": "hb#1.tx",
          "type": "unicast",
          "state": "running",
          "configured_at": "2026-09-18T09:00:00Z",
          "created_at": "2026-09-18T09:00:00Z",
          "updated_at": "0001-01-01T00:00:00Z",
          "alerts": {"total": 0, "count": 0, "items": [], "truncated": false},
          "peers": {
            "total": 1,
            "count": 1,
            "items": [{
              "name": "node2",
              "description": "→ node2:10000",
              "description_truncated": false,
              "is_beating": true,
              "changed_at": "2026-09-18T09:00:01Z",
              "last_beating_at": "2026-09-18T10:00:00Z"
            }],
            "truncated": false
          }
        }
      ],
      "truncated": false
    }
  }
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Insufficient daemon grants | Tool error containing daemon HTTP `403` |
| Empty, oversized, or non-exact `node` | Tool error before the daemon request |
| Configured node with no published data | Tool error naming the node |
| Unknown node | Tool error naming the node |
| Daemon unavailable or malformed response | Tool error with transport or decoding context |

### `list_node_capabilities`

Returns the exact capability markers stored in the OpenSVC capability cache of
one node. The list can contain base driver capabilities, driver sub-features,
and node-level environment detections. Use it to establish what OpenSVC last
detected before investigating configuration, resources, or missing runtime
dependencies.

#### OpenSVC API, authorization, and freshness

```text
GET /api/node/name/_/capabilities
```

The endpoint requires the global `root` grant. It loads the capability cache
of the local node by default. When the optional `node` input is set, `_` is
replaced with that exact node name and OpenSVC proxies the request when
necessary. This read does not execute scanners, probe dependencies, or modify
the cache.

The endpoint provides no scan timestamp. `provenance.observed_at` dates only
the MCP read, not the capability scan. A missing marker can therefore mean
that a dependency was absent during the last scan, that a scanner failed, or
that the cache is stale. Conversely, a present marker does not prove that a
feature is configured, used, reachable, or currently healthy. Some scanners
report built-in support unconditionally, while others inspect commands or OS
features.

#### Input and pagination

| Input | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `node` | No | Local node (`_`) | Exact hostname using letters, digits, `.`, `_`, or `-`; at most 255 characters | Node whose cache is read |
| `limit` | No | 100 | 1..200 | Maximum distinct capability names returned |
| `cursor` | No | — | At most 1024 characters | Exact `next_cursor` from the preceding page for the same node |

The MCP validates every `CapabilityItem`. With an explicit node,
`meta.node` must match it. With the local `_` alias, all entries must report
one consistent exact node, which is returned in the output `node` field. An
empty local result keeps `node` set to `_`, because no item can resolve the
alias. Names are preserved exactly; only exact duplicates are removed before
lexicographic sorting. `reported_total` is the raw number of entries returned
by OpenSVC; `total` is the number of distinct names. A page is additionally
bounded to 64 Ki Unicode code points, and each name to 1024.

#### MCP properties

| Property | Value |
|---|---|
| Title | List node capabilities |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon and its OpenSVC proxy path are used |
| Side effects | None; no capability scan |

#### Example

Input:

```json
{"limit":3}
```

Output using the real node1 capability cache:

```json
{
  "provenance": {"source":"opensvc_daemon","observed_at":"2026-09-24T11:30:00Z"},
  "node": "node1",
  "reported_total": 65,
  "total": 65,
  "count": 3,
  "capabilities": [
    "drivers.array.freenas",
    "drivers.array.hds",
    "drivers.array.hoc"
  ],
  "next_cursor": "drivers.array.hoc",
  "truncated": true
}
```

Examples of other marker classes observed on node1 include
`drivers.resource.container.docker`,
`drivers.resource.container.docker.registry_creds`, and `node.x.systemd`.
The MCP does not translate these markers into availability or health verdicts.

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Missing global `root` grant | Tool error containing daemon HTTP `403` |
| Oversized or non-exact non-empty `node` | Tool error before the daemon request |
| Invalid page size or cursor | Tool validation error |
| Cursor no longer present after a cache change | Explicit stale-cursor tool error |
| Unexpected list/item kind, node, or capability name | Tool error; no partial list |
| Daemon or remote-node proxy unavailable | Tool error with transport context |

### `list_node_drivers`

Returns the exact driver names registered in one running OpenSVC daemon. Use
it to establish whether that daemon knows how to parse and operate a driver
type before checking its detected capabilities, configuration, or resource
status.

#### OpenSVC API and authorization

```text
GET /api/node/name/_/drivers
```

The endpoint requires the global `root` grant. It reads the in-memory driver
registry of the local daemon by default. When the optional `node` input is
set, `_` is replaced with that exact node name and OpenSVC proxies the request
when necessary. This read does not execute drivers, scan capabilities, probe
dependencies, or change daemon state.

The registry includes drivers compiled or loaded into the running process.
Registration alone does not prove that required commands or operating-system
features exist, that a driver is configured or used, or that it is healthy.
Use `list_node_capabilities` separately when the last detected runtime support
matters. The MCP deliberately does not merge both sources into an
`available` verdict.

#### Input and pagination

| Input | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `node` | No | Local node (`_`) | Exact hostname using letters, digits, `.`, `_`, or `-`; at most 255 characters | Node whose running driver registry is read |
| `limit` | No | 100 | 1..200 | Maximum distinct driver names returned |
| `cursor` | No | — | At most 1024 characters | Exact `next_cursor` from the preceding page for the same node |

The MCP validates the live response shape observed from OpenSVC:
`DriverList` containing `DriverItem` entries. With an explicit node,
`meta.node` must match it. With the local `_` alias, all entries must report
one consistent exact node, which is returned in the output `node` field. An
empty local result keeps `node` set to `_`. Names are preserved exactly; only
exact duplicates are removed before lexicographic sorting. `reported_total`
is the raw number of entries returned by OpenSVC; `total` is the number of
distinct names. A page is additionally bounded to 64 Ki Unicode code points,
and each name to 1024.

The current OpenAPI schema spells the item enum `DriversItem`, while both the
daemon handler and the live response use `DriverItem`. The MCP follows the
actual daemon contract and rejects the schema-only spelling.

#### MCP properties

| Property | Value |
|---|---|
| Title | List node drivers |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon and its OpenSVC proxy path are used |
| Side effects | None; no driver execution or capability scan |

#### Example

Input:

```json
{"limit":5}
```

Output based on the real node1 registry:

```json
{
  "provenance": {"source":"opensvc_daemon","observed_at":"2026-09-24T15:30:00Z"},
  "node": "node1",
  "reported_total": 122,
  "total": 122,
  "count": 5,
  "drivers": [
    "app.forking",
    "app.simple",
    "array.freenas",
    "array.hds",
    "array.hoc"
  ],
  "next_cursor": "array.hoc",
  "truncated": true
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Missing global `root` grant | Tool error containing daemon HTTP `403` |
| Oversized or non-exact non-empty `node` | Tool error before the daemon request |
| Invalid page size or cursor | Tool validation error |
| Cursor no longer present after a registry change | Explicit stale-cursor tool error |
| Unexpected list/item kind, node, or driver name | Tool error; no partial list |
| Daemon or remote-node proxy unavailable | Tool error with transport context |

### `get_node_daemon_metrics`

Returns bounded Prometheus metric families exported by one running OpenSVC
daemon. This is an advanced diagnostic tool for hypotheses about daemon
activity, API latency or errors, scheduler activity, internal queues, Go
runtime behavior, and process resource consumption. It is not the primary
source for object, instance, resource, heartbeat, or cluster status.

#### OpenSVC API and scope

```text
GET /api/node/name/_/metrics
Accept: text/plain
```

The optional `node` input replaces `_` with one exact node name; OpenSVC may
proxy the read to that node. The MCP delegates the request JWT and leaves the
authorization decision to the daemon. The request does not probe resources or
change daemon state.

The endpoint exports metrics of the OpenSVC daemon process, including its Go
runtime and process collector. It does not represent general host, workload,
or cluster metrics. A counter is cumulative: one isolated value usually does
not establish a current rate or an anomaly. The MCP deliberately does not
calculate rates, compare thresholds, or emit a health conclusion.

#### Input, filtering, and pagination

| Input | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `node` | No | Local daemon (`_`) | Exact node name; at most 255 characters | Daemon whose metrics are read |
| `names` | No | All | At most 32 | Exact metric family names |
| `prefixes` | No | All | At most 32 | Metric family name prefixes |
| `limit` | No | 100 | 1..200 | Maximum families in the page |
| `cursor` | No | — | At most 1024 characters | Exact `next_cursor` from the preceding call with identical node and filters |

`names` and `prefixes` are combined using OR. Filtering is performed after
parsing the endpoint response. Families are sorted by exact name and paginated;
the response reports totals before filtering, after filtering, and for samples.
The source text is limited to 256 KiB, the filtered result to 5,000 samples,
and one family to 1,000 samples. These bounds make accidental full metric dumps
fail explicitly instead of silently dropping measurements.

Each family preserves its `name`, `help`, and Prometheus `type`. Samples expose
sorted labels and an optional source timestamp. Counter, gauge, and untyped
values are strings; summaries expose count, sum, and quantiles; histograms
expose count, sum, and cumulative buckets. Numeric strings preserve Prometheus
special values such as `NaN` and `+Inf` in valid JSON.

`target_node` is the requested node or `_`. Unlike JSON list endpoints, the
Prometheus response contains no metadata that reliably resolves `_` to a node
name, so the MCP does not invent one.

#### MCP properties

| Property | Value |
|---|---|
| Title | Get node daemon metrics |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon and its OpenSVC proxy path are used |
| Side effects | None |

#### Example

Input:

```json
{"prefixes":["opensvc_api_"],"limit":20}
```

Abbreviated output:

```json
{
  "target_node":"_",
  "reported_total":59,
  "total":2,
  "sample_total":5,
  "count":2,
  "returned_sample_count":5,
  "metrics":[{
    "name":"opensvc_api_requests_total",
    "help":"API requests.",
    "type":"counter",
    "sample_count":2,
    "samples":[{
      "labels":[{"name":"code","value":"200"},{"name":"method","value":"GET"}],
      "value":"12"
    }]
  }],
  "truncated":false
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Daemon refuses the delegated JWT | Tool error preserving the daemon status and bounded problem detail |
| Invalid node, filter, page size, or cursor | Tool validation error |
| Cursor no longer present with the same filters | Explicit stale-cursor tool error |
| Unexpected media type or malformed Prometheus text | Tool error; no partial metrics are returned |
| Source or filtered sample bounds exceeded | Explicit tool error suggesting narrower filters |
| Daemon or remote-node proxy unavailable | Tool error with daemon transport context |

### `probe_node_reachability`

Actively verifies that one exact OpenSVC daemon answers through the contacted
daemon's node proxy path.

#### OpenSVC API and interpretation

```text
GET /api/node/name/<node>/ping
```

The endpoint requires the global `root` grant. The target `node` is mandatory;
there is no implicit `_` default because probing the daemon already contacted
by the MCP adds little diagnostic evidence. `_` remains accepted when supplied
explicitly for a deliberate local control probe.

For a remote target, the contacted daemon first verifies that it has status
data for the node and that the node belongs to the cluster, then calls the
remote daemon with the delegated JWT. A successful result means the complete
path returned HTTP `204 No Content`:

```text
MCP -> contacted daemon -> OpenSVC proxy -> target daemon -> 204
```

This proves neither heartbeat health nor the state of daemon subsystems,
objects, instances, or resources. The endpoint is an HTTP application probe,
not ICMP. `round_trip_ms` measures the complete path above, including local MCP
and proxy overhead; it is not a pure network latency measurement.

#### Input and output

`node` is required and must be one exact OpenSVC node name of at most 255
characters. Paths, whitespace, wildcards, and selectors are rejected before
the daemon request.

Successful output fields are:

| Field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP completion time |
| `node` | Exact requested node or an explicitly supplied `_` alias |
| `reachable` | `true`, because only an exact `204` produces a successful result |
| `status_code` | `204` |
| `round_trip_ms` | End-to-end elapsed milliseconds measured by the MCP |

Failures remain explicit tool errors rather than successful
`reachable=false` results. This preserves the difference between a refused
JWT, an unknown node, missing status data, and a remote connection failure.

#### MCP properties

| Property | Value |
|---|---|
| Title | Probe node reachability |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon and its OpenSVC proxy path are used |
| Side effects | One active HTTP request to the selected daemon |

#### Example

Input:

```json
{"node":"node2"}
```

Output:

```json
{
  "provenance":{"source":"opensvc_daemon","observed_at":"2026-09-25T10:00:00Z"},
  "node":"node2",
  "reachable":true,
  "status_code":204,
  "round_trip_ms":1.42
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Missing global `root` grant | Tool error containing daemon HTTP `403` |
| Empty, oversized, or non-exact `node` | Tool validation error before the daemon request |
| Node has no status data | Tool error containing daemon HTTP `404` |
| Target is not a configured cluster node | Tool error containing daemon HTTP `400` |
| Target connection or request fails | Tool error containing daemon HTTP `500 Request peer` |
| Target returns anything other than `204` | Tool error; no reachability success is claimed |

### `get_node_logs`

Returns a finite, bounded list of recent OpenSVC `om` journal entries for one
exact node. Use it after `get_node_status` to inspect daemon activity around a
reported node, heartbeat, listener, scheduler, or orchestration issue. The
optional `component` selects one exact OpenSVC `PKG` value such as
`daemon/hbctrl`.

```text
GET /api/node/name/<node>/log?follow=false&lines=<lines+1>[&filter=PKG=<component>]
Accept: text/event-stream
```

The endpoint reads journald entries selected by `_COMM=om`. It does not filter
by systemd unit: an entry may have `systemd_unit=opensvc-server.service`, or it
may come from another `om` execution. It does not include messages emitted by
the systemd manager about the service or workload stdout and stderr. OpenSVC
requires the global `root` grant for this endpoint.

| Input | Required | Default | Bounds | Meaning |
|---|---:|---:|---:|---|
| `node` | Yes | — | Exact hostname using letters, digits, `.`, `_`, or `-`; at most 255 characters | Node whose journal is read |
| `lines` | No | 50 | 1..100 | Maximum entries returned |
| `component` | No | — | Exact value, at most 255 characters | `PKG` filter |

The MCP requests one extra entry to detect omission of older entries. It
consumes the SSE stream to EOF, retains at most `lines` entries in chronological
order, and returns ordinary JSON. `follow` is always `false`.

The output contains `provenance`, `node`, the effective `lines`, `count`,
`entries`, and `truncated`; `component` appears when requested. Each entry
contains `timestamp`, `message`, `message_truncated`, and optional `level`,
`priority`, `component`, `systemd_unit`, `object_path`, `resource_id`,
`session_id`, `event_id`, `request_id`, and `orchestration_id`. OpenSVC may omit
`level`; the journald `priority` is preserved separately when present. A raw
journald microsecond timestamp is converted to RFC 3339 when the OpenSVC
payload has no timestamp.

Messages are limited to 2,048 Unicode code points each and 64 Ki code points
across the response. Other fields are limited to 255 code points. Control and
formatting characters are normalized. Raw journald metadata such as machine
identifiers, command lines, and user IDs is omitted. `truncated` reports older
entries omitted by the requested line limit or message content shortened by
these bounds. Invalid inputs, malformed SSE or JSON, unexpected node or
component values, oversized streams, and daemon errors become MCP tool errors.

## Compatibility

Node status, capability, driver, and log behavior was verified against the OpenSVC
development branch used by the two-node lab. Node configuration redaction
requires OpenSVC main including `opensvc/om3#1125` until that change is
included in a tagged release.
