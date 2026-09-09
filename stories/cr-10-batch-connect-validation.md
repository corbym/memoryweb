# CR-10: Validate from_memory/to_memory in batch connect

**Status:** READY
**Priority:** High

`tools/connect.go:119-131` — Individual batch edge items are decoded and verdict is
validated, but `from_memory` and `to_memory` are never checked for emptiness.
An item with empty IDs produces confusing store errors.

---

## Acceptance criteria

- [ ] Each batch item validates `from_memory` and `to_memory` non-empty
- [ ] Empty ID items returned in `rejections` array with descriptive error
- [ ] New test: `TestConnect_Batch_EmptyIDs` — empty IDs rejected, valid items processed
- [ ] Existing batch connect tests pass
