# CR-50: Document group_by_domain + node_kind constraint

**Status:** READY
**Priority:** Low

`tools/recent.go:38-39` — `group_by_domain` + `node_kind` rejected at runtime but
not documented in the schema in `definitions.go`.

---

## Acceptance criteria

- [ ] Add note to `recent` tool description in `definitions.go`
- [ ] Or: support the combination (remove the restriction)
