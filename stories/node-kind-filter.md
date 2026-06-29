# node_kind filter — search, recent, audit, history, significance

**Status:** OPEN (memoryweb half)

**Shared-surface nodes:**
- `cross-cutting-gap-neither-product-s-search-list-tools-support-filtering-or-listing-by-node-kind-11f49cfb`
- `spec-node-kind-filter-parameter-shape-and-behavior-across-search-recent-audit-history-significance-d6900a54`

**Recordari:** STORY-153 shipped 2026-06-28. memoryweb mirrors the same contract.

**Depends on:** `stories/node-kind.md` (COMPLETE — v1.30.0)

---

## Why

After the node_kind taxonomy shipped (v1.30.0), agents still cannot list or filter by
kind. `search(query="option")` matches text in labels, not `node_kind=option`. The
same gap exists on every list-shaped retrieval tool: `recent`, `audit` (all modes),
`history`, and `significance` (dual-signal and trust).

Recordari closed its half in STORY-153. memoryweb has the taxonomy but not the query
parameter — filing Recordari-only would leave the sibling product with the identical
gap.

---

## Parameter contract

Add optional `node_kind` to all five tools. Mirror Recordari STORY-153 exactly:

- **Type:** string, space-separated union match (same convention as `tags`).
- **Placement:** hard SQL `WHERE` filter before scoring/ranking — not embedding ranking.
- **Non-goals:** no negation (`not transient`), no wildcards.

Per tool (from shared-surface spec):

| Tool | Behaviour |
|------|-----------|
| `search` | Filter alongside domain/memory_id. If `node_kind` set but `query` omitted, fall back to `updated_at DESC` (same as `exact=true` with no semantic ranking). Enables pure listing: `search(node_kind="option")`. |
| `recent` | ANDed into existing domain+tags+memory_id intersection. `group_by_domain` keeps node_kind in digest lines. |
| `audit` | Applies across stale/orphans/archived the way `tags` already does. Verify audit digest lines include node_kind before assuming zero response-shape change. |
| `history` | Enables pulling the decision spine of just standing rules, etc. |
| `significance` | Generalises trust mode's hardcoded reference/transient exclusion into an explicit parameter. |

Response shape barely changes — lean/digest lines already render node_kind where applicable.

---

## Changes

### db layer

- `SearchNodes`, `RecentChanges` / `RecentChangesScoped`, `Timeline` / `GetHistoryForMemoryID`,
  `GetSignificance`, `FindDrift`, `FindDisconnected`, `ListArchived` — add `nodeKinds []string`
  parameter; apply `AND node_kind IN (...)` when non-empty.

### tools layer

- Parse `node_kind` on search, recent, audit, history, significance handlers.
- Pass through to Store methods.
- Update tool descriptions and property schemas.

---

## Acceptance criteria

- `search(node_kind="standing")` returns only standing nodes; omitting `query` orders by
  `updated_at DESC`.
- `recent(node_kind="decision")` respects domain/tags/memory_id intersection.
- `audit(mode=stale, node_kind="transient")` filters candidates.
- `history(node_kind="standing", important_only=true)` returns standing spine entries only.
- `significance(node_kind="decision")` filters all four sections consistently.
- Space-separated union: `node_kind="decision standing"` matches either kind.
- Existing behaviour unchanged when `node_kind` omitted.
- `go test ./...` green.

---

## Files (expected)

- `db/search.go`, `db/timeline.go`, `db/significance.go`, `db/audit.go` — filter plumbing
- `tools/search.go`, `tools/recent.go`, `tools/archive.go`, `tools/history.go`,
  `tools/significance.go` — arg parsing + descriptions
- Matching `*_test.go` files — one test per tool confirming filter behaviour

---

## References

- Recordari: STORY-153, `story-153-implementation-decisions-node-kind-filter-placement-and-routing-5bdf3a11`
- Shared-surface spec: `spec-node-kind-filter-parameter-shape-and-behavior-across-search-recent-audit-history-significance-d6900a54`
