# OpenSVC Daemon MCP Tools

On the HTTPS listener, with clusters configured with `auth` and exchange
profiles, every daemon tool has a required `cluster_id` input added by the
registrar. The domain-specific inputs below are unchanged.
`list_clusters(query?, limit?, cursor?)` discovers those names and IDs without
contacting daemons or checking grants.
See [token exchange](../token-exchange.md#tool-contracts) for the shared target
contract. Without exchange configuration, daemon tools remain listed but blocked.

This directory documents the human-facing contracts of the OpenSVC daemon MCP
tools. Names, descriptions, annotations and JSON Schemas are defined in
`internal/tools` and exposed by bearer-checked `tools/list` calls. These
documents describe the implemented tool contracts.

## Domains

| Domain | Tools | Documentation |
|---|---|---|
| Catalogue | `list_clusters` | [Multi-cluster tools](../token-exchange.md#tool-contracts) |
| Daemon | `get_daemon_status`, `list_daemon_executions`, `list_daemon_orchestrations` | [Daemon tools](daemon.md) |
| Cluster | `get_cluster_config`, `get_cluster_status` | [Cluster tools](cluster.md) |
| Node | `get_node_config`, `get_node_status`, `get_node_logs`, `list_node_properties`, `list_node_hardware`, `list_node_packages`, `get_node_daemon_metrics`, `probe_node_reachability`, `list_node_capabilities`, `list_node_drivers` | [Node tools](node.md) |
| Objects | `list_cluster_objects`, `get_object_status`, `get_object_config`, `list_object_config_keywords` | [Object tools](objects.md) |
| Instances | `list_object_instances`, `get_instance_status`, `get_instance_logs`, `refresh_instance_status` | [Instance tools](instances.md) |
| Resources | `list_cluster_ip_resources`, `list_object_resources`, `list_resource_info`, `get_container_logs` | [Resource tools](resources.md) |
| Scheduler | `list_schedules` | [Scheduler tools](schedules.md) |

## Authentication and visibility

Remote agents supply an OAuth access token intended for the MCP resource on
every `/mcp` request. The MCP verifies its issuer, audience, signature and
lifetime. No MCP business scope is required. Each daemon tool then requires
an explicit catalogue `cluster_id` and a successful confidential token exchange.
Only the exchanged token is sent to the configured VIP. Grants and namespace
visibility remain authoritative at the daemon.

Missing or invalid incoming credentials return HTTP 401 with the OAuth
discovery challenge. Tool target errors, SSO exchange refusals and daemon
refusals return `isError=true`. SSO descriptions and response bodies are not
exposed. Daemon errors use the existing bounded API error contract. No request
or session can replace another call's identity or target.

On the delegated Unix socket, tools take no `cluster_id` argument and
`list_clusters` is not offered: the `X-OpenSVC-Cluster-ID` header binds every
call to one cluster. See [token delegation](../delegation.md).

## Freshness model

Every successful tool result includes a `provenance` object:

```json
{
  "source": "opensvc_daemon",
  "observed_at": "2026-09-16T10:00:00Z"
}
```

For `list_clusters`, source is `opensvc_mcp_catalog`. For daemon tools,
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

Domain examples show the business arguments; add the required `cluster_id`
for external OAuth calls. They show the `arguments` object passed to `tools/call`, not the complete
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
| `list_node_properties` | Yes | No | `root` |
| `list_node_hardware` | Yes | No | `root` |
| `list_node_packages` | Yes | No | `root` |
| `list_node_capabilities` | Yes | No | `root` |
| `list_node_drivers` | Yes | No | `root` |
| `get_node_daemon_metrics` | Yes | No | OpenSVC JWT; daemon endpoint policy |
| `probe_node_reachability` | Yes | No | `root` |
| `get_node_logs` | Yes | No | `root` |
| `list_cluster_objects` | Yes | No | Visible namespaces |
| `get_object_status` | Yes | No | Visibility on the object namespace |
| `get_object_config` | Yes | No | Visibility on the object namespace |
| `list_object_config_keywords` | Yes | No | `guest` or higher on the object namespace |
| `list_object_instances` | Yes | No | Visibility on the object namespace |
| `get_instance_status` | Yes | No | Namespace `guest` or higher |
| `get_instance_logs` | Yes | No | `root` |
| `refresh_instance_status` | No | No | `operator`, `admin`, or `root` |
| `list_cluster_ip_resources` | Yes | No | Visible namespaces |
| `list_object_resources` | Yes | No | Visibility on the object namespace |
| `list_resource_info` | Yes | No | Namespace `guest` or higher |
| `get_container_logs` | Yes | No | `root` |
| `list_schedules` | Yes | No | `root` for node scope; namespace `guest` or higher for object and instance scopes |

Annotations are client hints. The daemon's authorization decision is always
authoritative.
