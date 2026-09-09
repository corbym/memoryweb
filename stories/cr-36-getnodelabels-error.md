# CR-36: Fix silent error returns in GetNodeLabels

**Status:** READY
**Priority:** Low

`db/nodes.go:106-108` — `GetNodeLabels` returns empty map on error. Caller cannot
distinguish "no nodes matched" from "database error".

---

## Acceptance criteria

- [ ] Return `(map[string]string, error)` — propagate actual errors
- [ ] Update callers to handle the error
