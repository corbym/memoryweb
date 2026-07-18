# Tool-surface reduction map — post-visualise consolidation analysis

**Status:** OPEN

**Shared-surface node:** `option-post-visualise-recordari-tool-surface-reduction-map-20-15-14-with-recent-history-fold-365c5a00`

**Primarily Recordari** — memoryweb has no 20-tool directory cap; file here for
cross-product parity review per `how-to-use-the-shared-surface-dea47fa6`.

---

## Why

Recordari faces a hard **20-tool directory cap**. `visualise` retirement (admin UI
replacement) frees one slot. This option maps where the next reductions come from
without losing capability — grounded in memoryweb production usage stats (104 sessions,
30 Apr–26 Jun 2026) as sibling-product signal.

memoryweb should review the map for tools that exist on both products: folds and
retirements here may become memoryweb stories even without the external cap.

---

## Proposed folds (confidence order)

| Change | Tools removed | Rationale |
|--------|---------------|-----------|
| `alias` + `rename_domain` → `domains(action=…)` | −2 | Domain-admin trio ~6 combined calls, all list-side |
| `restore` → `forget(restore:true)` | −1 | Symmetric archive/unarchive; gated-flag-over-new-tool pattern |
| Retire `trace` | −1 | 1 call / 104 sessions; verification role moved to `why_connected`; see `stories/trace-vestigial-fate.md` |
| Optional: `recent` → `history(order=modified)` | −1 | Both chronological listings (7+5 calls) |

**Net:** 20 → 15 (14 with recent fold), 5–6 slots headroom under cap.

---

## Keep despite low/zero stats

Do **not** cull on usage alone:

- `audit`, `suggest_connections`, `why_connected`, `disconnect` — skill-contract-driven
- `significance` — weakest keeper; if room needed, surface trust inside orient and drop tool

---

## Caveats

- Stats are **memoryweb** (single-tenant), frequency top-N per session, no sequence data.
- Stats predate `why_connected` promotion to pair-verification — re-rank after STORY-183.
- Re-validate against Recordari's own audit_log read path (STORY-165) before freezing.

---

## memoryweb action items

For each proposed fold, decide whether memoryweb follows for parity:

1. **domains action enum** — memoryweb has separate `domains`, `alias`, `rename_domain`;
   fold is optional (no cap pressure).
2. **forget(restore:true)** — evaluate gated-flag pattern vs keeping `restore` tool.
3. **trace retirement** — spike in `stories/trace-vestigial-fate.md` first.
4. **recent→history fold** — evaluate `history` description + `order` param vs separate
   `recent` tool.

This story closes when each row has a memoryweb decision (follow / defer / N/A) filed
to shared surface or split into product-specific implementation stories.

---

## Acceptance criteria

- Each proposed fold has explicit memoryweb disposition documented in this file or a
  linked child story.
- Recordari-side decisions tracked separately (STORY-045 successor pass).
- Skill + `removedTools` blacklist plan drafted for any retirement adopted on memoryweb.
- No capability removed without documented retrieval path.

---

## References

- Shared-surface node: `option-post-visualise-recordari-tool-surface-reduction-map-20-15-14-with-recent-history-fold-365c5a00`
- Usage stats: `finding-memoryweb-stats-log-analysis-104-sessions-30-apr-26-jun-2026-confirms-orient-is-the-most-called-tool-log-format-has-no-per-call-sequence-data-ccaba1fa`
- Prior consolidation: `tool-consolidation-reduce-28-too-5ba2a680`, STORY-045
- Trace spike: `stories/trace-vestigial-fate.md`
- Gated-flag standing rule: `standing-recordari-s-mcp-tool-surface-is-capped-at-20-prefer-a-gated-flag-field-on-an-existing-tool-over-a-new-tool-a546f2eb`
