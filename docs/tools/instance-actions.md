---
domain: instance-actions
tools:
  - start_instance
  - stop_instance
  - restart_instance
  - freeze_instance
  - unfreeze_instance
  - clear_instance
  - unprovision_instance
  - mark_instance_unprovisioned
  - run_task
  - start_instance_standby
  - shutdown_instance
  - update_instance_caps
  - reset_instance_caps
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
| `unprovision_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/unprovision[?rid=…&leader=true]` | svc, vol | Yes | Yes |
| `mark_instance_unprovisioned` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/unprovision?state_only=true[&rid=…]` | svc, vol | No | Yes |
| `run_task` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/run?rid=task#…[&env=K=V…]` | svc, vol | Yes | No |
| `start_instance_standby` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/startstandby[?rid=…]` | svc, vol | No | Yes |
| `shutdown_instance` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/shutdown[?rid=…]` | svc, vol | Yes | Yes |
| `update_instance_caps` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/pg/update[?rid=…]` | svc, vol | Yes | Yes |
| `reset_instance_caps` | `POST /api/node/name/<node>/instance/path/<ns>/<kind>/<name>/action/pg/reset[?rid=…]` | svc, vol | No | Yes |

They require `operator` on the object namespace; `unprovision_instance`,
`mark_instance_unprovisioned`, `run_task`, `update_instance_caps` and
`reset_instance_caps` require `admin`, as the daemon asserts it.

- The twelve actions fork `om <path> instance <action>` on the node, and the
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
  `unprovision failed`, `delete failed`, `resize failed`, `cap failed`,
  `prepare failed` and `purge failed`. It reads the instance again for 10 seconds at most, and
  returns `previous_state`, `current_state` and `cleared`. The daemon lifts a
  `start failed` itself when the object becomes up on another node: the clear
  serves when the object stays down.
- `unprovision_instance` stops then unprovisions the resources of the
  instance, or those of `rids`, triggers included, which destroys the data
  of the disk and fs resources, outside of the orchestration of the object.
  `leader: true` unprovisions the resources the instances share too, which
  the other instances lose; by default they are left, and
  `unprovision_object` unprovisions every node in order. The daemon looks at
  nothing first: the MCP refuses an instance `up` or `warn`, `stop_instance`
  first. `to`, `subset`, `tag`, `slaves`, `master` and `force` are not
  offered.
- `mark_instance_unprovisioned` sends the same unprovision with
  `state_only=true`: the daemon records each resource, or those of `rids`, as
  unprovisioned, its provisioned value and record, and touches nothing,
  stops nothing. It serves a sysadmin who freed the resources by hand; the
  resources still there are then orphaned, nothing frees them later. It is
  not destructive, and an instance up is not refused. `leader` does not
  apply. The record is undone by provisioning again, `provision_object`,
  with `adopt` for resources still there.
- Why a tool of its own: asked on 2026-10-23 to mark an instance
  unprovisioned "without touching anything", a model called
  `unprovision_instance`, the name matching the request, before reading its
  description, two times out of three even once its first sentence said it
  always acts on the resources; only the guard on the instance up stopped
  it. A record without a real cleanup leaves an orphan; an unprovision in
  place of a record destroys the data. A name matching the request better
  steers the model to the lesser, reversible, one, and
  `unprovision_instance` points to it.

### Standby resources, shutdown and caps

Two stopped flags are not to be confused. The flag of the instance, which the
ha orchestration reads, "the instance is flagged stopped", is set by a stop
and lifted by a start; none of the four actions below touches it. The flag
of a resource, a file per resource that keeps the daemon from starting it
again, is set by a stop with `rid`; `start_instance_standby` lifts it on
every resource of the instance, and `shutdown_instance` without `rids` too.

- `start_instance_standby` starts the resources `standby=true` only, such as
  a disk or a filesystem kept up on a passive node: the instance shows
  `stdby up`. Lifting the flag of the resources gives back to the daemon the
  right to start again a resource a stop with `rid` stopped. It takes `rids`
  and `disable_rollback`, as `start_instance`. Not destructive.
- `shutdown_instance` stops every resource, the standby ones a stop leaves
  up too, interrupts the data syncs and runs the pre and post shutdown
  triggers: what precedes the shutdown of the node. Not flagging the
  instance stopped, on an object `orchestrate=ha` it is undone at once: the
  daemon, seeing the instance down without the flag, starts it again here
  when the node leads the placement, or fails over. The MCP reads the
  orchestration and the freeze of the object, the freeze of the instance and
  of the node, and refuses the shutdown of an instance of an ha object none
  of them freezes: `freeze_instance` on that node first, the narrowest freeze,
  which leaves the service free to fail over, then the shutdown again. The
  drain of a node freezes the node itself before its shutdowns.
  `stop_instance` flags the instance, but leaves the standby resources up:
  the refusal says so, since a model offered it as the way out of the guard
  in the recette of 2026-10-23, where the shutdown was asked before a
  reboot. It takes `rids`, not `force`. Destructive.
- `update_instance_caps` writes the `pg_*` keywords of the configuration in
  the cgroups of the instance on that node, as a start does; a memory limit
  lowered under what the instance uses ends its processes: destructive.
  `update_object_config` changes a value first; `cap_object` applies the
  limits on every node by orchestration.
- `reset_instance_caps` writes what a node that never capped anything holds,
  within the slice of the object, including a capping the configuration
  does not know: one left by an older agent, by systemd or by hand. The
  instance runs without limits until the next update or start, which writes
  the `pg_*` keywords configured again; a `pg_*` keyword set to `default`
  lifts one for good. Not destructive.
- Neither caps action is atomic: one file written per controller, an
  execution that fails may have written, or lifted, part of the limits, and
  `log_tail` names the file refused. The descriptions say so: in the recette
  of 2026-10-23, a reset failed on `cpuset.cpus`, a controller not delegated
  to the slice, after `memory.max` was lifted, and the model, reading
  `failed`, concluded that no limit had been lifted.
- `prstart` and `prstop`, the SCSI persistent reservations, are not offered:
  no lab disk to try them on, and `prstop` on an instance running lifts the
  only protection against a write from another node. `boot` neither: it
  serves the start of a node, before the daemon.

### Running tasks

A task is a resource, `task#<name>`, whose driver runs a command: `task.host`
on the node, `task.docker`, `task.podman` or `task.oci` in a container. Its
status stays `n/a`: a task runs on demand, or on its schedule, and an object
of tasks alone needs no start. `run_task` forks `om <path> instance run --rid
<tasks>` on the node.

