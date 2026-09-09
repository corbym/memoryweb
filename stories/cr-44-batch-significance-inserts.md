# CR-44: Batch significance_log inserts

**Status:** DONE
**Priority:** Low

`db/significance.go:212-233` — `GetSignificance` writes one `INSERT` per node to
`significance_log`. O(N) individual inserts for large result sets.

---

## Acceptance criteria

- [x] Use multi-row `INSERT INTO ... VALUES (...), (...), ...` via `logSignificanceBatch`
- [x] `TestGetSignificance*` tests pass
