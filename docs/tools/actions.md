---
domain: actions
tools:
  - abort_object_orchestration
  - start_object
  - stop_object
  - restart_object
  - switch_object
  - giveback_object
  - freeze_object
  - unfreeze_object
stability: experimental
---

# Action Tools

This document describes the tools that change the state of an OpenSVC cluster.
They are registered only when `OPENSVC_MCP_ACTIONS=enabled`, on the HTTPS
listener and on the Unix socket alike; otherwise the MCP exposes none of them.

Implementation:

- business logic: `internal/core/object_action.go`;
- MCP definitions: `internal/tools/object_action.go`.

## Contract

Every action tool:

- targets one exact object, never a selector or several objects;
- is annotated `readOnlyHint: false`: a client identifies the action tools by
  this standard MCP annotation, to ask the user before running one;
  `destructiveHint` tells whether the action can stop, move or lose a service;
- returns as soon as the daemon queued the orchestration, with its
  `orchestration_id`, without waiting for the target state;
- leaves authorization to the daemon: the caller needs `operator` on the
  object namespace, and a refusal is returned as the daemon reports it;
- is recorded by the MCP audit line of every tool call, with the caller
  identity.

To follow an action, pass its `orchestration_id` to
`list_daemon_orchestrations` and `get_node_logs`, and read the outcome with
`get_object_status`.

## Tools

| Tool | OpenSVC API | Kinds | Destructive | Idempotent |
|---|---|---|---:|---:|
| `freeze_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/freeze` | svc, vol | No | Yes |
| `unfreeze_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/unfreeze` | svc, vol | Yes | Yes |
| `abort_object_orchestration` | `POST /api/object/path/<ns>/<kind>/<name>/action/abort` | svc, vol, cfg, sec, usr, nscfg | No | Yes |
| `start_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/start` | svc | No | Yes |
| `stop_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/stop` | svc | Yes | Yes |
| `restart_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/restart` | svc | Yes | No |
| `switch_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/switch` | svc | Yes | No |
| `giveback_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/giveback` | svc | Yes | Yes |

- `freeze_object` stops the automatic actions of the daemon on the object on
  all its nodes, such as an ha failover or the restart of a degraded monitored
  resource, until it is unfrozen. Running instances keep running.
- `unfreeze_object` resumes those automatic actions, which may then start, fail
  over or restart instances to reach the object placement: it is destructive, as
  they may interrupt the service.
- `abort_object_orchestration` drops the pending target state of the object.
  An action already running on an instance is not interrupted: the daemon
  queues the abort and applies it when that action ends.
- `start_object` starts the instances the placement and topology of the
  service call for. The daemon answers 409 for a service already started, and
  for a start it cannot run, such as a failover service in warn state or an
  unprovisioned service, with the reason. A frozen service is started too and
  stays frozen.
- `stop_object` stops every running instance of the service, which makes it
  unavailable. The stopped instances are flagged so the daemon does not
  restart them on its own. By default the stop waits for the data syncs
  running on the instances to end; `interrupt_syncs: true` ends them instead,
  through the `interrupt_syncs=true` query parameter. A service already
  stopped is accepted and left unchanged. A frozen service is stopped too and
  stays frozen.
- `restart_object` stops then starts each running instance of the service, one
  node after the other, so the service is interrupted on each node in turn.
  Instances down or in standby are left as they are, and a service already
  stopped stays stopped; the restart still clears the stopped flag of its
  instances, so the daemon may then start an ha service on its own. The tool
  sends no request body: the daemon `force` option, which shuts the instances
  down and starts only their standby resources, is not exposed.
- `switch_object` moves the service to another node: the daemon stops the
  instance where it runs and starts it on the destination. `to` names the one
  destination node; omitted, the daemon selects the first provisioned node
  where the service is not up, in placement order. The MCP sends the request
  body `{"destination": ["<to>"], "live": <live>, "interrupt_syncs": <bool>}`,
  without `destination` when `to` is omitted. It accepts one exact node name
  only, where the daemon reads the destination as a node selector. `live`
  migrates the service live when its resources allow it, and otherwise
  switches it by a stop and a start. A switch to the node already running the
  service changes nothing; the daemon answers 409 when no provisioned node can
  receive it.
- `giveback_object` moves the running instances of the service to the nodes
  its placement policy ranks first. A service already on its preferred nodes
  is left as it is.

### Input and output

The input is `path`, one exact canonical object path, plus the optional
`interrupt_syncs` of `stop_object`, and the optional `to`, `live` and
`interrupt_syncs` of `switch_object`. The output holds
`provenance`, `object` (the canonical reference), `action` and
`orchestration_id`. An object of another kind is refused before the daemon
call; the daemon answers 404 for an object no node holds and 409 for an
expectation it refuses.
