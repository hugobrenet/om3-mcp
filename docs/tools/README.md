# OpenSVC Daemon MCP Tools

This directory documents the human-facing contracts of the OpenSVC daemon MCP
tools. Runtime names, descriptions, annotations, and JSON Schemas remain
available through MCP `tools/list`; these documents explain how to select and
combine the tools during operations.

## Domains

| Domain | Tools | Documentation |
|---|---|---|
| Daemon | `get_daemon_status`, `list_daemon_executions`, `list_daemon_orchestrations` | [Daemon tools](daemon.md) |
| Cluster | `get_cluster_config`, `get_cluster_status` | [Cluster tools](cluster.md) |
| Node | `get_node_config`, `get_node_status`, `get_node_logs`, `get_node_daemon_metrics`, `probe_node_reachability`, `list_node_capabilities`, `list_node_drivers` | [Node tools](node.md) |
| Objects | `list_cluster_objects`, `get_object_status`, `get_object_config` | [Object tools](objects.md) |
| Instances | `list_object_instances`, `get_instance_logs`, `refresh_instance_status` | [Instance tools](instances.md) |
| Resources | `list_cluster_ip_resources`, `list_object_resources`, `get_container_logs` | [Resource tools](resources.md) |

## Diagnostic workflow

Use the smallest tool that answers the current question:

```text
get_daemon_status
  -> list_daemon_orchestrations when a requested target state matters
  -> list_daemon_executions when recent or running commands matter
  -> get_cluster_status
  -> get_cluster_config when declared cluster settings matter
  -> get_node_status when one node needs closer inspection
  -> list_node_capabilities when detected runtime or driver support matters
  -> list_node_drivers when the drivers registered by the running daemon matter
  -> get_node_daemon_metrics when a hypothesis concerns daemon activity, latency, errors, or process behavior
  -> probe_node_reachability when the proxy path to one exact daemon must be verified
  -> get_node_config when declared node settings matter
  -> get_node_logs when recent daemon activity on that node matters
  -> list_cluster_ip_resources when cluster service-address ownership matters
  -> list_cluster_objects
  -> get_object_status
  -> get_object_config when declared settings matter
  -> list_object_instances
  -> get_instance_logs when recent OpenSVC activity matters
  -> refresh_instance_status when freshness is insufficient
  -> list_object_resources
  -> get_container_logs when workload stdout or stderr matters
```

`get_daemon_status` confirms the target node and cluster, then exposes the
local daemon process and exact subsystem states without adding a health
verdict.
`list_daemon_executions` exposes bounded running and recent command records on
one exact node. It preserves the daemon's state, exit code, and error facts and
requires `root` because command arguments can be sensitive.
`list_daemon_orchestrations` exposes the requested target state and exact
outcome of bounded running and recent orchestrations. Its `orchestration_id`
can be passed to `list_daemon_executions` to inspect the commands run beneath
that intent.
`get_cluster_status` provides bounded cluster, node, heartbeat, and actor-object
facts from the daemon's last-known cluster view without an MCP health verdict.
`get_cluster_config` provides the current redacted cluster configuration when
declared settings matter.
`get_node_status` shows the reported state, membership context, monitor,
capacity, policy, and bounded heartbeat facts for one exact node.
`list_node_capabilities` returns the exact capability markers stored by the
last OpenSVC capability scan, defaulting to the local node through `_`, without
treating presence as current health or configuration.
`list_node_drivers` returns the exact driver names registered by the running
daemon, defaulting to the local node through `_`, without treating registration
as runtime availability, configuration, use, or health.
`get_node_daemon_metrics` exposes bounded, filterable Prometheus facts from one
daemon for advanced activity or performance diagnosis. It does not calculate
rates, interpret health, or expose general host and workload metrics.
`probe_node_reachability` actively verifies the complete OpenSVC proxy path to
one exact daemon. A `204` proves that daemon answered the request, not that its
cluster, heartbeat, subsystems, objects, or resources are healthy.
`get_node_config` provides the current redacted configuration for one exact
node.
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

## Examples

The JSON examples use representative values and illustrate the documented
response shape. Timestamps and provenance values are illustrative:

| Value | Example |
|---|---|
| Cluster | `cluster-a` |
| Node | `node-a` |
| Object | `prod/svc/redis` |
| Resource | `container#redis` |

Examples show the `arguments` object passed to `tools/call`, not the complete
JSON-RPC envelope. Timestamps, process identifiers, UUIDs, and routine counts
are representative and will differ between calls.

## Safety summary

| Tool | Read-only | Destructive | Required OpenSVC access |
|---|---:|---:|---|
| `get_daemon_status` | Yes | No | `guest` or higher |
| `list_daemon_executions` | Yes | No | `root` |
| `list_daemon_orchestrations` | Yes | No | `root` |
| `get_cluster_config` | Yes | No | `root` |
| `get_cluster_status` | Yes | No | `guest` or higher |
| `get_node_config` | Yes | No | `root` |
| `get_node_status` | Yes | No | `guest` or higher |
| `list_node_capabilities` | Yes | No | `root` |
| `list_node_drivers` | Yes | No | `root` |
| `get_node_daemon_metrics` | Yes | No | Delegated JWT; daemon endpoint policy |
| `probe_node_reachability` | Yes | No | `root` |
| `get_node_logs` | Yes | No | `root` |
| `list_cluster_objects` | Yes | No | Visible namespaces |
| `get_object_status` | Yes | No | Visibility on the object namespace |
| `get_object_config` | Yes | No | Visibility on the object namespace |
| `list_object_instances` | Yes | No | Visibility on the object namespace |
| `get_instance_logs` | Yes | No | `root` |
| `refresh_instance_status` | No | No | `operator`, `admin`, or `root` |
| `list_cluster_ip_resources` | Yes | No | Visible namespaces |
| `list_object_resources` | Yes | No | Visibility on the object namespace |
| `get_container_logs` | Yes | No | `root` |

Annotations are client hints. The daemon's authorization decision is always
authoritative.
