# CR-33: Fix VACUUM INTO bind parameter

**Status:** DONE
**Priority:** Low

`db/store.go:70` — `VACUUM INTO ?` uses a bind parameter for the filename. SQLite's
`VACUUM INTO` does not support bind parameters in all versions.

---

## Acceptance criteria

- [x] String-interpolate the path after sanitization (not parameterized)
- [x] Validate path to prevent injection (no `;` or path traversal)
- [x] `TestBackup*` tests pass
