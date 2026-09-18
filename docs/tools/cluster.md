---
domain: cluster
tools:
  - get_cluster_health
  - get_node_status
stability: experimental
---

# Cluster Tools

This document describes tools that assess the current OpenSVC cluster view.

Implementation:

- business logic: `internal/core/cluster.go` and `internal/core/node.go`;
- MCP definitions: `internal/tools/cluster.go`.

## Tools

### `get_cluster_health`

Computes a deterministic, point-in-time health assessment for the cluster, its
nodes, and actor objects visible to the delegated caller.

Use it for the first operational diagnosis: leadership, compatibility, frozen
or missing nodes, non-idle monitors, overload, and a bounded list of problematic
objects. It also summarizes heartbeat streams and peer links for each node.
Continue with object and instance tools for a focused diagnosis.

This is an MCP-defined assessment derived from OpenSVC fields, not a canonical
health flag returned by the daemon.

#### OpenSVC API and freshness

```text
GET /api/cluster/status
```

The daemon serves a cached cluster view. Refreshing that cache does not execute
resource status drivers. Consequently, `healthy=true` means no issue is present
in the visible last-known OpenSVC state; it does not prove that every resource
was probed during this call.

The endpoint accepts `guest` or a higher role. Without `selector` or
`namespace`, OpenSVC can serve its prepared cluster JSON directly to global
`guest`, `operator`, `admin`, or `root` callers. OpenSVC still filters the
response for namespace-scoped grants. Object summaries cover only namespaces
visible to the delegated JWT. A healthy result makes no assertion about
inaccessible namespaces.

#### Assessment rules

Cluster issues include:

- incompatible nodes;
- a frozen cluster;
- no node reporting itself as leader;
- more than one node reporting itself as leader.

Leader names are sorted lexicographically.

A node is unhealthy when it is missing, has no agent version or monitor state,
has a non-empty monitor state other than `idle`, is frozen, reports overload, or
has degraded or unavailable heartbeat status in a multi-node cluster.
Node issues are structured objects with a stable `code` and a `message`.
An unparseable non-empty `frozen_at` is treated conservatively as frozen. The
evaluated node set is the union of configured and reported nodes.

| Node issue code | Meaning |
|---|---|
| `node_status_missing` | A configured node has no published status |
| `agent_version_missing` | The node publishes no agent version |
| `monitor_state_missing` | The node publishes no monitor state |
| `monitor_state_not_idle` | The node monitor state is not `idle` |
| `node_frozen` | The node has a non-zero or invalid frozen timestamp |
| `memory_below_threshold` | Available memory violates the configured policy |
| `swap_below_threshold` | Available swap violates the configured policy |
| `overload_cause_undetermined` | The overload flag cannot be explained from the bounded data |
| `heartbeat_degraded` | A reported heartbeat stream is not running or a reported stream/peer link is not beating |
| `heartbeat_status_unknown` | Heartbeat data is absent, too old, has an invalid timestamp, or lacks an RX view of a configured peer |

Each reported node has a `heartbeat` object with `state`, OpenSVC
`updated_at`, stream and link counts, RX peer counts, and a bounded issue list.
`node_summary.heartbeat_degraded` and `node_summary.heartbeat_unknown` count
nodes in those states. For a single-node cluster with no streams, heartbeat
state is `not_applicable`.

| Heartbeat state | Rule |
|---|---|
| `healthy` | Recent status; every reported stream is `running`, every reported link is beating, and each configured peer has an RX link |
| `degraded` | A reported stream is not `running` or a reported link has `is_beating=false` |
| `unknown` | Status or timestamp is missing/invalid, older than three minutes, more than 30 seconds in the future, or a configured peer has no reported RX link |
| `not_applicable` | Single configured node and no heartbeat streams |

`heartbeat.updated_at` is written by OpenSVC when it publishes the heartbeat
subsystem. OpenSVC normally refreshes it within 60 seconds; the MCP allows three
minutes for propagation before classifying the view as `unknown`. A recent MCP
`provenance.observed_at` does not make old heartbeat data fresh. The MCP does
not use the stream's own `updated_at`. It uses OpenSVC's `is_beating` value
rather than inventing an age threshold for `last_beating_at`. It does not treat
every alert as a failure: OpenSVC also publishes informational alerts.

Heartbeat issue codes provide the exact evidence:

| Heartbeat issue code | Meaning |
|---|---|
| `heartbeat_stream_not_running` | A stream reports a state other than `running`; `stream_id` and `stream_state` identify it |
| `heartbeat_peer_not_beating` | A stream/peer link reports `is_beating=false`; `stream_id`, `peer`, `changed_at`, and `last_beating_at` identify it |
| `heartbeat_peer_stale` | All reported RX links to a configured peer are not beating, from this node's view |
| `heartbeat_rx_peer_missing` | No RX link to a configured peer is reported; reachability cannot be inferred |
| `heartbeat_status_missing` | No heartbeat status is published for a reported node |
| `heartbeat_timestamp_missing` | Publication time is absent, invalid, or the Go zero time |
| `heartbeat_status_stale` | Publication time is older than three minutes |
| `heartbeat_timestamp_in_future` | Publication time is more than 30 seconds ahead of the MCP clock |

