# audit: lift resolved-pair suppression when either node is revised

**Status:** DONE — commit 85a618b

`audit(mode=stale)` Rule 1 suppresses contradicting pairs permanently once a
`resolved`/`resolved_by`/`supersedes` edge exists between them. Revising either
node after the resolution does not re-surface the pair — the suppression is blind
to whether the resolution still covers the current content. Confirmed empirically
in Recordari prod 2026-09-12; the same suppression query structure is live in
memoryweb `db/audit.go`.

Fix: add a timestamp-qualified condition to the resolved-pair anti-join so that a
substantive revision to either node after the resolution was created lifts the
suppression and re-surfaces the pair for re-review.

Closes shared-surface issue
`audit-stale-resolved-pair-over-suppression-revision-does-not-lift-suppression-890fa78e`.

---

## Motivation

Rule 1 in `FindDrift` (`db/audit.go:213`) queries for `contradicts` edges and
excludes pairs where a resolution edge already exists between the two nodes:

```sql
WHERE e.relationship = 'contradicts'
  AND NOT EXISTS (
      SELECT 1 FROM edges r
       WHERE r.relationship IN ('resolved', 'resolved_by', 'supersedes')
         AND (
             (r.from_node = a.id AND r.to_node = b.id) OR
             (r.from_node = b.id AND r.to_node = a.id)
         )
  )
```

This anti-join has no timestamp check, so once a resolution edge exists it
suppresses the pair forever — even if one of the nodes is revised months later to
say something substantively different. An agent revising a previously-resolved
decision never knows it has reintroduced a conflict.

The fix adds two conditions to the inner SELECT:

```sql
AND r.created_at >= a.updated_at
AND r.created_at >= b.updated_at
```

Semantics: the resolution edge only suppresses the pair when it was created
**at or after** the last update to both nodes. If either node was updated more
recently than the resolution, the anti-join returns no rows, and the pair is
surfaced.

Accepted caveat (from the filed issue): `revise()` bumps `updated_at` on any
field — including tags — so a trivial tag-only revision will also lift suppression.
This is an acceptable false-positive: the agent re-reviews the pair and re-confirms
the resolution is still valid.

---

## Changes

### 1. `db/audit.go` — Rule 1 anti-join

The only change is two additional `AND` conditions in the resolved-pair inner
SELECT. The existing comment block above the query documents the intent; extend it
to mention the timestamp guard.

Current inner SELECT (lines ~222-229):

```sql
NOT EXISTS (
    SELECT 1 FROM edges r
     WHERE r.relationship IN ('resolved', 'resolved_by', 'supersedes')
       AND (
           (r.from_node = a.id AND r.to_node = b.id) OR
           (r.from_node = b.id AND r.to_node = a.id)
       )
  )
```

Replace with:

```sql
NOT EXISTS (
    SELECT 1 FROM edges r
     WHERE r.relationship IN ('resolved', 'resolved_by', 'supersedes')
       AND (
           (r.from_node = a.id AND r.to_node = b.id) OR
           (r.from_node = b.id AND r.to_node = a.id)
       )
       AND r.created_at >= a.updated_at
       AND r.created_at >= b.updated_at
  )
```

No schema change, no new parameters, no other rules affected.

Extend the comment block above the query:

> Timestamp guard: the resolution edge only suppresses the pair when it was
> created at or after the last update to both nodes (r.created_at >= a/b.updated_at).
> If either node is revised after the resolution, the anti-join returns no rows,
> re-surfacing the pair for re-review. Accepted false-positive: any revise() call
> (including tag-only) bumps updated_at and lifts suppression.

### 2. `db/audit_test.go` — new test cases

Add to the existing `FindDrift` test file:

- `TestFindDrift_ResolvedPair_NotSurfaced` — `contradicts` edge + `resolved` edge
  created after both nodes' last update → pair NOT in drift candidates (existing
  behaviour, regression guard).

- `TestFindDrift_ResolvedPair_RevivedAfterResolution` — `contradicts` edge +
  `resolved` edge created at T; then one node revised at T+1 →
  pair IS surfaced (new behaviour).

- `TestFindDrift_ResolvedPair_BothRevisedAfterResolution` — same but both nodes
  revised after resolution → pair IS surfaced.

- `TestFindDrift_ResolvedPair_RevisionBeforeResolution` — node revised at T,
  resolution created at T+1 → pair NOT surfaced (resolution still covers current
  content).

---

## Acceptance criteria

- A `contradicts` pair with a `resolved` edge where the resolution was created
  before either node's `updated_at` is re-surfaced by `audit(mode=stale)`.
- A `contradicts` pair where the resolution was created after both nodes' last
  update remains suppressed.
- All four new `TestFindDrift_ResolvedPair_*` cases pass.
- No existing `FindDrift`-related tests broken.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `db/audit.go` | Add timestamp guard to Rule 1 resolved-pair anti-join |
| `db/audit_test.go` | 4 new `TestFindDrift_ResolvedPair_*` cases |

---

## References

- Shared-surface issue: `audit-stale-resolved-pair-over-suppression-revision-does-not-lift-suppression-890fa78e`
- Related finding: `finding-resolved-edge-suppression-is-permanent-revision-to-either-endpoint-does-not-lift-audit-mode-stale-suppression-over-suppression-is-confirmed-empirically-in-recordari-prod-9a931b5f`
- `db/audit.go` Rule 1, lines ~200-270
