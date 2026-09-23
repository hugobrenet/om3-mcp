---
domain: node
tools:
  - get_node_config
  - get_node_status
  - get_node_logs
stability: experimental
---

# Node Tools

This document describes tools that read bounded node configuration evidence,
inspect the last-known state of one exact node, and read its recent OpenSVC
journal entries.

Implementation:

- business logic: `internal/core/node.go`;
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

`node` is required and must be one exact OpenSVC node name of at most 255
characters using letters, digits, dot, underscore, or hyphen. Leading and
trailing whitespace, paths, wildcards, and selectors are rejected before any
daemon request.

The output has the same `provenance`, `content`, `size_bytes`,
`returned_bytes`, `truncated`, and `redaction_requested` fields as
`get_cluster_config`, plus `node` containing the requested node. The 65,536-byte
MCP output limit, 1 MiB transport ceiling, UTF-8 requirement, and
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
{"node":"node1"}
```

Output:

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-23T10:00:01Z"},
  "node": "node1",
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
| Empty, oversized, or non-exact `node` | Tool error before the daemon request |
| Node or node configuration file missing | Tool error containing daemon HTTP `404` |
| Response above 1 MiB | Tool error before any content is returned |
| Invalid UTF-8 or unexpected media type | Tool error; no partial configuration is returned |
| Daemon or remote-node proxy unavailable | Tool error with daemon transport context |

### `get_node_status`

Returns the last-known status of one exact node after `get_cluster_status`
provides the cluster-wide facts. It exposes the reported agent
and compatibility versions, leader and overload flags, freeze and boot times,
monitor state and targets, capacity statistics, memory and swap policy
thresholds, and a bounded heartbeat assessment. Use `get_cluster_status` for a
factual cluster-wide snapshot. This node tool still classifies heartbeat state;
that contract is scheduled for the next refactor increment.

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
dates the monitor state and `heartbeat.updated_at` dates the published heartbeat
view. The heartbeat state follows the rules documented above. `stats` or
`policy` is `null` when that section is absent from the daemon response. An
unreported scalar field in `status` or `monitor` appears as its JSON zero value;
use `get_cluster_status` to inspect configured and reported node membership.

#### Input and output

`node` is required and must be one exact name of at most 255 characters.
Leading or trailing spaces and wildcard/selector characters are rejected. No
default node, selector, or pagination is used.

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `node` | Selected OpenSVC node name |
| `status` | Agent/API/compatibility versions, leader and overload flags, boot and freeze times |
| `monitor` | State, global and local targets, orchestration ID/completion, and update time |
| `stats` | Fifteen-minute load, available and total memory/swap, and OpenSVC capacity score; `null` if absent |
| `policy` | Minimum available memory and swap percentages; `null` if absent; zero disables the corresponding check |
| `heartbeat` | State, publication time, stream/link/RX-peer counts, and at most 50 structured issues with truncation metadata |

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

Illustrative output using the two-node lab's confirmed healthy heartbeat and
no-swap policy. Timestamps and capacity values are representative:

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-18T10:00:30Z"},
  "node": "node1",
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
    "state": "healthy",
    "updated_at": "2026-09-18T10:00:00Z",
    "streams_total": 2,
    "streams_running": 2,
    "links_total": 2,
    "links_beating": 2,
    "links_not_beating": 0,
    "rx_peers_beating": 1,
    "rx_peers_stale": 0,
    "issues_total": 0,
    "issues": [],
    "issues_truncated": false
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

Node status and log behavior was verified against OpenSVC `3.0.0-rc30`. Node
configuration redaction requires OpenSVC main including `opensvc/om3#1125`
until that change is included in a tagged release.
