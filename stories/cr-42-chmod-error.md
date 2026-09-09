# CR-42: Fix os.Chmod error swallowing

**Status:** DONE
**Priority:** Low

`db/store.go:38` — `os.Chmod(path, 0600)` error silently swallowed (nolint present).
DB file may be world-readable.

---

## Acceptance criteria

- [x] Log the error (at minimum `log.Printf`)
- [x] Remove nolint comment
