# CR-25: Fix FindPossibleDuplicates full table load

**Status:** DONE
**Priority:** Medium

`db/nodes.go:682-706` — Loads ALL live non-archived nodes in the domain into memory,
then filters in Go for duplicate detection.

---

## Acceptance criteria

- [ ] Use SQL with normalised label comparison (e.g., `LOWER(TRIM(label))`)
- [ ] Add LIMIT to prevent unbounded result sets
- [ ] `TestFindPossibleDuplicates*` tests pass
