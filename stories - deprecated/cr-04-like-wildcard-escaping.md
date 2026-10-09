# CR-04: Escape LIKE wildcards in search and tag filtering

**Status:** DONE — implemented this session. `escapeLike` helper added; applied to `searchNodesLike`, `searchByWords`, `tagFilter`, and `bestMatch` with `ESCAPE '\'` clauses. All tests pass.

`db/search.go:223` and `db/util.go:42` — User input and tag values containing `%` or `_`
are used as LIKE patterns without escaping. `%` matches any sequence; `_` matches any
single character. A tag like `100%` matches `100anything`.

---

## Affected locations

- `db/search.go:223` — `"%" + query + "%"` in LIKE fallback path
- `db/util.go:42-43` — Tag values used as LIKE patterns in `tagFilter`

---

## Acceptance criteria

- [ ] Add `escapeLike(s string) string` helper to `db/util.go` that replaces `%` → `\%` and `_` → `\_`
- [ ] `search.go` LIKE fallback wraps query with `escapeLike`
- [ ] `util.go` tagFilter wraps each tag value with `escapeLike`
- [ ] SQL LIKE clauses use `ESCAPE '\'` clause
- [ ] New test: `TestSearchNodesExact_LikeEscaping` — node with label `100% complete` found by exact query `100%`, not by `100anything`
- [ ] New test: `TestTagFilter_Escaping` — tag `100%` does not match `100foo`
- [ ] Existing `TestSearchNodes*` and `TestTagFilter*` tests pass

---

## Notes

This is a correctness bug, not a security issue (no data exfiltration possible).
But it causes false-positive search results that confuse agents.
