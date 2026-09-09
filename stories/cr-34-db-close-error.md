# CR-34: Handle db.Close() error

**Status:** READY
**Priority:** Low

`db/store.go:50` — `st.db.Close()` error silently discarded. Data may not be flushed.

---

## Acceptance criteria

- [ ] `Close()` returns the error from `db.Close()`
- [ ] WAL checkpoint error is also captured
- [ ] Callers log or handle the error
