---
domain: cluster
tools:
  - get_cluster_config
  - get_cluster_health
stability: experimental
---

# Cluster Tools

This document describes tools that read bounded cluster configuration evidence
and assess the current OpenSVC cluster view.

Implementation:

- business logic: `internal/core/config_file.go` and
  `internal/core/cluster.go`;
- MCP definitions: `internal/tools/cluster.go`.

## Tools

### `get_cluster_config`

Returns the cluster configuration file as bounded UTF-8 text. Use it when a
diagnosis depends on declared cluster settings such as node membership,
heartbeat definitions, listener settings, quorum policy, or shared node
defaults. It returns configuration evidence only; the MCP does not evaluate
keywords or derive a diagnosis from the file.

#### OpenSVC API, authorization, and freshness

```text
GET /api/cluster/config/file?redact-secrets=true
Accept: application/octet-stream
```

The MCP always sends `redact-secrets=true`. This choice is fixed and is not an
MCP input, so a model cannot request the unredacted form. The OpenSVC daemon
performs the redaction and requires the global `root` grant. The delegated JWT
therefore controls access at the daemon. The call reads the current
`cluster.conf` file rather than the cluster status cache and does not refresh
drivers or change daemon state.

Cluster-side redaction requires an OpenSVC daemon containing the fix merged in
`opensvc/om3#1125`. An older daemon can ignore this optional query parameter and
return an unredacted file, so the daemon must be upgraded before enabling this
tool against it.

#### Input and output

The input is an empty JSON object. There is no selector, pagination, node
impersonation, or redaction switch.

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `content` | Raw redacted configuration text, limited to 65,536 bytes and cut only at a valid UTF-8 boundary |
| `size_bytes` | Complete file size received from the daemon before MCP output truncation |
| `returned_bytes` | Number of bytes present in `content` |
| `truncated` | Whether the MCP omitted bytes after its 65,536-byte output limit |
| `redaction_requested` | Always `true`; confirms that the MCP sent `redact-secrets=true` |

The HTTP client rejects file responses above 1 MiB, invalid UTF-8, and media
types other than `application/octet-stream`. This transport ceiling is separate
from the smaller MCP output limit.

#### MCP properties

| Property | Value |
|---|---|
| Title | Get cluster configuration |
| Read-only | Yes |
| Destructive | No |
| Open world | No; only the configured daemon is contacted |
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
  "provenance": {"source": "opensvc_daemon", "observed_at": "2026-09-23T10:00:00Z"},
  "content": "[cluster]\nname = lab\nnodes = node1 node2\nsecret = ********\n",
  "size_bytes": 65,
  "returned_bytes": 65,
  "truncated": false,
  "redaction_requested": true
}
```

#### Errors

| Condition | Result |
|---|---|
| Invalid MCP JWT | MCP HTTP `401` |
| Missing global `root` grant | Tool error containing daemon HTTP `403` |
| Cluster configuration file missing | Tool error containing daemon HTTP `404` |
| Response above 1 MiB | Tool error before any content is returned |
| Invalid UTF-8 or unexpected media type | Tool error; no partial configuration is returned |
| Daemon unavailable | Tool error with transport context |

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

## Compatibility

Cluster health behavior was verified against OpenSVC `3.0.0-rc30`. Cluster
configuration redaction requires OpenSVC main including `opensvc/om3#1125`
until that change is included in a tagged release. Health rules must be
reviewed whenever OpenSVC adds or changes status values.
