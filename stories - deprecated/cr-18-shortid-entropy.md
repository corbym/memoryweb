# CR-18: Increase shortID entropy

**Status:** DONE
**Priority:** Medium

`db/nodes.go:42` — `shortID()` generates 4 random bytes (2^32 space). With slug truncated
to 32 chars, birthday-bound collision becomes non-trivial at ~100K nodes with the same
slug prefix.

---

## Acceptance criteria

- [ ] `shortID()` generates 8 random bytes (16 hex chars, 2^64 space)
- [ ] Existing tests pass (ID format may change — update assertions if needed)
- [ ] New test: `TestShortID_Uniqueness` — generate 10K IDs, no duplicates
