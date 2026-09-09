# CR-46: Batch logSignificance inserts in trust.go

**Status:** READY
**Priority:** Low

`db/trust.go:262-266` — One `logSignificance` INSERT per node in `finishTrust`.
Same O(N) individual inserts as significance.go.

---

## Acceptance criteria

- [ ] Use multi-row insert
- [ ] `TestTrust*` tests pass
