# lean/digest lines: surface lifecycle state (resolved/superseded/contested)

**Status:** COMPLETE

**Shared-surface node:** `cross-cutting-lean-digest-result-lines-omit-lifecycle-state-resolved-superseded-contested-agents-misreport-closed-items-as-open-in-both-memoryweb-and-recordari-2ec0aa09`

**Cross-product:** file here first; Recordari mirrors after memoryweb proves shape

---

## Why

Lean and digest result lines show `[id] label — why_matters (date)` but not whether a
memory is still open, resolved, superseded, or contested. Agents summarising "what's
still open" from orient/search/recent overstate open work because resolved issues and
adjudicated contradictions look identical to live ones.

Verified 2026-07-26: `tools/lean.go` — no lifecycle suffix in `digestLineFromEntry` or
`toLeanEntry`.

---

## Design questions (resolve before coding)

1. **Signal source** — derive from graph state, not stored column:
   - `issue` + outbound `resolved`/`resolved_by`/`supersedes` edge → `[resolved]`
   - `contradicts` pair without resolution edge → `[contested]`
   - Label prefix `RESOLVED` (legacy backstop only — prefer edge signal)
2. **Which tools** — at minimum orient significant/recent/spine/rules + search/recent
   digest lines; significance structural/declared if cheap.
3. **Format** — suffix on digest line, e.g. `[resolved 2026-07-09]` or compact
   `(resolved)` — keep token cost minimal.

---

## Proposed changes

### `db/graph.go` or `db/trust.go`

`LifecycleState(nodeID) → none | resolved | contested | superseded` — batch-friendly
for list rendering.

### `tools/lean.go`

Extend `leanEntry` or digest formatters with optional `lifecycle_state` / inline suffix.
Apply in `digestLineFromEntry`, `digestLineFromScored`, grouped-recent formatters.

### Tests

- Fixture: issue with `resolved` edge → digest line includes resolved marker
- Fixture: contradicts pair without resolution → contested marker
- Fixture: ordinary decision → no marker

---

## Out of scope

- New MCP tool
- Changing node_kind at resolve time (agents still use `connect(relationship=resolved)`)

---

## References

- Contradiction resolution: `stories/audit-contradiction-resolution.md` (COMPLETE)
- Connect resolved enum: `stories/connect-resolved-relationship-discoverability.md` (COMPLETE)
