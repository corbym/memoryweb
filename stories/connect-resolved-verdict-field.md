# connect: add optional `verdict` field for `relationship=resolved`

**Status:** OPEN

**Shared-surface node:** `story-connect-verdict-field-for-relationship-resolved-split-from-the-p0-enum-fix-as-non-blocking-follow-up-ac6ac252`

**Split from:** `stories/connect-resolved-relationship-discoverability.md` (COMPLETE
2026-07-09) — that story shipped the P0 fix (unblocking `connect(relationship=
resolved)` at all). This story is the deferred, non-blocking remainder: full shape
parity with Recordari's resolution mechanism.

**Depends on:** nothing blocking — `resolved` is already a working relationship
type in memoryweb as of the P0 fix; this only adds an optional field to it.

---

## Why

Recordari's canonical contradiction-resolution mechanism is
`connect(relationship=resolved, verdict=false_positive|reconciled|superseded)` —
the `verdict` field classifies *how* the contradiction was adjudicated (a false
alarm, a genuine reconciliation, or one side being outright superseded), stored on
the edge and returned by `recall`/`why_connected`. This nuance is part of what
Recordari shipped as STORY-162 and what the shared recordari skill document teaches
in its Layer 2 relationship-types table.

memoryweb's `resolved` relationship (shipped 2026-07-09, see the P0 story above)
suppresses stale/conflicts re-flagging correctly, but carries no equivalent outcome
classification — an agent can mark a pair resolved, but not record *why* in a
structured, queryable way. `supersedes` (memoryweb's own pre-existing relationship
type) covers one of the three verdict values informally, but `false_positive` and
`reconciled` have no equivalent today; that nuance currently has to live in the
edge's free-text `narrative` field, unstructured and unqueryable.

This is polish, not a blocker — `resolved` alone is fully functional. Filed
separately so it doesn't get bundled with (or block on) urgent fixes again.

---

## Design

Add an optional `verdict` parameter to `connect`, honoured only when
`relationship=resolved` (silently ignored — not an error — for other relationship
types, consistent with how `narrative` is already optional/relationship-agnostic):

```
connect(from_memory, to_memory, relationship="resolved", verdict="reconciled", narrative="...")
```

**Storage:** mirrors Recordari's shape — a column on the edge, not a new table.
`db/edges.go`'s `Edge` struct gains an optional `Verdict *string` (or `string` with
empty meaning unset). Requires a migration (append-only, per `db/migrations.go`
convention — next version number, `edges: add verdict TEXT column`).

**Validation:** enum `false_positive | reconciled | superseded` at the tool layer
(`tools/connect.go`), same pattern as other enum-constrained string fields — reject
with a clear error if `verdict` is set to something outside the three values, rather
than silently storing garbage.

**Exposure:** `verdict` appears in `recall`'s edge list and in `why_connected`'s
output wherever the edge is shown — same pattern as `narrative` already follows.

---

## Resolved design questions

1. **`verdict` scope — `resolved` only, not `resolved_by`/`supersedes`.**
   Resolved by precedent: Recordari hit this exact question and answered it as
   STORY-167 — "gate verdict storage to relationship==resolved (enum validation
   stays unconditional)". Adopt the same answer rather than re-deriving it:
   `verdict` storage is gated to `relationship="resolved"`; the enum is still
   validated up front regardless of relationship (so a bad enum value is always
   rejected, even on a call whose `verdict` would otherwise be discarded) —
   catches a mistyped value immediately instead of silently swallowing it.
   `resolved_by`/`supersedes` remain memoryweb's pre-existing bare relationship
   types with no verdict concept; not overloading them avoids a second, smaller
   version of the scope-creep this split already avoided once.
2. **Batch mode (`items[]`) needs `verdict` per item.** Also confirmed by
   precedent: Recordari's initial STORY-162 shipped verdict on the single-item
   path only, and batch parity was a separate follow-up fix (also folded into
   STORY-167, "batch connect parity") after the gap was found in post-merge
   review. Ship batch support in the same commit as single-mode this time,
   rather than repeating that two-step miss.

## Awareness — do not replicate scope, just avoid the same bugs

Recordari's STORY-162 cluster needed two more follow-up fixes after initial ship
(`STORY-168`: `GetResolvedEdge` idempotent no-op, to stop duplicate `resolved`
connect calls creating duplicate edges and duplicate audit events; `STORY-170`: a
shared `resolved_pairs` view to replace duplicated bidirectional resolved-edge
checks). Neither is in scope for this story — memoryweb's resolution-suppression
queries are already centralised in `db/audit.go`, not duplicated across call
sites, so `STORY-170`'s problem doesn't exist here. `STORY-168`'s idempotency
concern (calling `connect(relationship=resolved)` twice on the same pair) is
worth a quick acceptance-criterion check at implementation time, but isn't
expected to need new code — `AddEdge` already permits multiple edges between the
same pair with no uniqueness constraint, so a duplicate call just adds a second,
harmless `resolved` edge; confirm this stays true rather than building explicit
deduplication preemptively.

---

## Acceptance criteria

- `connect(relationship="resolved", verdict="reconciled")` stores the verdict on
  the created edge.
- `connect(relationship="resolved", verdict="not-a-real-value")` returns a
  validation error naming the accepted enum values.
- `connect(relationship="resolved")` (no verdict) still works — `verdict` stays
  fully optional, no behaviour change for existing callers.
- `verdict` set on a `resolved_by`, `supersedes`, or any other relationship is enum-
  validated (a bad value is still rejected) but not stored — matches Recordari's
  STORY-167 precedent (see Resolved design questions).
- `recall(id)` and `why_connected(...)` include `verdict` in the edge output when
  set; omit the field (not an empty string) when unset.
- Batch mode (`connect` with `items[]`) supports `verdict` per item.
- New migration added (append-only, next version number) for the `edges.verdict`
  column, per `db/migrations.go`'s existing convention.
- `go test ./...` green.

---

## Files (expected)

- `db/migrations.go` — new migration, `edges` table gains `verdict TEXT` column
- `db/edges.go` — `Edge` struct, `AddEdge`/`AddEdgesBatch` signatures gain
  `verdict *string`
- `tools/connect.go` — decode `verdict`, validate enum, pass through
- `tools/definitions.go` — `connect` schema gains `verdict` property (enum:
  `false_positive`, `reconciled`, `superseded`) in both single and batch item
  schemas; description updated
- `tools/tools.go` or wherever `recall`/`why_connected` serialise edges — include
  `verdict` in output
- `db/edges_test.go`, `tools/connect_test.go` — new tests

---

## References

- P0 prerequisite (shipped first, unblocks this): `stories/connect-resolved-relationship-discoverability.md`
- Recordari's shape this aligns to: `decision-contradiction-resolution-is-a-first-class-resolved-edge-not-a-label-resolve-primitive-replaces-contradicts-edge-and-closes-the-lifecycle-e12972a9`
  (STORY-162) — `verdict` enum `false_positive | reconciled | superseded`
- Migration convention: `CLAUDE.md` — "The migration system — critical rules"
