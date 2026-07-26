# digest-mode on cross-domain orient bootstrap

**Status:** OPEN

**Shared-surface node:** `cross-cutting-gap-digest-mode-not-shipped-on-orient-or-audit-04aa0ae4` (residual: bootstrap only)

**Depends on:** `stories/digest-mode-orient-audit.md` (COMPLETE — domain-scoped orient/audit)

---

## Why

Digest mode shipped for domain-scoped `orient(domain=…, digest=true)` and
`orient(domains=[…], digest=true)` but **not** for the no-arg bootstrap path
`orient()` → `orientCrossDomain()`.

Verified 2026-07-26: `tools/orient.go orientCrossDomain()` accepts no `digest` parameter;
returns full JSON objects with `id`, `label`, `updated_at` per recent entry — highest
token cost at the exact moment agents have least domain context.

Recordari finding (2026-07-01): cross-domain bootstrap returned full JSON while
descriptions claimed digest rendering.

---

## Proposed changes

### `tools/orient.go`

- Thread `digest bool` from `handleOrient` into `orientCrossDomain(limit, digest)`.
- When `digest=true`, render domain recent entries as single-line digest strings
  (reuse `digestLineFromEntry` or a bootstrap-specific formatter — bootstrap currently
  uses a slimmer `recentEntry` without why_matters; decide whether to enrich or keep
  id+label+updated_at only in digest form).

### `tools/definitions.go`

Document `digest` applies to bootstrap path.

### Tests

- `orient()` with `digest=true` returns string lines, not JSON objects per node
- `orient(domain=X, digest=true)` unchanged (regression)

---

## Out of scope

- Multi-domain `domains=[…]` path (already supports digest)
- audit bootstrap (audit always requires mode)

---

## References

- Digest standing rule: `digest-mode-render-multi-node-re-8fa64dcf`
- Parent: `stories/digest-mode-orient-audit.md`
