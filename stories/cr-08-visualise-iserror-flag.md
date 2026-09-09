# CR-08: Fix missing IsError on visualise empty domain

**Status:** DONE — implemented this session. Empty-domain path now returns via `errorResult()` (`IsError: true`); `TestVisualiseEmptyDomain` updated to assert the error. All tests pass.
**Priority:** High

`tools/graph.go:110` — Error-like JSON string returned without `IsError: true`.
Compare with line 98 which correctly sets `IsError: true`.

---

## Acceptance criteria

- [ ] Line 110 returns via `errorResult()` or sets `IsError: true`
- [ ] `TestVisualise_*` tests pass
