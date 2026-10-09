# CR-17: Escape LIKE wildcards in tag filter

**Status:** DONE — confirmed this session. `db/util.go:18` `escapeLike` escapes `%`, `_`, and `\`; used by `tagFilter` (util.go:52) and the search/graph LIKE clauses (search.go:223/318, graph.go:181); every LIKE clause carries `ESCAPE '\'` (util.go:54, search.go:226/300, graph.go:186-194). Covered by the CR-04 LIKE-escaping fix that shipped in v1.54.8. No code change needed.
**Priority:** Medium

`db/util.go:42-43` — Tag values used as LIKE patterns without escaping `%` or `_`.
A tag like `100%` matches `100anything`.

---

## Acceptance criteria

- [ ] Add `escapeLike(s string) string` helper
- [ ] `tagFilter` wraps each tag with `escapeLike`
- [ ] SQL uses `ESCAPE '\'` clause
- [ ] New test: tag `100%` does not match `100foo`
- [ ] Existing tag filter tests pass
