# connect: actionable error when target memory not found

**Status:** OPEN

**Shared-surface node:** `connect-fails-silently-when-targ-e86973ca`

**Related:** `stories/cross-domain-connect-ux.md` (COMPLETE — domain field on suggested_connections)

---

## Why

When `connect(from_memory=A, to_memory=B)` fails because `B` does not exist (wrong ID,
typo, or stale schema), the error says the node was "not found (searched domain X)" and
claims cross-domain connections are "not yet supported". Cross-domain connect **does**
work when both IDs are valid — the message misleads agents into thinking capability is
missing rather than the ID being wrong.

Agents cannot recover without a separate search to discover the target's actual domain.

---

## Changes

### db/edges.go — AddEdge / AddEdgesBatch

When `toID` is not found among live nodes:

1. Look up whether the ID exists archived → error: "memory archived; use restore first"
2. Look up whether the ID exists in another domain (live) → should not happen if COUNT
   is global; if ID truly missing, error without blaming cross-domain
3. Replace misleading "cross-domain not supported" copy with:
   - `"memory not found: %q — verify the ID with recall or search"`
   - When `from_memory` domain is known, include: `"filing domain was %q"`

When `toID` exists live in a different domain than `from_memory`, connect succeeds
(same as today) — add regression test.

### tools/connect.go

Ensure tool-level errors surface the improved message (no swallowing).

---

## Acceptance criteria

- Cross-domain connect with valid IDs succeeds (regression test).
- Missing to_memory error does not claim cross-domain is unsupported.
- Archived target gets distinct error mentioning restore.
- `go test ./...` green.

---

## References

- Shared-surface: `connect-fails-silently-when-targ-e86973ca`
- Precedent: `tools/connect_test.go` TestConnect_CrossDomain_ErrorMentionsDomain
