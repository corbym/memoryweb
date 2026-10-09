# orient redesign: three-section response + description contract

**Shared-surface nodes:** `orient-redesign-replace-all-node-3edca09d`, `orient-tool-description-contract-c43c477e`
**Status:** COMPLETE (implemented June 2026)

---

## Why

`orient` currently returns an `all_nodes` dump. On a mature domain this is ~147k characters — exceeding agent context limits in some clients and forcing file-based workarounds. Even when it fits, receiving 107 nodes in arbitrary insertion order gives no prioritisation signal: an agent cannot tell what to read first, what is active, or what decisions shaped the domain.

The tool description also has two known problems (from the tool-description-quality-pass audit, May 2026):
- It ends with a hardcoded list of all tool names; `significance` is absent from it and it will drift on every tool change.
- The stale-tool-list pattern is fragile — it should be removed and replaced with post-orient guidance.

---

## Response structure change

Replace `all_nodes` with three purposeful sections. `all_nodes` is removed entirely.

### Section 1: `declared_spine`
Nodes with `occurred_at` set, ordered chronologically, capped at 20.
The curated narrative: decisions explicitly marked significant.
**Already implemented** (v1.10.0, `orient-spine-declared-spine-ship-d0d25af4`). No change needed.

### Section 2: `significant`
Top 10–15 nodes by recency-weighted structural importance. Same signal as the `significance` tool.

Ranking formula (inbound weighted degree):
```
SUM(1.0 / (1 + days_since_linker_updated))  per linking node
```
Decays with age of linking node so dormant linkers contribute near-zero. Gives *current* structural importance.

Query sketch (SQLite-compatible, integer days):
```sql
SELECT n.id, n.label, n.description, n.why_matters, n.domain,
       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.transient,
       SUM(1.0 / (1.0 + CAST((julianday('now') - julianday(n2.updated_at)) AS REAL)))
         AS importance_score
FROM edges e
JOIN nodes n  ON e.to_node   = n.id
JOIN nodes n2 ON e.from_node = n2.id
WHERE n.domain = ?
  AND n.archived_at IS NULL
  AND n2.archived_at IS NULL
GROUP BY n.id
ORDER BY importance_score DESC
LIMIT 15
```

### Section 3: `recent`
Last 10 nodes by `updated_at`. Cold-start anchor: where work was actually happening, regardless of structural importance.

### No deduplication
A node appearing in both `significant` and `recent` is *stronger* signal than either alone — suppressing it from one list hides information the agent needs. Sections are complete and independent; the agent reasons about overlap, not the tool. Total node count across all three sections is at most 45 (20+15+10), reduced by natural overlap.

### If an agent needs more
The correct next step is `search` with a specific query — not a broader orient call.

---

## Handler changes

In `tools/tools.go`, `summariseDomain()`:

1. Remove the `AllNodes` field from the orient response struct (or stop populating it).
2. Add `Significant []db.Node` to the response struct.
3. Add a `h.store.SignificantNodes(domain, 15)` call — or inline the SQL above as a new store method `SignificantNodes(domain string, limit int) ([]db.Node, error)` in `db/db.go`.
4. `Recent` is already returned; verify it is ordered by `updated_at DESC` and capped at 10.
5. Update the `orient` tool description (see below).

New store method to add to `db/db.go`:
```go
func (s *Store) SignificantNodes(domain string, limit int) ([]Node, error) {
    // inbound recency-weighted degree query above
    // must filter archived_at IS NULL on both n and n2
}
```

---

## Tool description

Replace the current orient description with one that satisfies the four requirements from `orient-tool-description-contract-c43c477e`:

1. **Imperative first** — first sentence tells the agent the single most important thing.
2. **Session-start purpose unambiguous** — cold-start orientation, picking up where you left off.
3. **Post-orient guidance** — use `search` for specific questions; do not call orient again to find more nodes.
4. **Section semantics explained** — agent must know what each section means.
5. **No hardcoded tool list** — remove the `Other tools in this server: ...` line; it drifts.

