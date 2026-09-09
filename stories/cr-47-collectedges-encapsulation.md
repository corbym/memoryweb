# CR-47: Fix collectEdges encapsulation break

**Status:** READY
**Priority:** Low

`db/edges.go:186-208` — `collectEdges` uses `db *sql.DB` parameter instead of the store,
breaking encapsulation. Also swallows scan errors with `log.Printf`.

---

## Acceptance criteria

- [ ] Change `collectEdges` to accept `*Store` instead of `*sql.DB`
- [ ] Return errors instead of logging and continuing
