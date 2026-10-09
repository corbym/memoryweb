# CR-40: Batch purge operations

**Status:** DONE
**Priority:** Low

`db/purge.go:119-140` — Purge processes candidates one at a time. For large purges
(thousands of nodes), a batch `DELETE FROM edges WHERE from_node IN (...)` is faster.

---

## Acceptance criteria

- [x] Batch edge deletion before node deletion
- [x] `TestPurge*` tests pass
