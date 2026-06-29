# audit: retire resolved contradicts pairs from stale re-flagging

**Status:** OPEN

**Shared-surface node:** `issue-resolving-a-contradicts-edge-doesn-t-clear-it-same-pair-re-flags-in-audit-mode-stale-forever-a87db2d5`

**Related:** `stories/audit-conflicts-mode.md` (detection/surfacing — upstream)

---

## Why

Observed live: contradiction pairs resolved by revising one side's label to
"RESOLVED \<date\>" and adding a resolution note. The underlying `contradicts` edge
was left untouched. `audit(mode=stale)` flags pairs based on the edge relationship,
not label text — so both pairs resurface every stale sweep. Future agents must
re-derive "already resolved" from prose every time.

Without edge-level or status-level retirement, every contradiction resolution is
Sisyphean.

---

## Proposed design options (pick one at implementation)

### Option A — resolved relationship type

Add `supersedes` or `resolved_by` edge type. When an agent adjudicates a contradiction:
- add resolution edge
- optionally archive or revise the losing side

`audit(mode=stale)` excludes pairs where a `resolved_by`/`supersedes` edge exists
between the contradicting nodes.

### Option B — node status field

Add optional `status` column or convention (separate from node_kind):
`active | resolved | superseded`.

Audit checks status before surfacing contradicts pairs.

### Option C — archive the contradicts edge

New tool or extend `disconnect` to mark contradicts edges as resolved (soft-delete
with audit log). Stale scan ignores resolved edges.

**Recommendation:** Option A or C — keeps resolution as graph action, visible in
`recall`/`why_connected`, doesn't require a new column. Document in forget/revise
protocol: "after resolving a contradiction, disconnect or supersede the contradicts edge."

---

## Acceptance criteria

- Fixture: two nodes with `contradicts` edge → flagged in stale audit.
- After resolution action (chosen option) → same pair not re-flagged.
- Unresolved contradicts pairs → still flagged.
- Agent guidance added to `audit` and/or `revise` tool descriptions.
- `go test ./...` green.

---

## Files (expected)

- `db/audit.go` — stale contradicts scan excludes resolved pairs
- `tools/archive.go` — description update for resolution workflow
- Possibly `db/edges.go` if new relationship type or edge metadata
- Tests in `db/audit_test.go`, `tools/archive_test.go`

---

## References

- Upstream: `conflict-flagging-is-candidate-s-4a5be655`, `stories/audit-conflicts-mode.md`
- Recordari story chain: `story-strengthen-contradiction-detection-log-on-remember-structured-possible-contradicts-flag-new-audit-mode-contradictions-dfcd80f9`
