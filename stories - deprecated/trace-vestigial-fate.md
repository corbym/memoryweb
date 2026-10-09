# trace: vestigial-fate spike — usage, role, keep/retire/repurpose

**Status:** CLOSED — **retire** (memoryweb 2026-07-27)

**Shared-surface node:** `trace-risks-the-same-vestigial-fate-as-visualise-pull-only-no-obvious-always-run-hook-ad372e39`

**Related:** `stories/why-connected-id-params.md` (downgrades trace from pair verification)

---

## Decision

**Retire** the `trace` MCP tool on memoryweb. Rationale:

- 1 call / 104 production sessions (Apr–Jun 2026)
- Pair verification role fully moved to `why_connected(from_id, to_id)`
- Pull-only with no always-run hook — same vestigial pattern as `visualise`
- Chain narration not agent-facing enough to justify a dedicated tool slot

Retrieval path after retirement:

- Direct edges: `why_connected(from_id, to_id)`
- Neighbourhood context: `recall(id)`
- Multi-hop path finding: not exposed via MCP (db `FindPath` remains for potential admin UI)

Implementation: hard-cut in `tools.go`, `removedTools` blacklist, skill migration.

Recordari disposition tracked separately (STORY-266 / shared-surface option).

---

## References

- Reduction map: `stories/tool-surface-reduction-map.md`
- Pair verification: `decision-why-connected-gains-explicit-from-id-to-id-params-exact-match-error-on-miss-becomes-the-recommended-pair-verification-tool-not-trace-2d31785f`
