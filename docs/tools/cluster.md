---
domain: cluster
tools:
  - get_cluster_health
stability: experimental
---

# Cluster Tools

This document describes tools that assess the current OpenSVC cluster view.

Implementation:

- business logic: `internal/core/cluster.go`;
- MCP definitions: `internal/tools/cluster.go`.

## Tools

### `get_cluster_health`

Computes a deterministic, point-in-time health assessment for the cluster, its
nodes, and actor objects visible to the delegated caller.

Use it for the first operational diagnosis: leadership, compatibility, frozen
or missing nodes, non-idle monitors, overload, and a bounded list of problematic
objects. Continue with object and instance tools for a focused diagnosis.

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
has a non-empty monitor state other than `idle`, is frozen, or reports overload.
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
evaluated node is healthy, and no visible actor object is problematic.

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

The real response also includes `cluster`, `object_summary`, `problem_objects`,
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

Verified against OpenSVC `3.0.0-rc30`. Health rules must be reviewed whenever
OpenSVC adds or changes status values.
