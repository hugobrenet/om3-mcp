---
domain: daemon-actions
tools:
  - set_daemon_log_level
  - start_heartbeat
  - restart_heartbeat
  - restart_daemon_listener
  - restart_daemon
stability: experimental
---

# Daemon Action Tools

This document describes the tools that act on the daemon of one node: the
level of its log, its heartbeat streams, its HTTPS listener and itself. They
are action tools: registered only when `OPENSVC_MCP_ACTIONS=enabled`, under
the contract of the [action tools](actions.md), and require `root`. The daemon
relays each request to the node.

Implementation:

- business logic: `internal/core/daemon_action.go`;
- MCP definitions: `internal/tools/daemon_action.go`.

## Tools

| Tool | OpenSVC API | Destructive | Idempotent |
|---|---|---:|---:|
| `set_daemon_log_level` | `POST /api/node/name/<node>/daemon/log/control {"level"}` | No | Yes |
| `start_heartbeat` | `POST /api/node/name/<node>/daemon/hb/name/<name>/action/start` | No | Yes |
| `restart_heartbeat` | `POST /api/node/name/<node>/daemon/hb/name/<name>/action/restart` | Yes | Yes |
| `restart_daemon_listener` | `POST /api/node/name/<node>/daemon/listener/name/api.inet/action/restart` | Yes | Yes |
| `restart_daemon` | `POST /api/node/name/<node>/daemon/action/restart` | Yes | No |

## The log level

`set_daemon_log_level` takes `info`, `warn`, `error` or `none`, and returns
the level set and the one read before. The daemon refuses `debug` and `trace`
with 400: its log is given nothing below `info`, and their feed is the
daemon audit, a stream; the MCP refuses them before the call, with the same
pointer. The tool quiets a daemon during a noisy incident; the level holds
until the daemon restarts, which takes the level of its configuration.

## The heartbeats

`name` is a heartbeat, such as `hb#1`, with or without the prefix, which
addresses both its streams `hb#1.rx` and `hb#1.tx`, or one stream, such as
`hb#1.rx`; the `#` is sent encoded in the path. A name not configured is the
400 of the daemon, which lists those it has. The daemon publishes the action
to its janitor and answers 200 with a problem body, "daemon action queued",
whose detail names the streams: asynchronous, without an id. The janitor
unregisters a stream stopped, which is no longer published; ignores a start
of a stream already running; and runs a restart as a stop then a start. A
stream is `running`, `stopped` or `failed`.

The tools read the streams again with the node status, every second at
most, `wait_seconds`, 10 by default, 60 at most: a start ends when the streams
run, a restart when they run with a `created_at` other than the one read
before the request: a stream restarted is registered again, where its
`updated_at` moves too when a peer changes; `updated_at` stands in for a
daemon that gives no `created_at`. Comparing to what was read avoids comparing
the clocks of the MCP and of the node. A last read that fails is told in
`status_error`. They return `completed`, each stream addressed with its
`state`, `updated_at` and whether each peer beats, and `absent`, the streams
addressed not published.

The node status is served by the daemon the MCP talks to: the streams of
another node are its view, received through the heartbeats themselves, and
no route gives the heartbeats as the node reads them, as
`GET /api/node/name/<node>/daemon/log/control` gives its log level. That view
arrives late: 10 seconds after a stream was stopped on dev5n2, dev5n1 still
saw it running and beating. A `completed` of `restart_heartbeat` read on a peer
may tell the streams of before the restart, and a `completed` false may come
from the lag alone; the tools say so in their description.
`restart_heartbeat` cuts the stream for the cycle, and a stream that fails to
start stays cut.

### Why stopping a heartbeat is not offered

The daemon checks nothing before a stop. Stopping the last `.tx` stream of a
node cuts all it publishes to its peers: a peer that no longer hears a node
declares it dead and takes its services over. The MCP could not guard it: the
view a guard would read is the one the heartbeats carry, late, the view the
stop cuts. On 2026-10-23, a guard reading that view let the two `.tx` streams
of dev5n2 be stopped, one after the other, the first still seen beating: at
11:06 dev5n1 saw dev5n2 stale and a cluster split, ignored only because the
lab runs without quorum. With an arbitrator, the side without it could have
stopped its services. The streams were started again with the local API of
dev5n2. A stop stays an operator's command on the node, `om daemon hb stop`.

## The listener

Only `api.inet`, the HTTPS listener, has a life cycle; `api.ux` lives as long
as the daemon, the request going through it. `restart_daemon_listener` cuts
the API of the node while it rebinds, the daemon answering "queued" before; a
rebind that fails leaves the node without its API for the MCP and its peers,
a relay from a peer going through the same listener. The tool reads the
listener again until it runs with a `created_at` or an `updated_at` other than
the ones read before: the listener builds its status once, with the
component, and a restart moves its `updated_at`, not always its
`created_at`.
The listener may close the connection while it restarts, before its answer
leaves: the request then fails, relayed by a peer as an EOF in a 500, though
the restart is done, as on dev5n2 on 2026-10-23. The tool reads the listener
again all the same: seen restarted, it returns the restart done with the error
in `answer_error`; not seen, the error. It tells a read
failing while the API restarts in `status_error`. Stopping or starting the
listener is not offered: a stopped listener could not be started again
through it.

## The daemon

`restart_daemon` runs `om daemon restart` in the background, the daemon
answering "background daemon restart has been called": the instances keep
their state, and the peers, told the node enters maintenance, take nothing
over. The tool returns the pid and `started_at` read before, then reads the
node again until a daemon with another `started_at` answers: `restarted`. The
reads failing while the daemon restarts do not end the wait; a daemon not
seen again is told in `status_error`, the restart being launched all the
same. Restarting the daemon of the node the MCP talks to cuts the MCP from the
cluster for the restart.

Not offered: stopping or shutting down the daemon, joining or leaving the
cluster, signing or wiping a heartbeat.
