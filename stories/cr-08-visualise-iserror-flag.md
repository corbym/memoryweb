# CR-08: Fix missing IsError on visualise empty domain

**Status:** READY
**Priority:** High

`tools/graph.go:110` — Error-like JSON string returned without `IsError: true`.
Compare with line 98 which correctly sets `IsError: true`.

---

## Acceptance criteria

- [ ] Line 110 returns via `errorResult()` or sets `IsError: true`
- [ ] `TestVisualise_*` tests pass
