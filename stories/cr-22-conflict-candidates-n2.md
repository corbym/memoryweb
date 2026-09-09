# CR-22: O(N²) pairwise distance in FindConflictCandidates

**Status:** DONE
**Priority:** Medium

`db/audit.go:118` — For each node, a separate SQL query runs against all other nodes.
With 100 embedded nodes this is 100 queries each scanning ~100 rows.

---

## Acceptance criteria

- [ ] Replace per-node queries with a single self-join or `vec_distance_cosine` matrix
- [ ] Complexity drops from O(N²) SQL queries to O(1) query
- [ ] `TestFindConflictCandidates*` tests pass
- [ ] New test: `TestFindConflictCandidates_Performance` — 200 nodes completes in <500ms
