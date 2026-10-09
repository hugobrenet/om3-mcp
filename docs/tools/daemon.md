---
domain: daemon
tools:
  - list_daemon_executions
  - list_daemon_orchestrations
  - list_dns_records
stability: experimental
---

# Daemon Tools

This document describes tools that read what one OpenSVC daemon runs and
serves: its executions, orchestrations, and DNS zone. The process and
subsystem states of a node daemon are reported by `get_node_status`.

Implementation:

- business logic: `internal/core/daemon.go`, `internal/core/daemon_execution.go`,
  `internal/core/daemon_orchestration.go`, and `internal/core/daemon_dns.go`;
- MCP definitions: `internal/tools/daemon.go`.

## Tools

### `list_daemon_executions`

Returns commands currently running or recently retained by the OpenSVC daemon
on one exact node. Use it to correlate a diagnostic with a session,
orchestration, object, resource, origin, exit code, or daemon-reported error.
It preserves the exact execution state and does not infer success, failure, or
cluster health. To read what an execution logged, pass its `exec_id` to
[`get_node_logs`](node.md#get_node_logs) with the same node. Filter with
`exec_id` to read one execution: the daemon answers the same record for one
id as in its list.

#### OpenSVC API

```text
GET /api/node/name/{node}/daemon/exec
```

`node` is required, as for every [node target](README.md#node-targets). The
endpoint requires the OpenSVC `root` grant. The MCP forwards only the daemon's
native filters: repeated `state` and `origin` values, `session_id`,
`orchestration_id`, `exec_id`, `selector` for one exact object path, and `rid`.

The daemon retains a bounded recent execution history; records can disappear
between pages. The MCP sorts the current result by `started_at` descending and
then `exec_id`, applies a page size of 50 by default and 100 at most, and emits
an opaque `next_cursor`. Reuse a cursor only with the same filters. A cursor
whose execution has left the daemon history produces an explicit tool error.

Commands are limited to 2048 runes, titles to 512, errors to 4096, and the
aggregate page text to 128 Ki runes. Each shortened field has a corresponding
`*_truncated` flag. IDs and timestamps are validated before data is returned.

#### MCP properties

| Property | Value |
|---|---|
| Title | List daemon executions |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon is contacted |
| Side effects | None |

#### Input example

```json
{
  "node": "node-a",
  "states": ["failed", "running"],
  "origins": ["api", "scheduler"],
  "object_path": "prod/svc/redis",
  "limit": 20
}
```

`node` is required; the filters are optional. `node` and `object_path` must
be exact values, not selectors. `session_id`, `orchestration_id`, and `exec_id`, when present, must
be canonical UUIDs. At most 16 exact state values and 16 exact origin values
are accepted. Unknown state and origin strings are forwarded and preserved.

#### Output shape example

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-24T10:02:00Z"},
  "node": "node-a",
  "total": 1,
  "count": 1,
  "executions": [
    {
      "session_id": "10000000-0000-0000-0000-000000000001",
      "exec_id": "20000000-0000-0000-0000-000000000001",
      "orchestration_id": null,
      "node": "node-a",
      "path": "prod/svc/redis",
      "origin": "scheduler",
      "rid": "container#redis",
      "title": null,
      "title_truncated": false,
      "command": "om prod/svc/redis status",
      "command_truncated": false,
      "state": "succeeded",
      "error": null,
      "error_truncated": false,
      "exit_code": 0,
      "started_at": "2026-09-24T10:01:00Z",
      "ended_at": "2026-09-24T10:01:01Z",
      "pid": null
    }
  ],
  "truncated": false
}
```

The top-level `node` is the requested node; every execution must report it, or
the MCP returns an error. Optional facts remain `null` when absent. In
particular, a running execution can have no `exit_code` or `ended_at`; the MCP
does not synthesize them. Command and error text can expose sensitive
operational arguments, which is why this tool is root-only and should be
requested only when execution history is needed.

#### Errors

| Condition | Result |
|---|---|
| Invalid input, page size, UUID, or cursor | Tool validation error before the daemon call |
| Insufficient daemon grants | Tool error containing daemon HTTP `403` |
| Cursor record no longer retained | Explicit stale-cursor tool error |
| Malformed kind, ID, timestamp, or oversized identity field | Tool error; no partial list |
| Daemon unavailable | Tool error with transport context |

### `list_daemon_orchestrations`

Returns target-state orchestrations currently running or recently retained by
one OpenSVC daemon. An orchestration describes the intent accepted by a
monitor and its overall outcome; individual commands run beneath it are
returned by `list_daemon_executions` when filtered with the same
`orchestration_id`.

#### OpenSVC API

```text
GET /api/node/name/{node}/daemon/orchestration
```

`node` is required, as for every [node target](README.md#node-targets). The
endpoint requires the OpenSVC `root` grant.

The MCP forwards repeated `state` filters and an optional `selector` query.
Although the API parameter is named `selector`, the daemon implementation
matches it as one exact object path; the MCP therefore exposes it as
`object_path` and rejects wildcard syntax.

Ended orchestrations are retained by the daemon for approximately one hour,
up to 1,000 records; running records are not removed by those retention
limits. The MCP sorts the current result by `started_at` descending and then
`orchestration_id`, applies a page size of 50 by default and 100 at most, and
returns an opaque `next_cursor`. Reuse a cursor only with the same filters.

Target-state text is limited to 512 runes, error text to 4096, and aggregate
page text to 128 Ki runes. Truncated fields carry explicit flags.

#### MCP properties

| Property | Value |
|---|---|
| Title | List daemon orchestrations |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon is contacted |
| Side effects | None |

#### Input example

```json
{
  "node": "node-a",
  "states": ["failed", "running"],
  "object_path": "prod/svc/redis",
  "limit": 20
}
```

All fields are optional. At most 16 exact state values are accepted. The MCP
does not restrict states to an enum: the daemon code can report `failed` even
though the current OpenAPI description lists only `running`, `succeeded`,
`aborted`, and `refused`. Unknown future values are also preserved verbatim.

#### Output shape example

```json
{
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-24T11:00:00Z"},
  "node": "node-a",
  "total": 1,
  "count": 1,
  "orchestrations": [
    {
      "orchestration_id": "30000000-0000-0000-0000-000000000001",
      "node": "node-a",
      "path": "prod/svc/redis",
      "expect": "started",
      "expect_truncated": false,
      "state": "succeeded",
      "error": null,
      "error_truncated": false,
      "started_at": "2026-09-24T10:59:30Z",
      "ended_at": "2026-09-24T10:59:35Z"
    }
  ],
  "truncated": false
}
```

The top-level `node` is the requested node, whose daemon reported the list,
even when it is empty. The `node` of an orchestration is the node that accepted
it and may differ. `path` is `null` for a node orchestration. That `node` can
be an empty string when the queried daemon learned about the orchestration
through participating monitors but did not observe which node accepted it.
`expect`, `error`, and `ended_at` also remain `null` when the daemon omits
them; an absent `ended_at` commonly means that the orchestration is still
running. The MCP neither infers an outcome nor checks whether the requested
target state was operationally appropriate.

#### Errors

| Condition | Result |
|---|---|
| Invalid node, object path, state filter, page size, or cursor | Tool validation error before the daemon call |
| Insufficient daemon grants | Tool error containing daemon HTTP `403` |
| Cursor record no longer retained | Explicit stale-cursor tool error |
| Malformed kind, UUID, path, timestamp, or oversized identity field | Tool error; no partial list |
| Daemon unavailable | Tool error with transport context |

### `list_dns_records`

Lists the records of the cluster DNS zone one daemon serves. Use it to check
whether a name resolves to the expected address, or which names point to an
address.

#### OpenSVC API, authorization, and freshness

```text
GET /api/node/name/{node}/daemon/dns/dump
```

The daemon requires the global `root` grant, and proxies the request to the
named node. Each daemon builds its zone from the instance status of the whole
cluster and from `cluster.dns`, and keeps it current as the status changes:
the zones of two nodes normally match. The dump is the zone as built, whether
or not a nameserver serves it. A zone without `SOA` and `NS` records means
`cluster.dns` declares no nameserver; the DNS subsystem state and its
nameservers are reported by `get_node_status`.

The zone of a cluster `<cluster>` holds, for each address an instance resource
reports:

| Name | Type | Published |
|---|---|---|
| `<name>.<namespace>.<kind>.<cluster>.` | `A` or `AAAA` | Only for the addresses serving the object: a resource up, and for a standby resource, an instance serving. An address every instance reports is the object's own and is published once, whatever the instance states |
| `<name>.<namespace>.<kind>.<node>.node.<cluster>.` | `A` or `AAAA` | For every instance, whatever its state |
| `<index>.` or `<hostname>.` before either name | `A` or `AAAA` | The name of the resource, by its index or its `hostname` |
| The reverse name of the address | `PTR` | Pointing to the resource name, or to the object name when the resource has none |
| `_<port>._<network>.<name>.<namespace>.<kind>.<cluster>.` | `SRV` | For each `expose` of the resource, weighted by the node score |

A name missing from the zone is therefore a fact about the instances: a
stopped object keeps its node affine names but loses its object name, unless
every instance reports the same address.

#### Input

| Input | Meaning |
|---|---|
| `node` | Required exact node name, following the [node target](README.md#node-targets) rule |
| `name` | Optional exact name, case insensitive, with or without its final dot |
| `type` | Optional type: `A`, `AAAA`, `PTR`, `SRV`, `SOA` or `NS` |
| `content` | Optional exact content; an address matches whatever its spelling |
| `object` | Optional exact object path: the records whose name, or for `PTR` whose content, is a name of the object |
| `limit`, `cursor` | Page size between 1 and 200, default 100, and the `next_cursor` of a previous call |

#### Output

`provenance`, `node`, the filters applied, `reported_total` (records in the
zone), `total` (matching records), `count`, `records`, `next_cursor` and
`truncated`. Each record holds `name`, `type`, `ttl` and `content`. Records are
sorted by name, type and content; records the daemon repeats, such as one
`SOA` per nameserver, are listed once.
