# CR-02: Fix BFS N+1 query bomb in FindPath

**Status:** DONE — implemented this session. `FindPath` now uses `buildAdjacency()` — a single batch query + in-memory BFS. All `db` tests pass.

`db/graph.go:70` — `FindPath` queries edges **per BFS node**. In a dense graph with max
depth 6, this can execute up to `branching^6` queries. For a domain with 20 edges per
node this is catastrophic.

---

## Current code path

```
FindPath (db/graph.go:70)
  └─ for each BFS node: SELECT edges WHERE from_node = ? (per-node query)
```

---

## Acceptance criteria

- [ ] `FindPath` batch-fetches all edges for the domain upfront into a map
- [ ] BFS traversal uses the in-memory edge map instead of per-node SQL queries
- [ ] Existing `TestFindPath_*` tests pass unchanged
- [ ] New test: `TestFindPath_DenseGraph` with 50+ nodes, 200+ edges — completes in <100ms
- [ ] No regressions in `TestFindPath_*` edge cases (empty graph, single node, no path)

---

## Implementation approach

1. Add `getAllEdgesForDomain(domain string) (map[string][]Edge, error)` to `db/graph.go`
2. Rewrite `FindPath` to call it once, then BFS over the map
3. For cross-domain edges, fetch those separately (small set)

---

## Notes

This is the single highest-impact performance fix in the codebase. The current implementation
is O(branching^depth) SQL queries; the fix makes it O(1) SQL + O(N) BFS in memory.
