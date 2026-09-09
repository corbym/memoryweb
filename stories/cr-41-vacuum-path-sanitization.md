# CR-41: Add VACUUM INTO path sanitization

**Status:** DONE
**Priority:** Low

`db/store.go:70` — When switching from bind parameter to string interpolation for
`VACUUM INTO`, the path must be sanitized to prevent injection.

---

## Acceptance criteria

- [x] Validate path contains no `;`, `--`, or path traversal sequences
- [x] Quote the path in the SQL string
