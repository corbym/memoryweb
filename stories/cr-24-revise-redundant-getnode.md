# CR-24: Reduce redundant GetNode calls in revise

**Status:** READY
**Priority:** Medium

`tools/revise.go:99,144,223,289` — Single revise does 2 extra `GetNode` round-trips
(before and after update). Batch does 2N. If `UpdateNode` returned the updated node,
this halves.

---

## Acceptance criteria

- [ ] `UpdateNode` returns the updated `Node` (or a separate `UpdateNodeAndReturn` method)
- [ ] Single revise: remove post-update `GetNode` call
- [ ] Batch revise: remove per-item post-update `GetNode` calls
- [ ] Existing `TestRevise*` tests pass
