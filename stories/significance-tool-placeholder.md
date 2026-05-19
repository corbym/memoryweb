# Story Placeholder: Significance tool (declared + structural)

**Status:** Placeholder  
**Created:** 2026-05-19  
**Shared reference:** significance tool: dual-signal importance analysis - declared + structural

---

## Intent

Introduce a dedicated significance tool that surfaces two ranked views of importance side by side:
1. Declared significance (occurred_at-curated spine)
2. Structural significance (graph centrality weighted by recency of active links)

The tool should explicitly flag divergence between the two views so missed curation and stale significance become actionable.

---

## Why this is a placeholder

This workstream is intentionally deferred until orient spine enhancement is shipped. The orient change is low-risk and gives immediate value; significance builds on top with heavier query and ranking logic.

---

## Planned requirements (draft)

1. New tool: significance(domain, recency_window?)
2. Output sections:
- declared_ranked
- structural_ranked
- divergence_flags
3. Default recency_window: 90 days
4. Structural ranking weights links from recently active nodes higher than dormant/archived neighborhoods
5. Divergence flags include:
- high structural, missing occurred_at
- occurred_at present, structurally isolated

---

## Open questions

1. Exact centrality metric and weighting formula.
2. Whether archived-neighbor effects are excluded or negatively weighted.
3. Performance strategy for large domains (indexing/materialized calculations vs live query).
4. How much of this should eventually be folded into orient vs kept separate.

---

## Acceptance criteria skeleton

1. Tool returns both declared and structural lists for a domain.
2. Divergence candidates are explicitly identified.
3. recency_window changes ranking outcomes in predictable ways.
4. Live-only and domain scoping contracts are preserved.
5. Tool descriptions provide clear behavioral guidance and usage boundaries.
