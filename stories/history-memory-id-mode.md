# history: memory_id mode — neighbourhood timeline from anchor

**Status:** COMPLETE — v1.21.0 (commit 5abf103)

**Shared-surface node:** `history-tool-memory-id-mode-cont-d4fceae4`

**Sibling:** `stories/significance-memory-id-mode.md` (COMPLETE)

---

## Why

Domain-mode `history` answers "everything that happened in this domain, sorted by date."
Workstream analysis needs a different question: "how did **this cluster** evolve?" —
scoped to the subgraph reachable from a known anchor, not the whole domain.

Completes the workstream triad:

- `recall` / `search` → find anchor
- `history(memory_id)` → how the workstream evolved (timeline)
- `significance(memory_id)` → current load-bearing health

---

## Contract (shipped)

When `memory_id` is supplied:

- Fetch neighbourhood at **depth 2** (default; configurable). Clip at anchor's domain
  boundary — cross-domain edges not followed.
- Order by `COALESCE(occurred_at, created_at) ASC` — same as domain mode.
- `important_only=true` → decision spine of the workstream.
- `tags`, `from`/`to` date filters apply in memory_id mode.
- `memory_id` takes precedence if both `domain` and `memory_id` supplied.

Reuses BFS neighbourhood fetch from significance memory_id path.

---

## Verification

Implemented and tested:

- `tools/history.go` — `memory_id` param
- `tools/history_test.go` — memory_id mode test suite
- `tools/definitions.go` — schema exposes `memory_id`

---

## References

- Shared-surface node: `history-tool-memory-id-mode-cont-d4fceae4`
- Significance counterpart: `stories/significance-memory-id-mode.md`
- Scoping principle: `traversal-tool-scoping-consisten-c691512d`
