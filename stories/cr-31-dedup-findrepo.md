# CR-31: Extract findRepoRoot to shared test helper

**Status:** READY
**Priority:** Low

`hooks/hooks_test.go:29-40`, `cmd/dream/main_test.go:43-55`, `cmd/embeddings/main_test.go:44-57` —
Duplicated `findRepoRoot` logic across three test files.

---

## Acceptance criteria

- [ ] Create `testutil/findrepo.go` with shared `FindRepoRoot` function
- [ ] All three test files import and use the shared helper
- [ ] All tests pass
