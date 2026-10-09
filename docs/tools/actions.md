---
domain: actions
tools:
  - abort_object_orchestration
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
| `unfreeze_object` | `POST /api/object/path/<ns>/<kind>/<name>/action/unfreeze` | svc, vol | No | Yes |
| `abort_object_orchestration` | `POST /api/object/path/<ns>/<kind>/<name>/action/abort` | svc, vol, cfg, sec, usr, nscfg | No | Yes |

- `freeze_object` stops the automatic actions of the daemon on the object on
  all its nodes, such as an ha failover or the restart of a degraded monitored
  resource, until it is unfrozen. Running instances keep running.
- `unfreeze_object` resumes those automatic actions, which may then start, fail
  over or restart instances to reach the object placement.
- `abort_object_orchestration` drops the pending target state of the object.
  An action already running on an instance is not interrupted: the daemon
  queues the abort and applies it when that action ends.

### Input and output

The input is `path`, one exact canonical object path. The output holds
`provenance`, `object` (the canonical reference), `action` and
`orchestration_id`. An object of another kind is refused before the daemon
call; the daemon answers 404 for an object no node holds and 409 for an
expectation it refuses.
