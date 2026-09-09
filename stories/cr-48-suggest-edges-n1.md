# CR-48: Batch N+1 in suggestEdgesSemanticEnriched

**Status:** DONE
**Priority:** Low

`db/graph.go:550-552` — For each candidate, a separate `SELECT tags` query is issued.
Could be batched into a single query.

---

## Acceptance criteria

- [x] Collect all candidate IDs, fetch tags in one query using `inClause`
- [x] `TestSuggestEdges*` tests pass
