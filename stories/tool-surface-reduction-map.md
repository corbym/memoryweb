# Tool-surface reduction map — post-visualise consolidation analysis

**Status:** CLOSED (memoryweb implemented 2026-07-27)

**Shared-surface node:** `option-post-visualise-recordari-tool-surface-reduction-map-20-15-14-with-recent-history-fold-365c5a00`

**Recordari:** STORY-266 still open (design questions). memoryweb followed for parity.

---

## memoryweb disposition (all followed)

| Change | Disposition | Implementation |
|--------|-------------|----------------|
| `alias` + `rename_domain` → `domains(action=…)` | **follow** | `domains` actions: `list`, `add_alias`, `remove_alias`, `resolve`, `rename` |
| `restore` → `forget(restore:true)` | **follow** | `forget(restore=true)` un-archives; `restore` hard-cut |
| Retire `trace` | **follow** | Hard-cut; pair verification via `why_connected` only |
| `recent` → `history(order=modified)` | **follow** | `history(order=modified, group_by_domain=…)`; `recent` hard-cut |

**Net:** 21 → 16 MCP tools.

Retired tool names added to `removedTools` in `tools/definitions_test.go` and hard-cut
in `tools/tools.go`. Skill + AGENTS.md + README updated.

---

## References

- Shared-surface node: `option-post-visualise-recordari-tool-surface-reduction-map-20-15-14-with-recent-history-fold-365c5a00`
- Prior consolidation: `tool-consolidation-reduce-28-too-5ba2a680`, STORY-045
- Trace spike: `stories/trace-vestigial-fate.md` (closed: retire)
