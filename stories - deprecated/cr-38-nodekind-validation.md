# CR-38: Validate nodeKind values on insert

**Status:** DONE
**Priority:** Low

`db/nodes.go:440-447` — `AddNodesBatch` and `AddNode` do not validate `nodeKind` values.
Invalid kinds are inserted silently (no CHECK constraint in schema).

---

## Acceptance criteria

- [x] Define valid `nodeKind` constants in `db/nodekinds.go`
- [x] Validate against the list before insert (AddNode + AddNodesBatch)
- [x] Return descriptive error for invalid kinds
- [x] Existing tests pass (search_test.go updated to use valid kinds)
