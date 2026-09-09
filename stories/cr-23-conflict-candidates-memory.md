# CR-23: Fix FindConflictCandidates memory load

**Status:** READY
**Priority:** Medium

`db/audit.go:34` — Loads ALL nodes with embeddings into memory, then filters in Go.
For large domains this is a memory + CPU bomb.

---

## Acceptance criteria

- [ ] Filter embedded nodes at SQL level (WHERE clause), not in Go
- [ ] Add domain scoping to reduce memory footprint
- [ ] `TestFindConflictCandidates*` tests pass
