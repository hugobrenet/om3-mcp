---
domain: auth
tools:
  - get_caller_identity
stability: experimental
---

# Auth Tools

This document describes the tool that reports who the OpenSVC daemon takes
the caller for, and with which grants.

Implementation:

- business logic: `internal/core/auth_whoami.go`;
- MCP definitions: `internal/tools/auth.go`.

## Tools

### `get_caller_identity`

Returns the user name and the grants the daemon applies to the caller. Use it
to explain a call the daemon refused, or objects missing from a listing: the
daemon filters what the caller may see by its grants.

#### OpenSVC API and authorization

```text
GET /api/auth/whoami
```

Any authenticated caller may read its own identity; no grant is needed. On the
HTTPS listener, the caller is the identity of the daemon token the MCP
obtained by exchange, which may differ from the SSO identity. On the Unix
socket, it is the identity of the forwarded daemon token.

#### Input and output

The tool takes no input. The output holds `provenance`, `name`, the user name
the daemon recognized, `grants` and `grants_truncated` (after 256 grants).
Each grant holds a `role`, such as `root`, `admin`, `operator` or `guest`, and
a `namespace` when the role is limited to one; a role granted cluster-wide has
no `namespace`. Grants are sorted by role then namespace, duplicates removed.

The grants are read from the raw grant list of the daemon, not from its role
map, which merges a role granted cluster-wide with the same role limited to
namespaces. The MCP reports the grants without deciding what they allow; the
daemon remains the authority on each call. The authentication method is not
returned.
