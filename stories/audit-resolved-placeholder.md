# audit: detect connected-but-resolved placeholder nodes

**Status:** COMPLETE

**Shared-surface node:** `issue-audit-mode-stale-and-audit-mode-orphans-both-miss-connected-but-stale-placeholder-nodes-529635d0`

---

## Why

A distinct staleness pattern neither existing audit mode catches:

- A **goal**-kind placeholder ("Story needed: api/openapi/admin.yaml") wired via
  `connects_to` to its completion record (STORY-139 complete).
- Not an orphan (has connections).
- Not flagged stale (nodes don't contradict each other).
- Sits forever because nobody updated the placeholder's node_kind/label after work shipped.

Pattern: **resolved-by-connection** — the thing the placeholder held a place for closed
the loop, but the placeholder itself was never retired or revised.

Without a check, every "X needed, see Y" placeholder requires a human or agent
stumbling on it.

---

## Proposed design (resolve at implementation)

Add detection to `audit(mode=stale)` (or a new sub-rule within stale) for placeholder
patterns:

**Candidate heuristics (initial):**
- `node_kind=goal` (or label matching "Story needed:", "TODO:", "Placeholder:")
- Has outbound `connects_to` or `led_to` edge to a node whose label/description
  indicates completion ("complete", "shipped", "RESOLVED", "done")
- Placeholder's own label/description was never updated post-connection

**Output:** surface as stale candidate with reason
`connected placeholder — target appears resolved; revise or archive placeholder`.

Do not auto-archive. Follow forget protocol.

---

## Open questions

1. Should this be a fourth stale sub-rule or a new `audit(mode=placeholders)` mode?
   Prefer extending stale — audit is already the graph-health tool.
2. Label-pattern matching vs node_kind-only — goal kind alone may be too broad.
3. Recordari parity — same heuristic on both products.

---

## Acceptance criteria

- File test fixture: goal placeholder + connects_to completion node → appears in stale audit.
- Live goal node with no completion signal → not flagged.
- Orphan-only node (no edges) → still caught by orphans mode, not double-flagged incorrectly.
- Reason string is actionable ("revise label" or "archive placeholder").
- `go test ./...` green.

---

## Files (expected)

- `db/audit.go` — new drift rule in `FindDrift` or companion query
- `tools/archive.go` — pass through reason in drift candidate output
- `db/audit_test.go`, `tools/archive_test.go` — fixture tests

---

## References

- Example: goal placeholder for STORY-139 in recordari-admin domain (2026-06-28 finding)
- Standing rule context: `list-shaped-responses-that-trunc-941da40a` (audit is Category B completeness tool)
