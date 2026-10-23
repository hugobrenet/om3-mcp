---
domain: cluster-actions
tools:
  - freeze_cluster
  - unfreeze_cluster
  - abort_cluster_orchestration
stability: experimental
---

# Cluster Action Tools

This document describes the tools that freeze and unfreeze the whole OpenSVC
cluster and abort its orchestration. They are action tools: registered only
when `OPENSVC_MCP_ACTIONS=enabled`, under the contract of the
[action tools](actions.md), and require `root`.

Implementation:

- business logic: `internal/core/cluster_action.go`, with the wait on an
  orchestration of `internal/core/node_action.go`;
- MCP definitions: `internal/tools/cluster_action.go`.

## Tools

| Tool | OpenSVC API | Destructive | Idempotent |
|---|---|---:|---:|
| `freeze_cluster` | `POST /api/cluster/action/freeze` | No | Yes |
| `unfreeze_cluster` | `POST /api/cluster/action/unfreeze` | Yes | Yes |
| `abort_cluster_orchestration` | `POST /api/cluster/action/abort` | No | Yes |

The node answering sets the global target of its monitor, `frozen`,
`unfrozen` or `aborted`, with an orchestration id, and answers
`OrchestrationQueued`, `{orchestration_id, node}`. The other nodes adopt the
most recent global target, and each runs `om node freeze` with the cluster
scope, which marks the freeze as a freeze of the cluster: a node down adopts
it when it comes back. Each node sets its global target back to `none` once
its `frozen_at` is set. A cluster already frozen ends at once, without a
refusal; the unfreeze is the mirror. The daemon refuses with 409 a freeze or
an unfreeze while a node holds another global target, "more recent value",
or runs an action, "node X state is …"; the abort is not refused, sets
`aborted` then `none` on every node that adopts it, and each takes its own
drain out of its local target `drained`: a drain running on any node is
interrupted by an abort of the cluster, not only on the node answering.

A frozen cluster stops the decisions of the monitors, failovers, restarts and
starts of ha services; the services keep running. It differs from the freeze
of a node by its scope. `unfreeze_cluster` gives the automatic failovers
back to the whole cluster: destructive.

The tools read the cluster status first, to fail cleanly on a cluster that
does not answer, then follow the orchestration of the node the answer names,
`GET /api/node/name/<node>/daemon/orchestration/id/<id>`, `wait_seconds`, 10
by default, 60 at most, 10 seconds for the abort, and return `completed`,
`state` and `error`. They read the cluster again for `is_frozen`, each node
and its `frozen_at`, and `pending_nodes`: for a freeze the nodes not frozen
yet, for an unfreeze those still frozen. The orchestration followed is that of
one node; the others converge after it.
