# CR-39: Warn on AddAlias overwrite

**Status:** DONE
**Priority:** Low

`db/domains.go:38-42` — `AddAlias` uses `INSERT OR REPLACE`, silently overwriting an
existing alias's `created_at` and target domain.

---

## Acceptance criteria

- [x] Check if alias already exists before insert
- [x] If exists and points to different domain, return error
- [x] If exists and points to same domain, idempotent (no overwrite)
