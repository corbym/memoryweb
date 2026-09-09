# CR-15: Update tests.md stale tool names

**Status:** READY
**Priority:** High

`tests.md:8,34,75` — References `add_node`, `search_nodes`, `get_node`. These were
renamed to `remember`, `search`, `recall`. Every test will fail if followed.

---

## Acceptance criteria

- [ ] Replace `add_node` → `remember`
- [ ] Replace `search_nodes` → `search`
- [ ] Replace `get_node` → `recall`
- [ ] Replace `recent_changes` → `history(order=modified)`
- [ ] Verify all tool names match current `tools/definitions.go`
