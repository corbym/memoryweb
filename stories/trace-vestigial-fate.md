# trace: vestigial-fate spike — usage, role, keep/retire/repurpose

**Status:** OPEN

**Shared-surface node:** `trace-risks-the-same-vestigial-fate-as-visualise-pull-only-no-obvious-always-run-hook-ad372e39`

**Related:** `stories/why-connected-id-params.md` (downgrades trace from pair verification)

---

## Why

`trace(from_id, to_id)` finds the connection chain between two already-known memories.
It is pull-only — nothing in the normal session loop invokes it. Useful only once an
agent or human already has two IDs and wants chain narration.

Same failure pattern as `visualise` before retirement (pull-only, no automatic
invocation, removal candidate) and `significance(mode=trust)` before orient hooks
(`trust-is-pull-only-risks-going-v-9857f924`).

Unlike trust, trace has **no obvious always-run hook** — its use case is inherently
retrospective/explainability-driven, not ambient. "Wire it into orient" may not be the
right fix.

Additional pressure: the shared-surface decision to move pair verification to
`why_connected(from_id, to_id)` removes trace's accidental verification role — one
fewer recurring job.

Production signal (memoryweb stats, 104 sessions Apr–Jun 2026): **1 trace call**.
`visualise`: 12 calls — also low, now moving to admin UI on Recordari side.

---

## Spike questions (resolve before code changes)

1. **Usage** — confirm trace call frequency from memoryweb stats logs; any skill-mandated
   paths that stats undercount?
2. **Distinct job** — after `why_connected` id-params ship, what remains for trace?
   Chain narration between *distant* nodes (6-hop cap) — is that agent-facing or
   human/admin UI?
3. **Keep / retire / repurpose**
   - **Keep as-is** — accept low usage; improve description to state chain-narration
     job explicitly; demote from verification docs.
   - **Retire** — blacklist tool name; migrate skill references to `why_connected` +
     `recall` neighbourhood; matches tool-surface-reduction-map option.
   - **Repurpose** — admin/explainability UI feature (Recordari direction for
     visualise) rather than MCP tool agents must remember to call.
4. **Correctness** — separate concern: `bug-trace-from-id-to-id-is-direction-sensitive-...`
   if trace is kept, fix direction sensitivity.

---

## Recommended sequencing

1. Ship `why-connected-id-params.md` first — clarifies trace's remaining job.
2. Run this spike with fresh stats + skill-contract review.
3. If keeping: description pass only (cheap). If retiring: follow
   `retired-tool-blacklist-strategy` — blacklist name, update skill, add to
   `removedTools` in `tools/tools_test.go`.

---

## Acceptance criteria (spike outcome)

- Written decision: keep / retire / repurpose — filed back to shared surface.
- If **keep**: story closed with description-only follow-up or "no change" rationale.
- If **retire**: new implementation story filed with blacklist + skill migration ACs.
- memoryweb and Recordari positions documented (cross-product; Recordari STORY-192
  may depend on this outcome).

---

## References

- Shared-surface node: `trace-risks-the-same-vestigial-fate-as-visualise-pull-only-no-obvious-always-run-hook-ad372e39`
- Usage stats: `finding-memoryweb-stats-log-analysis-104-sessions-30-apr-26-jun-2026-confirms-orient-is-the-most-called-tool-log-format-has-no-per-call-sequence-data-ccaba1fa`
- Reduction map option: `stories/tool-surface-reduction-map.md`
- Pair verification downgrade: `decision-why-connected-gains-explicit-from-id-to-id-params-exact-match-error-on-miss-becomes-the-recommended-pair-verification-tool-not-trace-2d31785f`
