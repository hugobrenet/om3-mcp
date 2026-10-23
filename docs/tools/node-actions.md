---
domain: node-actions
tools:
  - freeze_node
  - unfreeze_node
  - drain_node
  - abort_node
  - clear_node
  - refresh_node_capabilities
  - refresh_node_hardware
  - refresh_node_disks
  - refresh_node_packages
  - scan_node_scsi
  - dequeue_node_actions
  - refresh_resource_info
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
| `refresh_node_capabilities` | `POST /api/node/name/<node>/action/scan/capabilities` | No | Yes |
| `refresh_node_hardware` | `POST /api/node/name/<node>/action/push/asset` | No | Yes |
| `refresh_node_disks` | `POST /api/node/name/<node>/action/push/disk` | No | Yes |
| `refresh_node_packages` | `POST /api/node/name/<node>/action/push/pkg` | No | Yes |
| `scan_node_scsi` | `POST /api/node/name/<node>/action/scsi/scan[?hba=…&target=…&lun=…]` | No | Yes |
| `dequeue_node_actions` | `POST /api/node/name/<node>/action/dequeue` | Yes | No |
| `refresh_resource_info` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/info[?rid=…]` | No | Yes |

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

`register` and `sysreport` are not offered, for the reasons the end of the
next section gives.

## Refreshes

Four reads of the MCP serve caches the node writes, not the live state:
`list_node_capabilities` reads `capabilities-v3.json`, which the daemon
writes at its start and when the packages change; `list_node_hardware`,
`list_node_disks` and `list_node_packages` read the system, disk and package
caches the push actions write; `list_resource_info` reads the resource
information an instance info computes. A cache missing answers 404 "waiting
for cached value", the error an agent meets on a new node. The daemon gives
no time for these caches: the refresh tools count the entries of the cache
before and after, `cache_entries_before` and `cache_entries_after`, and the
date of a cache is the one of its file on the node.

Each tool posts the action, which the node runs as an execution, and waits
for it as the freeze does: `completed`, `state`, `exit_code`, `error`, and
`log_tail` for a failure. They require `root`, but `refresh_resource_info`,
`operator` on the namespace.

- `refresh_node_capabilities` rescans the capabilities.
- `refresh_node_hardware`, `refresh_node_disks` and `refresh_node_packages`
  run `push asset`, `push disk` and `push pkg`: the node probes, writes the
  cache, then sends it to the collector, in that order. Without a collector,
  or with one refusing, the cache is written and the execution fails on the
  send, `exit_code` 1: no failure of the refresh, the descriptions say so.
  The om command writes that error to its standard error, out of the
  journal: `log_tail` is empty, and the cause unsaid, as seen on dev5n1 on
  2026-10-23, where `system.json` and `disks.json` went from 9 October to
  that day while the model, reading `exit_code` 1, an empty `log_tail` and
  the same number of entries, concluded the cache had not been refreshed. The inventory of many nodes in
  the collector is refreshed by `queue_node_action` of oc3-mcp, by filter.
- `scan_node_scsi` scans the SCSI hosts for new devices: every host, or one
  host by its port name, an FC WWN, with or without `0x`, or an iSCSI
  initiator name `iqn.…`, a target of it by its port name, a logical unit of
  that target, a number or `-`. om3 resolves `hba` and `target` from these
  names, not from numbers, ignores a target without its host, scanning every
  host, and a logical unit without its target: the MCP refuses those
  combinations. A scan adds what the kernel finds and removes nothing. An
  om3 defect makes `hba` alone write the host number as the bus in the scan
  file of every host: only the whole scan, and `hba` with `target` and
  `lun`, scan what they say. A new
  LUN shows in `list_node_disks` once `refresh_node_disks` rebuilt the cache.
- `dequeue_node_actions` runs `om node dequeue`: the node reads its queue on
  the collector, acknowledges it, then runs each entry as `om node <args>` or
  `om svc -s <service> <args>` and sends back their results. The queue may
  hold any command an administrator of the collector queued, service actions
  and array disk creations among them, and the MCP does not read it before:
  `list_node_actions` of oc3-mcp reads it. Destructive, not idempotent. A
  node without collector fails with `exit_code` 1, the error, collector not
  configured, written out of the journal: `log_tail` holds only "fetch
  actions from collector".
- `refresh_resource_info` recomputes the resource information of an
  instance, `rids` narrowing it, `config_updated_at` accepted.

Not offered: `sysreport`, which sends system files to the collector and
refreshes nothing the MCP reads, left to `queue_node_action` of oc3-mcp, and
`register`, which binds the node to a collector and writes `node.uuid`.

