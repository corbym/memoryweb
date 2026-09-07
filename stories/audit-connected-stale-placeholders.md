# audit: connected-but-stale placeholder detection

Adds detection of nodes that are well-connected (have edges) but semantically stale
— placeholder labels like "TBD", "TODO", "open question older than N days", or labels
that match the superseded/contradicts heuristics but are NOT orphaned. These fall
through both `audit(mode=stale)` (which misses them when they're well-connected) and
`audit(mode=orphans)` (which only finds disconnected nodes). Closes shared-surface
issue `issue-audit-mode-stale-and-audit-mode-orphans-both-miss-connected-but-stale-placeholder-nodes-529635d0`.

---

## Motivation

`audit(mode=stale)` surfaces nodes based on label heuristics (TBD, superseded, open
question age) and explicit `contradicts` edges. `audit(mode=orphans)` surfaces nodes
with no edges at all. There is a third staleness pattern neither covers:

> A node is well-connected (has edges) but its **label or content signals it is still
> a placeholder** — e.g., `"open question: should we use X?"` filed 60 days ago with
> no resolution edge, or `"TBD: decide on auth strategy"` with three inbound `depends_on`
> edges from other active decisions.

These nodes are actively harmful: other decisions depend on them, but they remain
unresolved placeholders. They never surface to the agent because `audit(mode=stale)`
skips connected nodes by default, and `audit(mode=orphans)` skips them because they have
connections.

The fix is a new `mode=stale` sub-check (or a new `mode=placeholders` mode, see below)
that specifically targets connected nodes whose label/content matches placeholder
patterns regardless of connection count.

---

## Design

### Placeholder patterns

A node is a connected-stale placeholder if ALL of the following are true:

1. `archived_at IS NULL` (live)
2. It has at least one edge (connected)
3. One or more of:
   - Label starts with or contains "TBD", "TODO", "FIXME", "open question", "decide",
     "pending", "placeholder" (case-insensitive)
   - `node_kind = 'issue'` AND `occurred_at` is NULL AND created more than 30 days ago
     (configurable via `stale_issue_days`, default 30)
   - `node_kind = 'goal'` AND no outbound `led_to` or `resolved` edge AND created more
     than 60 days ago (configurable via `stale_goal_days`, default 60)
   - Label matches the existing `audit(mode=stale)` heuristics but connection count ≥ 1

### Delivery: extend `audit(mode=stale)` rather than a new mode

Add a `placeholders` section to the existing `audit(mode=stale)` response alongside
`candidates`. This avoids a new mode name, stays within the existing audit contract, and
means agents already calling `audit(mode=stale)` get the new data without a prompt change.

Response shape change (additive, no existing fields removed):

```json
{
  "candidates": [...],
  "results_truncated": false,
  "placeholders": [
    {
      "id": "...",
      "label": "...",
      "domain": "...",
      "node_kind": "...",
      "age_days": 47,
      "connection_count": 3,
      "reason": "open question label, 47 days old, 3 connections"
    }
  ],
  "placeholders_truncated": false
}
```

`placeholders` defaults to `limit` (same as candidates). When `placeholders_truncated`
is true, raise `limit` and call again.

---

## Changes

### 1. `db/audit.go` — `FindPlaceholders`

New function:

```go
// FindPlaceholders returns connected live nodes whose label or node_kind signals they
// are unresolved placeholders, ordered by age descending.
func (s *Store) FindPlaceholders(domain string, limit, staleIssueDays, staleGoalDays int) ([]PlaceholderCandidate, error)
```

`PlaceholderCandidate`:

```go
type PlaceholderCandidate struct {
    Node
    AgeDays         int    `json:"age_days"`
    ConnectionCount int    `json:"connection_count"`
    Reason          string `json:"reason"`
}
```

Query skeleton:
```sql
WITH edge_counts AS (
    SELECT node_id, COUNT(*) AS cnt
    FROM (
        SELECT from_id AS node_id FROM edges WHERE verdict IS NULL OR verdict <> 'archived'
        UNION ALL
        SELECT to_id   AS node_id FROM edges WHERE verdict IS NULL OR verdict <> 'archived'
    )
    GROUP BY node_id
),
candidates AS (
    SELECT n.*, ec.cnt AS connection_count,
           CAST((julianday('now') - julianday(COALESCE(n.occurred_at, n.created_at))) AS INTEGER) AS age_days
    FROM nodes n
    JOIN edge_counts ec ON ec.node_id = n.id
    WHERE n.archived_at IS NULL
      AND (? = '' OR n.domain = ?)           -- domain filter
      AND ec.cnt >= 1
      AND (
            lower(n.label) GLOB '*tbd*'
         OR lower(n.label) GLOB '*todo*'
         OR lower(n.label) GLOB '*fixme*'
         OR lower(n.label) GLOB '*open question*'
         OR lower(n.label) GLOB '*pending*'
         OR lower(n.label) GLOB '*placeholder*'
         OR lower(n.label) GLOB '*decide*'
         OR (n.node_kind = 'issue'  AND n.occurred_at IS NULL AND age_days > ?)
         OR (n.node_kind = 'goal'   AND age_days > ?
             AND NOT EXISTS (
                 SELECT 1 FROM edges e
                 WHERE (e.from_id = n.id OR e.to_id = n.id)
                   AND e.relationship IN ('led_to','resolved','resolved_by')
             ))
      )
)
SELECT * FROM candidates ORDER BY age_days DESC LIMIT ?
```

### 2. `tools/archive.go` — extend stale handler

In the `mode=stale` branch, after building `candidates`, call `FindPlaceholders` and
add `placeholders` / `placeholders_truncated` to the response map.

### 3. `tools/definitions.go` — update `audit` description

Add to the `mode=stale` description:

> Also returns a `placeholders` section: connected live nodes whose label or node_kind
> signals an unresolved placeholder (TBD, open question, stale goal/issue). Check
> `placeholders_truncated` and raise `limit` when true.

### 4. Tests

**`db/audit_test.go`**:
- `TestFindPlaceholders_TBDLabel` — node with "TBD" in label + one edge → returned.
- `TestFindPlaceholders_OpenQuestion` — "open question" label + one edge → returned.
- `TestFindPlaceholders_StaleIssue` — `node_kind=issue`, no occurred_at, created 31 days ago, one edge → returned.
- `TestFindPlaceholders_StaleGoal_NoResolution` — `node_kind=goal`, 61 days old, no led_to/resolved edge → returned.
- `TestFindPlaceholders_StaleGoal_WithResolution` — same but has `led_to` edge → NOT returned.
- `TestFindPlaceholders_Orphan` — no edges → NOT returned (orphans are a separate audit mode).
- `TestFindPlaceholders_Archived` — archived node matching pattern → NOT returned.
- `TestFindPlaceholders_CleanLabel` — normal label + one edge → NOT returned.

**`tools/archive_test.go`**:
- `TestAuditStale_IncludesPlaceholders` — DB has one placeholder node → `audit(mode=stale)` response contains `placeholders` array with the node.
- `TestAuditStale_PlaceholdersTruncated` — more placeholders than limit → `placeholders_truncated: true`.

---

## Acceptance criteria

- `audit(mode=stale)` response includes `placeholders` and `placeholders_truncated` fields.
- Nodes with TBD/TODO/open-question labels AND at least one edge appear in `placeholders`.
- Orphaned nodes (no edges) do NOT appear in `placeholders` (they belong in `mode=orphans`).
- Archived nodes never appear.
- `stale_issue_days` and `stale_goal_days` thresholds are respected.
- All `TestFindPlaceholders_*` and `TestAuditStale_*` cases pass.
- Before merging: update `docs/memoryweb-skill.md` to document the `placeholders` / `placeholders_truncated` fields in the `audit(mode=stale)` response. Check AGENTS.md audit section for accuracy.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `db/audit.go` | `PlaceholderCandidate` type, `FindPlaceholders` |
| `db/audit_test.go` | 8 new `TestFindPlaceholders_*` cases |
| `tools/archive.go` | Call `FindPlaceholders` in `mode=stale` handler; add to response |
| `tools/archive_test.go` | `TestAuditStale_IncludesPlaceholders`, `TestAuditStale_PlaceholdersTruncated` |
| `tools/definitions.go` | Update `audit` description for `mode=stale` |

---

## References

- Shared-surface issue: `issue-audit-mode-stale-and-audit-mode-orphans-both-miss-connected-but-stale-placeholder-nodes-529635d0`
- Related: `db/audit.go` — `FindDrift`, `FindDisconnected` (patterns to follow)
- Related: `tools/archive.go` — existing stale handler structure
