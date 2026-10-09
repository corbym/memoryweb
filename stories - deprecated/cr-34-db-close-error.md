# CR-34: Handle db.Close() error

**Status:** DONE
**Priority:** Low

`db/store.go:50` — `st.db.Close()` error silently discarded. Data may not be flushed.

---

## Acceptance criteria

- [x] `Close()` returns the error from `db.Close()`
- [x] WAL checkpoint error is also captured (best-effort, nolint)
- [x] main.go callers log the error; compile-time interface assertion in backup_test.go
