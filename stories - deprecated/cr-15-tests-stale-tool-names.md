# CR-15: Update tests.md stale tool names

**Status:** DONE — implemented this session. `tests.md` updated to current tool surface (verified against `tools/definitions.go`): line 8 sidebar check now names `remember`/`search`; test A pass condition uses `search`/`recall`; test C orientation uses `history(order=modified)` with domain (three mentions + source-of-truth line).
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
