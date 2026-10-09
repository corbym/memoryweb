# CR-50: Document group_by_domain + node_kind constraint

**Status:** DONE
**Priority:** Low

`tools/recent.go:38-39` — `group_by_domain` + `node_kind` rejected at runtime but
not documented in the schema in `definitions.go`.

---

## Acceptance criteria

- [x] Added "Cannot be combined with node_kind — returns an error if both are set." to `group_by_domain` description in `definitions.go`