Draft description:
```
Call this at the start of every session to orient yourself in a domain before filing or searching.
Returns three sections: declared_spine (curated significant decisions with occurred_at, chronological),
significant (structurally load-bearing nodes right now, by recency-weighted inbound connections),
and recent (where work was last happening). Overlap between sections is intentional — a node in both
significant and recent is stronger signal than either alone. After orient, use search for specific
questions. Do not call orient again to find more nodes — it is a starting point, not an exhaustive
index. This tool only returns live memories. Archived memories are hidden. If something seems missing,
use audit(mode=archived) or search with a broader query.
```

---

## Implementation notes (post-completion)

- `SignificantNodes` was **not** added as a new store method. `GetSignificance(domain, 15, 90)` already executes the exact same recency-weighted inbound-degree query and returns `SignificanceResult.Structural` — reused directly.
- `CountNodes(domain string) (int, error)` **was** added to `db/db.go`. The empty-domain check and `total_nodes` field both require an accurate live node count; summing the `SignificanceResult` slices misses orphan nodes (no edges, no `occurred_at`).
- The hardcoded tool list in the orient description was already absent from the codebase before this story ran. The "Stale tool list cleanup note" section below is therefore moot. No `TestOrient_DescriptionToolListComplete` test was added.
- `TestOrient_DescriptionImperativeFirst` was added and passes; `TestOrient_DescriptionToolListComplete` was deliberately omitted (see note above).
- All 5 new tests pass; all 3 pre-existing regressions were fixed.

---

## Tests added

In `tools/tools_test.go`:

- `TestOrient_HasSignificantSection` ✅
- `TestOrient_SignificantRankedByImportance` ✅
- `TestOrient_NoAllNodes` ✅
- `TestOrient_DescriptionImperativeFirst` ✅
- `TestOrient_RecentCappedAtTen` ✅

`TestOrient_DescriptionToolListComplete` — **not added** (list was already absent; see implementation notes)

---



In `tools/tools_test.go`:

- `TestOrient_HasSignificantSection` — response includes `significant` array (may be empty for a domain with no edges)
- `TestOrient_SignificantRankedByImportance` — with edges set up, the most-linked node appears first in significant
- `TestOrient_NoAllNodes` — response does NOT include `all_nodes` field (or it is absent/nil/empty)
- `TestOrient_DescriptionImperativeFirst` — orient description does not start with "The " or "This "
- `TestOrient_DescriptionToolListComplete` — orient description contains all current tool names (fails when a tool is added/removed without updating the list)

In `db/db_test.go`:

- `TestSignificantNodes_EmptyDomain` — returns empty slice, no error
- `TestSignificantNodes_RankedByInboundDegree` — node with more inbound edges from recent nodes ranks higher
- `TestSignificantNodes_ExcludesArchived` — archived node does not appear even if it has many edges

---

## Stale tool list cleanup note — open question before removal

The hardcoded tool list (`tool-surface-cleanup-orient-disc-49306bf0`, shipped v1.15.0) was added to solve a confirmed VS Code Copilot problem: MCP tools are deferred and not loaded into agent context until `tool_search` is called. An agent that discovers `orient` via tool_search would previously see no other memoryweb tools — the list in the orient description was the only reliable fix.

**The list should NOT be removed without verifying this behaviour has changed.** If deferred-tool loading still works the same way, removing the list reintroduces the original problem: agents using memoryweb in VS Code Copilot will silently be missing most of the tool surface.

Two options:
1. **Keep the list, but make it auto-generated at build time** (or via a test assertion) so it stays accurate as tools change. Remove `significance` absence as a bug fix.
2. **Confirm VS Code Copilot now loads all MCP tools eagerly** (or that a better mechanism exists), then remove it.

The `TestOrient_DescriptionNoHardcodedToolList` test proposed above is premature — do not add it until option 2 is confirmed. Instead, add a test that asserts the list is present and contains all current tool names, so it fails when a tool is added or removed without updating the list.
