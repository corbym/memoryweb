# Wave 3 Phases 3–5 — code shipped, release filing gap closed

**Status:** COMPLETE (code + tests); release memory pending v1.41.x tag

**Shared-surface node:** `memoryweb-open-stories-wave-3-execution-order-2ee4eade`

**Resolves:** Handoff node still said "14 OPEN" after Phases 1–2 release filings (v1.39.0,
v1.40.0) while Phases 3–5 landed in the same sprint without corresponding release memories.

---

## What the handoff listed vs repo reality (2026-07-26 audit)

| Phase | Stories | Handoff (Jul 17) | Code | Tests | Release filed |
|-------|---------|------------------|------|-------|---------------|
| 1 | remember-finding-linkback, suggest-connections-conflict-framing, remember-new-domain-warning, skill-audit-protocol, agents-fold-source-material | OPEN | ✅ | ✅ | ✅ v1.39.0 |
| 2 | why-connected-id-params, list-truncation-boolean-signal | OPEN | ✅ | ✅ | ✅ v1.40.0 |
| 3 | trust-inline-annotation, filing-time-trust-nudge | OPEN | ✅ | ✅ `tools/wave3_test.go` | ❌ → this doc |
| 4 | connect-resolved-verdict-field, remember-new-domain-candidate-check | OPEN | ✅ | ✅ `tools/wave3_test.go` | ❌ → this doc |
| 5 | node-kind-coverage-signal | OPEN | ✅ | ✅ `TestAudit_KindCoverage*` | ❌ → this doc |
| 6 | trace-vestigial-fate, tool-surface-reduction-map | OPEN (spikes) | — | — | n/a |
| anytime | tech-debt-variable-naming-sweep | OPEN | — | — | n/a |

**Terminal Wave 3 state:** 11 implementation stories COMPLETE, 3 planning/background stories
remain OPEN by design.

---

## Phase 3 evidence (trust surfacing)

- `tools/trust_helpers.go` — `annotateSignificantWithTrust()` adds `"trust": "low — …"` on
  orient's `significant` section (`scoredLeanEntry.Trust`).
- `tools/remember.go`, `tools/revise.go` — optional `trust_nudge` on content-changing writes.
- `tools/wave3_test.go` — `TestOrient_SignificantLowTrustAnnotation`,
  `TestRemember_TrustNudge*`, `TestRevise_TrustNudge*`.

---

## Phase 4 evidence (parity + domain defense)

- `tools/connect.go` — `verdict` param on `relationship=resolved`; stored on edge.
- `tools/remember.go` — `possible_misdomain`, `suggested_domain`, `suggested_memory_id` on
  new-domain creation when embeddings available.
- `tools/wave3_test.go` — `TestConnect_*Verdict*`, `TestRemember_PossibleMisdomain*`.

---

## Phase 5 evidence (kind coverage)

- `db/audit.go` — `KindCoverageResult`, `FindKindCoverage`.
- `tools/archive.go` — `audit(mode=kind_coverage)`.
- `tools/wave3_test.go` — `TestAudit_KindCoverage`, `TestAudit_KindCoverageTruncation`.
- `docs/memoryweb-skill.md` — documents `audit(mode=kind_coverage)`.

---

## Release filing action

When tagging the next release, file one memoryweb-meta release node covering Phases 3–5
(minor semver if any response-shape fields are new to agents: `trust`, `trust_nudge`,
`possible_misdomain`, `verdict`, `audit(mode=kind_coverage)`).

Suggested message: *v1.41.0 — Wave 3 Phases 3–5: trust surfacing, connect verdict,
misdomain check, kind_coverage audit mode*.

Revise the handoff node description to: *Wave 3 complete except Phase 6 spikes + tech-debt
sweep.*
