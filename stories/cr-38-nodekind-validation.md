# CR-38: Validate nodeKind values on insert

**Status:** READY
**Priority:** Low

`db/nodes.go:440-447` — `AddNodesBatch` and `AddNode` do not validate `nodeKind` values.
Invalid kinds are inserted silently (no CHECK constraint in schema).

---

## Acceptance criteria

- [ ] Define valid `nodeKind` constants in `db/` package
- [ ] Validate against the list before insert
- [ ] Return descriptive error for invalid kinds
- [ ] Existing tests pass