- `rids` is required, one `task#<name>` at least: a run without `rid` runs
  every task of the instance, the other resources ignoring it. The MCP reads
  the `<group>.type` keywords and refuses a task the object has not.
- A task with `confirmation=true` asks a human to confirm each run. The
  daemon runs it with `--confirm`, or, without a tty, aborts it at once, "run
  aborted (stdin is not a tty)". The MCP never sends `--confirm`, and offers
  no input for it: the model writes the arguments, and the confirmation card
  of an agent would show a `confirm: true` the model wrote, not a human. It
  reads `confirmation` before the call, the value scoped to the node first,
  and refuses such a task, naming `om <path> instance run --rid <task>
  --confirm` on the node; a value it cannot read as false, such as a
  reference, is taken as true.
- `env` exports `KEY=VALUE` variables to the commands, after those of the
  configuration: at most 32, 4096 bytes each, one line, the key
  `^[A-Z_][A-Z0-9_]*$`. `PATH`, `LD_*` and `OPENSVC_*` are refused: they
  change what the command runs rather than what it does. The values are
  never returned. The journal of the execution, which `log_tail` and
  `get_node_logs` with `exec_id` read, shows them masked, `--env KEY=xxx`;
  the daemon line of the API request, under the same session but without
  the exec id, shows them in clear, an om3 defect: `get_node_logs` filtered
  on `session_id` returns it.
- The task decides whether it runs: `run_requires`, `max_parallel` ("task is
  already running n times") and `snooze` refuse a run inside the execution,
  which fails; the reason is in `log_tail`. The MCP does not guard them.
- After the wait, the MCP reads the instance status again, `last_run_exitcode`,
  `last_run_at` and `last_run_session_id` in the `info` of each task, which
  the task writes at the end of each run, `log` or not. A run refused before
  it ran writes none: `this_run`, the last run of the session of the call,
  tells them apart. The status publishes the run a moment after the
  execution ends: the MCP reads again up to 5 seconds until it shows.
- `output_logged` is the `log` keyword of the task: true, its stdout goes to
  the journal at info and its stderr at warn, under the execution:
  `get_node_logs` with `exec_id` gives them, including, for a container task
  with `rm`, the name of the container `docker rm` prints. False, only the
  exit code is kept.
- A task longer than `wait_seconds` returns `completed: false` with its
  `exec_id`. Not offered: `cron`, `force`, `to`, `subset`, `tag`, `slaves`,
  `master` and `confirm`.

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
