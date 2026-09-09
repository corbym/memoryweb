# CR-33: Fix VACUUM INTO bind parameter

**Status:** READY
**Priority:** Low

`db/store.go:70` — `VACUUM INTO ?` uses a bind parameter for the filename. SQLite's
`VACUUM INTO` does not support bind parameters in all versions.

---

## Acceptance criteria

- [ ] String-interpolate the path after sanitization (not parameterized)
- [ ] Validate path to prevent injection (no `;` or path traversal)
- [ ] `TestBackup*` tests pass
