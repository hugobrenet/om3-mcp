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
  - provision_object
  - resize_object
  - cap_object
  - enable_object
  - disable_object
  - delete_object
  - unprovision_object
  - purge_object
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
  `orchestration_id`, without waiting for the target state, but for
  `enable_object` and `disable_object`, which write the configuration at once;
- leaves authorization to the daemon: the caller needs `operator` on the
  object namespace, `admin` for the lifecycle actions below, and a refusal is
  returned as the daemon reports it;
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
| `provision_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/provision[?adopt=true]` | svc, vol | No | Yes |
| `resize_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/resize`, `{"size"}` | vol | No | Yes |
| `cap_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/cap?set=…` | svc, vol | Yes | Yes |
| `enable_object` | `POST /api/object/path/<ns>/svc/<name>/enable[?rid=…]` | svc | No | Yes |
| `disable_object` | `POST /api/object/path/<ns>/svc/<name>/disable[?rid=…]` | svc | Yes | Yes |
| `delete_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/delete` | svc, vol, cfg, sec, usr | Yes | No |
| `unprovision_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/unprovision` | svc, vol | Yes | Yes |
| `purge_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/purge` | svc, vol | Yes | No |

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

### Lifecycle actions

They require `admin` on the object namespace.

- `provision_object` creates what the resources of the object hold, a
  volume, a filesystem, a disk, then starts the leader instance, and lifts the
  stopped flag `create_object` leaves; on an object already provisioned, it
  starts it. Without `adopt`, each node first checks that nothing holds what
  the object would take, a filesystem mounted on its mount point, a volume
  group of its name, a device in use: a node refusing ends the orchestration
  as a provision failure, and nothing is provisioned. `adopt: true` takes over
  what it finds, as the replicated storage a restored object finds.
- `resize_object` grows a volume: the daemon writes `size` to its
  configuration, within the claims of its pool, then queues the orchestration.
  `size` is absolute, `11g`, or an amount to add, `+1g`, checked on its form
  by the MCP; a size under the one configured is refused with 400; omitted,
  the resize converges to the size configured, which finishes a resize
  stopped part way. The orchestration fails when no instance is up. The
  daemon declares the route for a svc too, and refuses it with 400, a svc
  having no size to grow: the MCP accepts a vol only.
- `cap_object` writes the `pg_*` keywords its operations name, as a
  configuration update would, and applies them to the running instances; an
  instance not running applies them at its start. An operation is
  `[<section>.]pg_<name>[@<node>]=<value>`, the operator `=` only, checked on
  its form by the MCP; `default` lifts a cap; without operations, the caps
  configured are applied. The orchestration is queued before the write, so
  that a cap refused, 409 while an orchestration runs, is not written. It
  returns `last_modified`, the `OM-Last-Modified` of the configuration
  written. Destructive: a memory limit under what an instance up uses ends
  its processes.
- `resize_object` and `cap_object` take `config_updated_at`, an RFC 3339
  time, the `last_modified` `update_object_config` gives: the orchestration
  waits for that configuration on every node. It serves the form that
  applies the configuration, without `size` or operations, after an update
  wrote `size` or `pg_*` keywords; the daemon replaces it with the time of
  its own write otherwise. The other object routes take none.
- `enable_object` and `disable_object` write the `disable` keyword of
  DEFAULT, or of each resource given in `rids`, exact ids such as `fs#1` sent
  as the daemon `rid` selector joined by commas, at once, without an
  orchestration: the daemon answers 204. The daemon answers 204 as well for
  an id the service has not, writing nothing: the MCP first reads the
  `<group>.type` keywords of the groups named, and refuses an id that is not
  a section of the configuration, naming the ones it has. The MCP reads the
  `disable` keywords of the sections touched again. `disable_object` is refused with 403 for a
  resource up on a node, and destructive: a disabled object is no longer
  orchestrated, a disabled resource no longer started. `enable_object` is
  refused with 409 when a resource enabled again holds a value a resource of
  another object holds on the same node.
- `delete_object` removes the configuration of the object and the files of
  every instance. The daemon does not look at what the object runs or holds:
  the MCP reads its instances first, and refuses when one is not `down` or
  `n/a`, or, for a svc or a vol, is provisioned, `true` or `mixed`, which
  would leave the resources mounted, or the storage, held by nothing: a
  provisioned object is purged, `purge_object`, once stopped. A namespace
  configuration, `nscfg`, whose deletion touches the namespace, is not
  offered.
- `unprovision_object` sets the global expect `unprovisioned`. Each instance
  first waits for its children, the objects depending on it, to be down,
  without a time limit, a wait `list_daemon_orchestrations` shows; the
  non-leader instances then unprovision, and the leader of the placement,
  once no other instance is provisioned, unprovisions with the resources
  they share. Each resource is stopped, then unprovisioned, pre and post
  triggers included: the data of the disk and fs resources is destroyed,
  "unprovision usually deletes the data", the API says. The vol objects a
  svc uses are released, not unprovisioned: they keep their data. An
  instance reaches the goal at `provisioned: false` or `n/a`, or `avail:
  n/a`; an object unprovisioned already reaches it at once. A resource with
  `unprovision=false` fails the instance, pointing to a manual unprovision
  and `--state-only`, not offered; a failure leaves `unprovision failed`,
  which `clear_instance` lifts.
- `purge_object` runs the same unprovision, then deletes the instance on
  each node; the orchestration ends when the monitors of the instances are
  gone, `get_object_status` then answering not found. A failure leaves
  `purge failed` and the object, partly unprovisioned. The daemon purges a
  cfg, a sec or a usr by deleting it: the MCP refuses them, pointing to
  `delete_object`. The description points, before the purge of a vol, to the
  objects using it: `children` in `list_pool_volumes`, or in
  `get_instance_status` on its node; empty, none does.
- The daemon looks at nothing before an unprovision or a purge: it stops an
  instance up, then destroys its data, where an operator expects a service
  running to be refused. The MCP reads the instances first and refuses when
  one is `up` or `warn`: `stop_object` first. It does not look at the
  children: their wait is visible, and the purge of a vol a svc holds is one
  to see wait. The daemon refuses with 409 an object whose orchestration
  runs, and with 408 a monitor busy with an action. The daemon API tags both
  routes for svc and vol, and its handler takes any kind.

### Input and output

The input is `path`, one exact canonical object path, plus the optional
`interrupt_syncs` of `stop_object`, and the optional `to`, `live` and
`interrupt_syncs` of `switch_object`. The output holds
`provenance`, `object` (the canonical reference), `action` and
`orchestration_id`. An object of another kind is refused before the daemon
call; the daemon answers 404 for an object no node holds and 409 for an
expectation it refuses.
