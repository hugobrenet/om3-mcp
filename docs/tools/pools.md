---
domain: pool
tools:
  - list_pool_volumes
  - list_storage_pools
stability: experimental
---

# Pool Tools

This document describes tools that report the capacity and usage of the OpenSVC
storage pools, and the volumes they serve. They answer whether a pool is full
and what takes its space, without a diagnostic verdict.

Implementation:

- business logic: `internal/core/pool.go` and `internal/core/pool_volume.go`;
- MCP definitions: `internal/tools/pool.go`.

## Tools

### `list_storage_pools`

Returns the capacity and usage of the storage pools. Use it when a volume
cannot be provisioned or a pool may be full.

#### OpenSVC API, authorization, and freshness

```text
GET /api/pool[?name=<pool>][&node=<node>|*]
```

The daemon requires the global `root` grant. Usage comes from the daemon pool
status, refreshed periodically: `updated_at` is its daemon timestamp.

#### Input

| Input | Meaning |
|---|---|
| `pool` | Optional exact pool name; omitted for every pool |
| `node` | Optional exact node name: the records of the pools on that node |
| `per_node` | Optional; `true` returns one record per node and pool, for every node. Exclusive with `node` |

Without `node` and `per_node`, the daemon returns the **cluster view**: one
record per pool, with an empty `node`. Node selector expressions (`n[12]`,
labels) are not accepted: `node` is one exact name, and `per_node` stands for
every node.

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `pool_filter`, `node_filter` | The filters sent to the daemon |
| `per_node` | Whether the records are per node rather than per pool |
| `total`, `count`, `truncated` | Records returned by the daemon, records included, and whether some were omitted after 512 |
| `pools[]` | Records sorted by node, then pool name |

Each pool record holds `name`, `node` (empty in the cluster view), `type`,
`head` (the storage location, such as a directory or a device),
`capabilities` (at most 32), `shared`, `volume_count`, `errors` (at most 10,
each limited to 512 characters), `updated_at`, and two capacities:

| Capacity | Meaning |
|---|---|
| `physical` | `size_bytes`, `used_bytes`, `free_bytes` of the storage behind the pool. The cluster view of a non-shared pool sums the nodes |
| `logical` | What the pool hands out to volumes, counting a volume once however many nodes hold a copy. `free_bytes` is what volumes can still claim: the measure of a full pool. The cluster view of a non-shared pool keeps the three figures of the node with the least room left, not their sum |

The daemon builds the cluster view so: a volume spanning several nodes needs
its size on each of them, so a non-shared pool can hand out what its most
constrained node can take. Summing the logical figures would promise several
times over what is free. In the cluster view, the `errors` of a pool may be
those of one node only, as the daemon keeps the last node it reads: use
`per_node` to see the errors of every node.

### `list_pool_volumes`

Lists the volumes the pools serve. Use it to find what takes the space of a
pool, or the volumes no object uses any more.

#### OpenSVC API, authorization, and freshness

```text
GET /api/pool/volume[?name=<pool>]
```

The daemon requires the global `root` grant.

#### Input

| Input | Meaning |
|---|---|
| `pool` | Optional exact pool name; omitted for the volumes of every pool |
| `orphans_only` | Optional; `true` keeps only the volumes no object uses |
| `limit` | Optional page size between 1 and 200; defaults to 100 |
| `cursor` | Optional `next_cursor` of a previous call |

#### Output

| Output field | Meaning |
|---|---|
| `provenance` | Daemon API source and MCP collection time |
| `pool_filter`, `orphans_only` | The filters applied |
| `total`, `count` | Matching volumes, and volumes in this page |
| `volumes[]` | Volumes sorted by pool, then path |
| `next_cursor`, `truncated` | Continuation of the page |

Each volume holds `path`, `pool`, `size_bytes` (the size the pool served),
`is_orphan`, `children` (the object paths using the volume, at most 32), and
`charges`: the bytes it takes of other pools, sorted by pool, at most 32.
