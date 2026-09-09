# CR-48: Batch N+1 in suggestEdgesSemanticEnriched

**Status:** READY
**Priority:** Low

`db/graph.go:550-552` — For each candidate, a separate `SELECT tags` query is issued.
Could be batched into a single query.

---

## Acceptance criteria

- [ ] Collect all candidate IDs, fetch tags in one query
- [ ] `TestSuggestEdges*` tests pass
