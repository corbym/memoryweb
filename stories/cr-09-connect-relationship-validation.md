# CR-09: Validate relationship in single connect

**Status:** DONE — implemented this session. `requireNonEmpty` now validates `relationship` in single connect; new `TestConnect_EmptyRelationship` covers it. All tests pass.
**Priority:** High

`tools/connect.go:55-60` — `from_memory` and `to_memory` are validated non-empty but
`relationship` is not. An empty relationship string passes through to `AddEdge`.

---

## Acceptance criteria

- [ ] `requireNonEmpty("relationship", params.Relationship)` called in single connect mode
- [ ] New test: `TestConnect_EmptyRelationship` returns validation error
- [ ] Existing `TestConnect_*` tests pass
