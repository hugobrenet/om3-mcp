---
domain: data-writes
tools:
  - set_object_data_key
  - delete_object_data_key
  - create_object_certificate
  - renew_object_certificate
stability: experimental
---

# Data Write Tools

This document describes the tools that write the keys of the cfg, sec and usr
objects of OpenSVC, and the certificates of the sec objects. They are action
tools: registered only when `OPENSVC_MCP_ACTIONS=enabled`, under the contract
of the [action tools](actions.md).

Implementation:

- business logic: `internal/core/object_data_key_write.go` and
  `internal/core/object_certificate.go`;
- MCP definitions: `internal/tools/object_data.go`.

## Tools

| Tool | OpenSVC API | Kinds | Destructive | Idempotent |
|---|---|---|---:|---:|
| `set_object_data_key` | `PATCH /api/object/path/<ns>/<kind>/<name>/data`, `[{"action":"change"}]` | cfg, sec, usr | Yes | Yes |
| `delete_object_data_key` | `PATCH /api/object/path/<ns>/<kind>/<name>/data`, `[{"action":"remove"}]` | cfg, sec, usr | Yes | Yes |
| `create_object_certificate` | `POST /api/object/path/<ns>/sec/<name>/certificate/create` | sec | Yes | No |
| `renew_object_certificate` | `POST /api/object/path/<ns>/sec/<name>/certificate/renew` | sec | No | Yes |

## Keys

The keys are written through `PATCH …/data`, one operation per call, where
`POST` and `PUT …/data/key` answer neither `OM-Last-Modified` nor take `wait`.
`PATCH` answers 204 with `OM-Last-Modified`, holds the answer with `wait`
until the keys reached every node holding the object, and refuses with 413 a
value over `max_key_size`, 1 MB by default. It is the route of
`om … key add --from`. One key per call keeps the confirmation of the agent
readable.

- `set_object_data_key` sends a `change`, which the daemon applies as an
  upsert: it adds the key, or replaces its value without asking. `add` is not
  used: on a key that exists the daemon answers it with 500 "key already
  exists" rather than 409. The value is a text, `value`, sent as `string`, or
  binary bytes, `value_base64`, sent as `bytes`, at most 64 KiB, an empty
  value allowed. It is never read back nor returned, for a cfg, a sec or a usr.
  The key `password` of a usr is the password of the user.
- `delete_object_data_key` sends a `remove`; the daemon does not refuse a key
  absent. `rename` is not offered.
- Both read the list of the keys first, `GET …/data/keys`, and return
  `existed`: it tells, it does not guard. They return `last_modified`, and,
  with `wait_seconds`, at most 120, the nodes the 408 of the daemon names as
  not reached, `propagation_pending`: the write is done. The 408 of the data
  says "the keys of X are committed as of T, and have not reached N", where
  that of a configuration says "has not reached": the tool reads either
  wording, should the daemon align them.

The daemon requires `admin` on the namespace, and for a usr, holding every
grant of the user or `root`, else 403 "writing the keys of a user requires
holding its grants, missing …".

## Certificates

The tools apply to sec objects only, as the routes are tagged, though the
daemon accepts any key store.

- `create_object_certificate` keeps the `private_key` of the sec, or
  generates one of `bits`, then writes `certificate`, `certificate_chain`,
  `fullpem` and `serial_number`, from the `cn`, `alt_names` and `validity`
  keywords: signed by the sec the `ca` keyword names, whose private key the
  node must read, or self-signed without `ca`. It replaces a certificate the
  sec holds without asking, with a new serial number: destructive, not
  idempotent. The daemon answers 204; the tool reads the keys again and
  returns those five by name and stored size, never their values.
- `renew_object_certificate` answers the `CertificateRenewal` of the daemon:
  `renewed`, `reason`, `domains`, `directory` and `not_after`. A certificate
  not due is kept, `renewed: false` with the reason. `force` is not offered:
  without it the tool replaces nothing that is not due, and a certificate
  installed from elsewhere is kept, with the 409 the daemon explains. An ACME
  renewal may take long: the daemon proxies it without timeout, the MCP waits
  within the request timeout of the cluster, and a client that gives up
  cancels the request.

The daemon requires `admin` on the namespace of the sec, `admin` on the
namespace of the ca when it is elsewhere, and `root` for an ACME renewal
through `acme.webroot`.
