# CR-17: Escape LIKE wildcards in tag filter

**Status:** READY
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
