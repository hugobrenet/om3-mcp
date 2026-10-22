---
domain: node-actions
tools:
  - freeze_node
  - unfreeze_node
  - drain_node
  - abort_node
  - clear_node
stability: experimental
---

# Node Action Tools

This document describes the tools that act on one node of an OpenSVC
cluster. They are action tools: registered only when
`OPENSVC_MCP_ACTIONS=enabled`, under the contract of the
[action tools](actions.md). They all require `root`, which each description
says, `get_caller_identity` being how the agent knows it has it.

Implementation:

- business logic: `internal/core/node_action.go`, and
  `internal/core/execution_wait.go` for the wait on an execution, shared with
  the [instance actions](instance-actions.md);
- MCP definitions: `internal/tools/node_action.go`.

## Tools

| Tool | OpenSVC API | Destructive | Idempotent |
|---|---|---:|---:|
| `freeze_node` | `POST /api/node/name/<node>/action/freeze` | No | Yes |
| `unfreeze_node` | `POST /api/node/name/<node>/action/unfreeze` | Yes | Yes |
| `drain_node` | `POST /api/node/name/<node>/action/drain` | Yes | Yes |
| `abort_node` | `POST /api/node/name/<node>/action/abort` | No | Yes |
| `clear_node` | `POST /api/node/name/<node>/action/clear` | No | Yes |

Every tool takes `node`, one exact node name, never the `_` alias, and reads
the node status first: a node absent from the cluster status is refused
before the call.

- `freeze_node` and `unfreeze_node` fork `om node freeze|unfreeze` on the
  node, and the daemon answers at once with the `session_id` and the
  `exec_id`. The MCP waits for the end of the execution as for the instance
  actions, `wait_seconds`, 10 by default, 60 at most, and returns
  `completed`, `state`, `exit_code`, `error`, and for a failure `log_tail`,
  the end of its journal. A frozen node is no longer a candidate to start an
  ha service, and its monitor starts or stops nothing there on its own; the
  instances running there keep running. It is distinct from the freeze of an
  instance or of an object. `unfreeze_node` gives the node back to the
  daemon, destructive as `unfreeze_object`; an ha service that moved away
  while the node was frozen does not come back on its own, `giveback_object`
  brings it back.
- `drain_node` sets the local target of the node monitor to `drained`, and
  the daemon answers the `orchestration_id`, or 409 for a drain already
  asked. The monitor freezes the node when it is not, then runs
  `om '*/svc/*' instance shutdown` on its local instances, standby resources
  included, skipped when it holds no svc. The shutdown does not mark the
  instances stopped: an ha service moves to another node while this one is
  frozen; a service orchestrated `no` or `start` stays down, even after the
  unfreeze. The node stays frozen: `unfreeze_node` is the next step. The MCP
  reads `GET /api/node/name/<node>/daemon/orchestration/id/<id>` on the
  drained node until it ends, `wait_seconds`, and returns `completed`,
  `state` (`succeeded`, `failed`, `aborted`), `error`, then reads the node
  again for `monitor_state`, `local_expect` and `frozen_at`. The orchestration
  is followed rather than the node monitor: the cluster status a daemon
  serves holds the monitor of a peer as its last heartbeat gave it, and a read
  too early would see `local_expect` still `none`. A drain of several
  services often outlasts the wait: `completed: false` is then normal. Before
  the drain, the MCP reads the instances of the node,
  `GET /api/instance?node=<node>`, and returns as `instances_up` the svc
  instances `up`, `warn` or `stdby up`, the ones the shutdown stops, 100 at
  most. Listing them before the call was left to the agent at first: with
  one object per `list_object_instances`, it spent its tool calls on the
  listing and never drained.
- `abort_node` sets the local target of the node monitor to `none`: what the
  drain had still to do is not done, the shutdown already launched runs to
  its end, and a node the drain froze stays frozen. The daemon answers at
  once, without waiting for the monitor: on a node running no orchestration,
  it answers 200 all the same, and the monitor records the abort `refused`,
  "node monitor local expect is already set to the same value none". The MCP
  reads the orchestration of the abort, 10 seconds at most, and returns
  `completed`, `state` and `error`, as for the drain.
- `clear_node` sets the node monitor state to `idle`. The daemon sets it
  whatever the current state, and answers 200 even when it is already
  `idle`: the MCP reads the node first, and accepts only the failed states,
  `freeze failed`, `unfreeze failed`, `drain failed` and `shutdown failed`. It
  reads the node again for 10 seconds at most, and returns
  `previous_state`, `current_state` and `cleared`. A new drain or freeze
  lifts a failed state itself: the clear serves when nothing is asked again.

`dequeue`, `push/asset`, `push/disk`, `push/pkg`, `register`,
`scan/capabilities`, `scsi/scan` and `sysreport` are not offered.
