# STORY-009: Label budget: soft-truncate label at 120 chars in lean/digest output

**Type:** bug

## Goal

tools/lean.go — toLeanEntry (line 98) stores node.Label verbatim; digestLineFromEntry (line 199) renders it with no length limit. truncateWhy() already caps why_matters at 150 chars with sentence-boundary awareness. Label is the one field present in every lean/digest response (orient, search, significance, history, audit). Live 600-char labels have been observed; a 30-line orient digest burns several thousand tokens against the intent of digest mode.

Fix:
- Add a truncateLabel(s string) (string, bool) helper in tools/lean.go mirroring truncateWhy but at 120 chars; no sentence-boundary search needed (labels are not prose).
- Call it in toLeanEntry alongside truncateWhy; set Truncated=true when either field is truncated.
- Surface over-limit labels in audit(mode=kind_coverage) migration_candidates, alongside the existing decision-node pattern detection.
- No hard rejection on remember/revise — existing data must not break.
- Update digestLineFromEntry to use the already-truncated leanEntry.Label (no change needed there once toLeanEntry truncates).

Refs: tools/lean.go, tools/lean.go:58 (truncateWhy pattern to mirror), db/audit.go (kind_coverage migration query)

## Acceptance criteria

- [x] AC-STORY-009-7ed93281: remember and revise accept labels longer than 120 chars without error
- [x] AC-STORY-009-f7114675: remember and revise include a label_nudge field when the supplied label exceeds 120 chars
- [x] AC-STORY-009-813bb6bc: All list-shaped responses (orient, search, history, significance, audit) truncate labels at 120 chars and set truncated=true on the affected entry
- [x] AC-STORY-009-907fefeb: recall(id) returns the full untruncated label regardless of its length
- [x] AC-STORY-009-35cea196: audit(mode=kind_coverage) includes nodes with labels over 120 chars in migration_candidates
- [x] AC-STORY-009-77cc87da: docs/memoryweb-skill.md reflects the 120-char label cap, the truncated flag covering both fields, and the label_nudge signal
- [x] AC-STORY-009-cc6871dc: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-16T18:18:20Z -->
Dev notes:
- Add truncateLabel(s string) (string, bool) helper in tools/lean.go, limit 120 chars. No sentence-boundary search — labels are not prose. Mirror the existing truncateWhy pattern (lean.go:58).
- Call from toLeanEntry alongside truncateWhy; set Truncated=true when either fires.
- Truncation is output-only — labels are stored in full in the DB. No DB-layer change needed.
- label_nudge: add to the single-node remember response struct and the batch entry struct in tools/remember.go.
- audit(mode=kind_coverage) migration query: extend kindMigrationPatternSQL or the candidates query in db/audit.go FindKindCoverage to also include nodes with len(label) > 120.
- Skill update locations: lean entry description (~line 233 "id, label, and a truncated why_matters"), truncated flag note (~line 261), and the response-signals section that documents trust_nudge — add label_nudge alongside it.

<!-- backlog-mcp: 2026-11-16T18:31:43Z -->
Added truncateLabel(s string) (string, bool) helper in tools/lean.go (120-rune cap, no sentence-boundary search). Called from toLeanEntry alongside truncateWhy; Truncated=true when either field fires. Added labelNudge() helper used by remember (single + batch) and revise (single + batch) to emit label_nudge when the stored label exceeds 120 chars. Extended FindKindCoverage in db/audit.go: candidates now match (node_kind='decision' AND text pattern) OR length(label)>120. Updated docs/memoryweb-skill.md: lean entry description, truncated flag note, label_nudge advisory, and kind_coverage description. Added 7 new tests: TestSearch_LeanFormat_LabelTruncated, TestSearch_LeanFormat_LabelAtLimit, TestRecall_ReturnsFullLabelUntruncated, TestAddNode_LabelNudge_WhenLabelExceeds120Chars, TestAddNode_NoLabelNudge_WhenLabelUnder120Chars, TestFindKindCoverage_LongLabelIncluded. All tests green.
