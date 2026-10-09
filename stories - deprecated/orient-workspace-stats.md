# orient: node counts — live_nodes + archived_nodes

**Status:** COMPLETE — v1.25.0 (commit 07b15de)

**Shared-surface node:** `design-gap-orient-should-return--5cee6e75`
**Meta story placeholder:** `story-add-workspace-stats-to-ori-3fa2e3a4`

---

## Why

orient currently returns `total_nodes` — an ambiguous integer. Agents have no way to tell
whether archived nodes are included or excluded, which matters when something appears to be
missing and the agent needs to decide whether to search or check the archive.

Replace `total_nodes` with two explicit counts: `live_nodes` (non-archived) and
`archived_nodes`. This is all the context a single-tenant local system needs — no tier
limits, no percentages.

---

## Behaviour

### orient response shape change

Replace the `total_nodes` integer field with two fields:

```json
{
  "live_nodes": 140,
  "archived_nodes": 12
}
```

`total_nodes` is **removed**. Search the codebase for any read site that uses `total_nodes`
and update it to `live_nodes`.

### Description update

Update the orient description to reflect the new field names. Replace any reference to
`total_nodes` with `live_nodes`. Add a note that `archived_nodes` shows how many nodes have
been soft-deleted (agents can surface them with `audit(mode=archived)`).

---

## Handler changes

In `tools/tools.go`, `summariseDomain()`:

1. Add a `CountArchived(domain string) (int, error)` call alongside the existing `CountNodes` call.
2. Replace `TotalNodes int` in the orient response struct with `LiveNodes int` and `ArchivedNodes int`.
3. Remove `total_nodes` from the JSON output.

In `db/db.go`:

Add `CountArchived(domain string) (int, error)` — mirrors `CountNodes` but filters
`archived_at IS NOT NULL`.

---

## Tests

In `tools/tools_test.go`:

- `TestOrient_LiveNodesCount` — file N nodes; call orient; assert `live_nodes == N`.
- `TestOrient_ArchivedNodesCount` — file 3 nodes, archive 1; call orient; assert
  `archived_nodes == 1` and `live_nodes == 2`.
- `TestOrient_NoTotalNodes` — call orient; assert response JSON does NOT contain a
  `total_nodes` key at the top level.

In `db/db_test.go`:

- `TestCountArchived_Empty` — fresh domain, returns 0, no error.
- `TestCountArchived_AfterArchive` — archive 2 of 5 nodes; assert returns 2.

---

## Files

- `db/db.go` — `CountArchived()` method
- `tools/tools.go` — orient response struct, `summariseDomain()`, orient description
- `tools/tools_test.go` — three new tests
- `db/db_test.go` — two new tests

## Notes

- Check `main_test.go` and any hook scripts that parse orient output for `total_nodes` references.
- The cross-domain (no-domain) orient path also returns `total_nodes` implicitly via per-domain
  counts — verify it is updated consistently.

## References

- shared-surface node: `design-gap-orient-should-return--5cee6e75`
- meta node: `story-add-workspace-stats-to-ori-3fa2e3a4`
