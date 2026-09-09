# CR-42: Fix os.Chmod error swallowing

**Status:** READY
**Priority:** Low

`db/store.go:38` — `os.Chmod(path, 0600)` error silently swallowed (nolint present).
DB file may be world-readable.

---

## Acceptance criteria

- [ ] Log the error (at minimum `log.Printf`)
- [ ] Remove nolint comment
