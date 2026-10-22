---
domain: instance-actions
tools:
  - start_instance
  - stop_instance
  - restart_instance
  - freeze_instance
  - unfreeze_instance
  - clear_instance
stability: experimental
---

# Instance Action Tools

This document describes the tools that act on one instance of an OpenSVC
object, on one node. They are action tools: registered only when
`OPENSVC_MCP_ACTIONS=enabled`, under the contract of the
[action tools](actions.md), but for one point: the daemon of the node runs the
action at once, outside of the orchestration of the object, and the tool
waits a bounded time for its end, where an object action returns as soon as
the orchestration is queued.

Implementation:

- business logic: `internal/core/instance_action.go`, and
  `internal/core/execution_wait.go` for the wait on an execution, shared with
  the [node actions](node-actions.md);
- MCP definitions: `internal/tools/instance_action.go`.

## Tools

| Tool | OpenSVC API | Kinds | Destructive | Idempotent |
|---|---|---|---:|---:|
| `start_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/start` | svc, vol | No | Yes |
| `stop_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/stop` | svc, vol | Yes | Yes |
| `restart_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/restart` | svc, vol | Yes | No |
| `freeze_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/freeze` | svc, vol | No | Yes |
| `unfreeze_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/unfreeze` | svc, vol | Yes | Yes |
| `clear_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/clear` | svc, vol, cfg, nscfg, sec, usr | No | Yes |

They require `operator` on the object namespace.

- The five actions fork `om <path> instance <action>` on the node, and the
  daemon answers at once with the `session_id` and the `exec_id` of the
  execution. The MCP then reads `GET /api/node/name/<node>/daemon/exec/id/<exec_id>`
  until the execution ends, `wait_seconds`, 10 by default, 60 at most, and
  returns `completed`, `state`, `exit_code` and `error`. An execution still
  running returns `completed: false`, to follow with
  `list_daemon_executions` and `get_node_logs` filtered on `exec_id`. The
  `error` of an execution is the exit of its process: for an execution that
  failed, a non-zero or absent `exit_code`, the MCP reads once the last 20
  lines of its journal on the node, `GET /api/node/name/<node>/log` filtered
  on `EXEC_ID`, and returns them in time order as `log_tail`, with
  `log_truncated`, or `log_error` when the read fails; a success reads
  nothing, nor does an execution still running. These reads require `root`:
  for an operator, `execution_error` says so, and the action runs all the
  same; `get_instance_status` gives its outcome. A 410 in `execution_error`
  is an execution the daemon ran and has dropped since, not one it never
  ran.
- `stop_instance` without `rids`, on a service orchestrated `ha` or `start`,
  sets the stopped flag of this instance only. The daemon does not start it
  again on that node, but on an unfrozen ha failover service another instance
  becomes candidate and the daemon starts the service there: a failover. To
  stop the service everywhere, `stop_object`; to stop this instance without
  the daemon acting, `freeze_object` first.
- `start_instance` lifts the stopped flag of the instance. On a flex service
  above its `flex_target`, the daemon stops a non-leader instance in return.
  The daemon refuses with 403 a start beyond the claims of the namespace.
- The daemon starts an instance of a failover service while another runs on a
  peer, and then runs it on two nodes, on the same shared storage: the MCP
  reads the topology of the object and its instances, and refuses
  `start_instance` and `restart_instance` when another instance is `up` or
  `warn`. `switch_object` moves the service.
- `freeze_instance` freezes this node only: the daemon no longer elects it
  to run an ha service, and stops its automatic actions there.
  `unfreeze_instance` gives that node back to the daemon, destructive as
  `unfreeze_object`.
- `clear_instance` sets the monitor state of the instance to `idle`, and
  nothing else. The daemon sets it whatever the current state, idle under an
  action still running: the MCP reads the instance first, and accepts only
  the failed states, `boot failed`, `shutdown failed`, `start failed`,
  `stop failed`, `freeze failed`, `unfreeze failed`, `provision failed`,
  `unprovision failed`, `delete failed`, `resize failed`, `cap failed` and
  `prepare failed`. It reads the instance again for 10 seconds at most, and
  returns `previous_state`, `current_state` and `cleared`. The daemon lifts a
  `start failed` itself when the object becomes up on another node: the clear
  serves when the object stays down.

### Input and output

The input is `path`, one exact canonical object path, and `node`, one exact
node name, never the `_` alias; the MCP refuses a node where the object has
no instance. `rids`, exact resource ids, applies to start, stop and restart,
and the daemon refuses an id the object has not; `disable_rollback` to start
and restart; `interrupt_syncs` to stop; `config_updated_at`, the
`last_modified` of `update_object_config`, to the five actions, for which the
daemon refuses with 409 a node whose configuration is older. `force`,
`force_affinity`, `to`, `move_to`, `sync_to`, `master`, `slave`, `slaves`,
`subset`, `tag` and `session_id` are not offered.

The output of the five actions holds `provenance`, `object`, `node`,
`action`, `resources`, `session_id`, `exec_id`, `instance_before`, the instance
read before the action, not its state after it, then `completed`, `state`, `exit_code`, `error`,
`execution_error`, and for a failure `log_tail`, `log_truncated` and
`log_error`.
