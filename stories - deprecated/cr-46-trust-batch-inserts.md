# CR-46: Batch logSignificance inserts in trust.go

**Status:** DONE
**Priority:** Low

`db/trust.go:262-266` — One `logSignificance` INSERT per node in `finishTrust`.
Same O(N) individual inserts as significance.go.

---

## Acceptance criteria

- [x] Use multi-row insert via `logSignificanceBatch`
- [x] `TestTrust*` tests pass