Stream/peer issues are sorted by stream ID and peer name. At most 50 heartbeat
issues per node are returned; `issues_total` and `issues_truncated` disclose
omissions. The link counters include RX and TX. `rx_peers_stale` follows
OpenSVC's rule that a peer is stale from one node's point of view when none of
its RX links are beating. One failed link with another RX link still beating is
a degraded heartbeat, not proof that the peer is unreachable. A stale peer in
this cached view does not by itself prove a node failure or cluster partition.
Compare the local views on both nodes for a partition investigation.

When a node reports overload, the MCP reproduces the daemon's deterministic
checks using the node configuration and statistics:

- `memory_below_threshold` when `node.min_avail_mem_pct > 0` and the available
  memory percentage is below that threshold;
- `swap_below_threshold` when `node.min_avail_swap_pct > 0` and the available
  swap percentage is below that threshold;
- `overload_cause_undetermined` when the node reports overload but the required
  data is absent or the published values do not explain it.

Memory and swap issues include the values used by the comparison, the exact
configuration key, and conditional remediation options. These options describe
operator choices; the MCP never selects or applies one. An exact configuration
candidate is included only when a deterministic value exists for a clearly
stated condition. For example, a node with no swap receives the candidate
`node.min_avail_swap_pct=0` under the condition that the absence of swap is
intentional; the caller still has to establish that intent.

Only objects with an `avail` field are treated as actors. An actor is
problematic when at least one condition holds:

- availability is not `up`, `stdby up`, or `n/a`;
- overall status is `down`, `warn`, `undef`, or `stdby down`;
- a non-empty placement state is neither `optimal` nor `n/a`;
- a non-empty freeze state is not `unfrozen`;
- provisioned state is `false`, `mixed`, or `undef`.

Availability counters use these normalized values:

| Counter | Values |
|---|---|
| `up` | `up`, `stdby up` |
| `down` | `down`, `stdby down` |
| `warn` | `warn` |
| `not_applicable` | `n/a` |
| `other` | Any other value |

Problem objects are sorted by path. At most 100 are returned and
`problem_objects_truncated` indicates whether additional problems were omitted.

The top-level `healthy` field is true only when the cluster has no issue, every
evaluated node is healthy, and no visible actor object is problematic. Unknown
heartbeat state on a reported multi-node cluster node prevents a healthy result,
but is kept distinct from a confirmed degraded heartbeat.

#### MCP properties

| Property | Value |
|---|---|
| Title | Assess cluster health |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon is contacted |
| Side effects | None |

#### Input example

```json
{}
```

#### Overload output excerpt

```json
{
  "provenance": {
    "source": "opensvc_daemon",
    "observed_at": "2026-07-15T05:00:00Z"
  },
  "healthy": false,
  "node_summary": {
    "frozen": 0,
    "healthy": 0,
    "missing": 0,
    "non_idle": 0,
    "overloaded": 1,
    "total": 1
  },
  "nodes": [
    {
      "healthy": false,
      "is_frozen": false,
      "is_leader": true,
      "is_overloaded": true,
      "issues": [
        {
          "code": "swap_below_threshold",
          "message": "node has no swap while a minimum available swap threshold is enabled",
          "evidence": {
            "swap_total_mb": 0,
            "swap_available_pct": 0,
            "minimum_swap_available_pct": 10
          },
          "policy": {
            "config_key": "node.min_avail_swap_pct",
            "current_value": 10,
            "unit": "percent",
            "overloaded_when": "swap_available_pct < minimum_swap_available_pct",
            "disabled_when_value_is": 0
          },
          "remediation_options": [
            {
              "id": "provide_swap",
              "applies_when": "swap is expected on this node",
              "description": "configure swap capacity so OpenSVC can evaluate available swap against the threshold"
            },
            {
              "id": "disable_swap_threshold",
              "applies_when": "the absence of swap is intentional",
              "description": "disable the OpenSVC available swap check for this node",
              "configuration": {
                "key": "node.min_avail_swap_pct",
                "value": 0
              }
            }
          ]
        }
      ],
      "monitor_state": "idle",
      "name": "lab-node-01",
      "reported": true
    }
  ]
}
```

A full response also includes `cluster`, `object_summary`, `problem_objects`,
and `problem_objects_truncated`; they are omitted from this excerpt to keep the
overload contract readable.

`problem_objects` is sorted by canonical object path. Node and leader names are
also sorted for deterministic output.

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Insufficient daemon grants | Tool error containing daemon HTTP `403` |
| Daemon unavailable or malformed status | Tool error; no partial assessment |

### `get_node_status`

Returns the last-known status of one exact node after `get_cluster_health`
identifies a node that needs closer inspection. It exposes the reported agent
and compatibility versions, leader and overload flags, freeze and boot times,
monitor state and targets, capacity statistics, memory and swap policy
thresholds, and the bounded heartbeat assessment used by the cluster health
tool. Use `get_cluster_health` for a cluster-wide health decision; this tool has
no top-level `healthy` flag.

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
use `get_cluster_health` to diagnose missing status fields.

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

## Compatibility

Verified against OpenSVC `3.0.0-rc30`. Health rules must be reviewed whenever
OpenSVC adds or changes status values.
