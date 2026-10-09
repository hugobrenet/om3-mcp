---
domain: network
tools:
  - list_network_ips
  - list_networks
stability: experimental
---

# Network Tools

This document describes tools that report the OpenSVC cluster backend
networks and the addresses the instances report in them. They answer whether
a network is running out of addresses and whether an address is reported by
several objects, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/network.go` and `internal/core/network_ip.go`;
- MCP definitions: `internal/tools/network.go`.

Both endpoints are computed on each request by the daemon receiving it. The
networks come from its configuration: the cluster configuration merged with
its own node configuration. The addresses come from the status of every
instance of the cluster: each resource reporting an `ipaddr` within a network
range. A failover address is reported by its instance on every node, whether
the instance is up or down.

## Tools

### `list_networks`

Returns the backend networks with their address usage. Use it when an address
cannot be allocated or a network may be exhausted.

#### OpenSVC API and authorization

```text
GET /api/network[?name=<network>]
```

The daemon requires the global `root` grant. An unknown name returns an empty
list.

#### Input

| Input | Meaning |
|---|---|
| `name` | Optional exact network name; omitted for every network |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `name_filter` | The filter sent to the daemon |
| `total`, `count`, `truncated` | Networks returned by the daemon, networks included, and whether some were omitted after 512 |
| `networks[]` | Networks sorted by name |

Each network holds `name`, `type` (such as `bridge`, `routed_bridge` or `lo`),
`network` (the range in CIDR notation), `errors` (at most 10, each limited to
512 characters) and three counts:

| Count | Meaning |
|---|---|
| `used` | The node, object and resource entries reporting an address in the range. A failover address counts once per node holding an instance |
| `size_addresses` | The addresses in the range, network and broadcast addresses included, as a decimal string |
| `free_addresses` | `size_addresses` minus `used`, as a decimal string |

The two address counts are strings because an IPv6 range exceeds 64-bit
integers: a `/64` holds 18446744073709551616 addresses. They are empty when the
daemon cannot compute them from the range. Since `used` counts per node, a
network nearly exhausted by `free_addresses` may hold fewer distinct addresses:
`list_network_ips` lists them.

### `list_network_ips`

Lists the addresses reported in the backend networks. Use it to find what
holds the addresses of a network, or an address reported by several objects.

#### OpenSVC API and authorization

```text
GET /api/network/ip[?name=<network>]
```

The daemon requires the global `root` grant. An address in several overlapping
networks is listed once per network.

#### Input

| Input | Meaning |
|---|---|
| `network` | Optional exact network name, sent to the daemon |
| `node` | Optional exact node name, following the [node target](README.md#node-targets) rule |
| `path` | Optional exact object path, without wildcard or selector; `web` and `root/svc/web` name the same object |
| `shared_only` | Optional; `true` keeps only the addresses reported by more than one object resource |
| `limit` | Optional page size between 1 and 200; defaults to 100 |
| `cursor` | Optional `next_cursor` of a previous call with the same filters |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `network_filter`, `node_filter`, `path_filter`, `shared_only` | The filters applied |
| `reported_total` | Address entries returned by the daemon, before the MCP filters |
| `total`, `count` | Matching entries, and entries in this page |
| `ips[]` | Entries sorted by network, address (numerically), object path, resource and node |
| `next_cursor`, `truncated` | Continuation of the page |

Each entry holds `ip`, `node`, `path`, `rid`, `network` (`name`, `type`,
`network`) and `other_resources`: the number of other object and resource
pairs reporting the same address in the daemon response. A failover address
reported by its instances on several nodes has `other_resources` 0. A value
above 0 means distinct resources report the same address: two objects
configured with one address, or one object with two resources. The MCP reports
the fact; whether it is a conflict depends on which instances are up, which
`list_cluster_ip_resources` shows. `other_resources` counts the whole daemon
response, so it is the same with or without the `node` and `path` filters, but
it depends on the `network` filter the daemon applies.
