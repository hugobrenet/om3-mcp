# OpenSVC Daemon MCP Tools

This directory documents the human-facing contracts of the OpenSVC daemon MCP
tools. Runtime names, descriptions, annotations, and JSON Schemas remain
available through MCP `tools/list`; these documents explain how to select and
combine the tools during operations.

## Domains

| Domain | Tools | Documentation |
|---|---|---|
| Daemon | `get_daemon_identity` | [Daemon tools](daemon.md) |
| Cluster | `get_cluster_health`, `get_node_status`, `get_node_logs` | [Cluster tools](cluster.md) |
| Objects | `list_cluster_objects`, `get_object_status`, `get_object_config` | [Object tools](objects.md) |
| Instances | `list_object_instances`, `get_instance_logs`, `refresh_instance_status` | [Instance tools](instances.md) |
| Resources | `list_object_resources`, `get_container_logs` | [Resource tools](resources.md) |

## Diagnostic workflow

Use the smallest tool that answers the current question:

```text
get_daemon_identity
  -> get_cluster_health
  -> get_node_status when one node needs closer inspection
  -> get_node_logs when recent daemon activity on that node matters
  -> list_cluster_objects
  -> get_object_status
  -> get_object_config when declared settings matter
  -> list_object_instances
  -> get_instance_logs when recent OpenSVC activity matters
  -> refresh_instance_status when freshness is insufficient
  -> list_object_resources
  -> get_container_logs when workload stdout or stderr matters
```

`get_daemon_identity` confirms the target node and cluster.
`get_cluster_health` provides a bounded first assessment, including heartbeat
stream and peer-link health from the daemon's last-known cluster view.
`get_node_status` shows the reported state, monitor, capacity, policy, and
heartbeat assessment for one exact node.
`get_node_logs` shows recent OpenSVC journal entries on one exact node, with an
optional component filter.
The object, instance, and resource tools then narrow a diagnosis from cluster
inventory to the exact failing resource.

## Authentication and visibility

Every MCP HTTP request requires an OpenSVC access JWT. The MCP validates the JWT
and delegates the same request-scoped token to the daemon API. OpenSVC remains
the source of truth for grants and namespace visibility.

Missing, invalid, expired, or non-access JWTs are rejected by the MCP transport
with HTTP `401`. A valid JWT that cannot execute a daemon operation produces an
MCP tool result with `isError=true`; the error preserves the HTTP status and
bounded RFC 7807 `title` and `detail` fields returned by OpenSVC.

## Freshness model

Every successful tool result includes a `provenance` object:

```json
{
  "source": "opensvc_daemon",
  "observed_at": "2026-09-16T10:00:00Z"
}
```

`source` identifies the daemon API that supplied the MCP result, not
necessarily the original data store (for example, instance logs originate in
the node journal). `observed_at` is the UTC time when the MCP finished
collecting and normalizing the result. It is not a status update timestamp and
does not guarantee that the underlying data is fresh. Tool errors have no
successful result provenance.

Read-only status tools return the last-known state held by the daemon. They do
not execute resource drivers. An out-of-band runtime failure or recovery may be
absent until OpenSVC refreshes the instance status.

Always inspect daemon-provided `updated_at`, where available, before relying on
status for a diagnosis. Unlike `observed_at`, it dates the OpenSVC status
itself. Use `refresh_instance_status` only for one exact object instance when
a fresher probe is required. It is non-destructive, but it executes status
drivers and updates daemon state.

## Lab examples

The JSON examples were captured through the real Streamable HTTP MCP server
against an OpenSVC `3.0.0-rc21` single-node lab running a Redis Docker resource.
Values are normalized for publication while preserving the actual response
shape. The `provenance` fields in these historical examples illustrate the
current contract; their timestamps are representative, not original capture
metadata:

| Value | Example |
|---|---|
| Cluster | `lab-cluster` |
| Node | `lab-node-01` |
| Object | `lab/svc/redis` |
| Resource | `container#redis` |

Examples show the `arguments` object passed to `tools/call`, not the complete
JSON-RPC envelope. Timestamps, process identifiers, UUIDs, and routine counts
are representative and will differ between calls.

## Safety summary

| Tool | Read-only | Destructive | Required OpenSVC access |
|---|---:|---:|---|
| `get_daemon_identity` | Yes | No | `guest` or higher |
| `get_cluster_health` | Yes | No | `guest` or higher |
| `get_node_status` | Yes | No | `guest` or higher |
| `get_node_logs` | Yes | No | `root` |
| `list_cluster_objects` | Yes | No | Visible namespaces |
| `get_object_status` | Yes | No | Visibility on the object namespace |
| `get_object_config` | Yes | No | Visibility on the object namespace |
| `list_object_instances` | Yes | No | Visibility on the object namespace |
| `get_instance_logs` | Yes | No | `root` in OpenSVC 3.0.0-rc21 |
| `refresh_instance_status` | No | No | `operator`, `admin`, or `root` |
| `list_object_resources` | Yes | No | Visibility on the object namespace |
| `get_container_logs` | Yes | No | `root` in OpenSVC 3.0.0-rc21 |

Annotations are client hints. The daemon's authorization decision is always
authoritative.
