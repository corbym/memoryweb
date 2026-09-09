# CR-47: Fix collectEdges encapsulation break

**Status:** DONE
**Priority:** Low

`db/edges.go:186-208` — `collectEdges` uses `db *sql.DB` parameter instead of the store,
breaking encapsulation. Also swallows scan errors with `log.Printf`.

---

## Acceptance criteria

- [x] Change `collectEdges` to accept `*Store` instead of `*sql.DB`
- [x] Return errors instead of logging and continuing; `scanRows` helper used
