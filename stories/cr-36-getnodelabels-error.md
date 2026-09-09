# CR-36: Fix silent error returns in GetNodeLabels

**Status:** DONE
**Priority:** Low

`db/nodes.go:106-108` — `GetNodeLabels` returns empty map on error. Caller cannot
distinguish "no nodes matched" from "database error".

---

## Acceptance criteria

- [x] Return `(map[string]string, error)` — propagate actual errors
- [x] Update callers to handle the error (tools/revise.go logs and falls back)
