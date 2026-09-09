# CR-39: Warn on AddAlias overwrite

**Status:** READY
**Priority:** Low

`db/domains.go:38-42` — `AddAlias` uses `INSERT OR REPLACE`, silently overwriting an
existing alias's `created_at` and target domain.

---

## Acceptance criteria

- [ ] Check if alias already exists before insert
- [ ] If exists and points to different domain, return warning or error
- [ ] If exists and points to same domain, idempotent (no overwrite)
