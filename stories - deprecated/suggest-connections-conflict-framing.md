# remember: suggested_connections conflict-check framing nudge

**Status:** COMPLETE

**Shared-surface node:** `suggested-connections-is-already-9a067784`

**Related:** `stories/audit-conflicts-mode.md` (COMPLETE — read/sweep path; different job)

---

## Why

Semantic candidate-surfacing already runs on every `remember` call via
`suggested_connections`. Agents have organically spotted contradictions by reading
that list and noticing a suggested memory asserts the opposite of what was just
filed — then acting on it.

The gap is **framing**, not mechanism: the tool description nudges agents to check
for **relevance** when reading `suggested_connections`, not for **contradiction**.
The cheapest intervention before building heavier machinery is a description change
in the write path.

Note (2026-07-05 adjudication): both claims are true at different layers. Write-path
framing (this story) and read-path `audit(mode=conflicts)` (shipped) serve different
jobs — domain-wide sweep with authority ranking vs filing-time neighbour list.

---

## Change

### `tools/definitions.go` — `remember` description

Add explicit guidance near the `suggested_connections` mention:

- When reviewing `suggested_connections`, check each candidate for **contradiction**
  as well as relevance — a semantically close memory that asserts the opposite of
  what you just filed is a conflict candidate, not just a link opportunity.
- If a contradiction is found, do not silently file over it — use `connect(relationship=contradicts)` or resolve via `connect(relationship=resolved)` after user confirmation.

Keep it one short paragraph; imperative voice; no structural vocabulary leak.

### Optional response field documentation

If `possible_contradicts` or similar already exists in the handler response, ensure
the tool description names it and ties it to the same framing. If not present on
memoryweb, description-only change is sufficient for this story.

---

## Acceptance criteria

- `remember` description explicitly instructs checking `suggested_connections` for
  contradiction, not just relevance/linking.
- Wording distinguishes write-path neighbour check from `audit(mode=conflicts)`
  domain sweep (one sentence — avoid implying they're interchangeable).
- `TestListTools_*` description quality tests pass.
- `go test ./...` green.

---

## Files (expected)

- `tools/definitions.go` — `remember` tool description (+ `items` property if needed)

---

## References

- Shared-surface node: `suggested-connections-is-already-9a067784`
- Read-path counterpart: `stories/audit-conflicts-mode.md`
- Recordari: STORY-147 (framing nudge shipped there)
