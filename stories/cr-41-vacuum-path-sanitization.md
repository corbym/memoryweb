# CR-41: Add VACUUM INTO path sanitization

**Status:** READY
**Priority:** Low

`db/store.go:70` — When switching from bind parameter to string interpolation for
`VACUUM INTO`, the path must be sanitized to prevent injection.

---

## Acceptance criteria

- [ ] Validate path contains no `;`, `--`, or path traversal sequences
- [ ] Quote the path in the SQL string
