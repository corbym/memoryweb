# Issue: agents fold source material into decision descriptions

**Status:** COMPLETE

**Shared-surface node:** `issue-agents-fold-source-material-code-docs-logs-search-results-into-a-decision-s-description-instead-of-filing-it-as-node-kind-finding-f63da6de`

**Related:** `stories/remember-finding-linkback.md` (tool-description nudge for decision→finding linkback — partial fix)

---

## Why

Agents file a `node_kind=decision` and embed the evidence (code read, doc fetched,
log output, search results) in the decision's `description` as unstructured prose
instead of filing separate `node_kind=finding` memories and connecting them.

The decision becomes the only record of what was checked — evidence nobody can
independently cite, search, or trust-score. Same failure mode as the monochrome-graph
audit (90% decision / 1 finding on a mature domain).

`remember-finding-linkback.md` addresses one lever (tool description instructing
linkback). This issue is broader: filing discipline, skill Layer 1 rules, and optional
response nudges when a decision description looks like embedded evidence.

---

## Changes (layered — ship cheapest first)

### 1. Skill — `docs/memoryweb-skill.md`

Strengthen Layer 1 (Recordari skill v2 precedent):

- Source material checked during a session → file as `node_kind=finding` first.
- Decisions cite findings by id; a decision must not *be* the finding wearing a
  decision label.
- Add Layer 2 disambiguation guide: when to use finding vs decision vs assumption.

### 2. Tool descriptions (if skill alone insufficient)

- `remember`: reinforce finding-first for evidence (extends linkback story).
- `revise`: warn against growing a decision description with new source material —
  file a finding and connect instead.

### 3. Optional handler nudge (defer unless 1–2 fail)

Non-blocking response hint when filing `node_kind=decision` with a long description
containing patterns suggestive of pasted evidence (heuristic — keep conservative to
avoid noise).

---

## Acceptance criteria

- Skill Layer 1 has explicit source-material-as-finding rule.
- Skill Layer 2 has finding vs decision disambiguation.
- Coordination with `remember-finding-linkback.md` — no contradictory wording.
- Optional handler nudge only if filed as separate follow-up story.

---

## Files (expected)

- `docs/memoryweb-skill.md` (minimum)
- Possibly `tools/definitions.go` if description layer shipped

---

## References

- Shared-surface node: `issue-agents-fold-source-material-code-docs-logs-search-results-into-a-decision-s-description-instead-of-filing-it-as-node-kind-finding-f63da6de`
- Partial tool fix: `stories/remember-finding-linkback.md`
- Graph audit context: `checkpoint-node-kind-migration-p-74b2a9d6`
