# STORY-005: Add occurred_at to embedding text for date-query recall

**Type:** feature

## Goal

Appends `"Occurred D Month YYYY"` to embedding text when `occurred_at` is set, making date-phrased queries resolvable via semantic search.

**Deferred** (2026-07-26): Titan-embed probe on Recordari STORY-272 showed 0/41 date-query recall after embedding change — date-shaped questions should use `history(from=…, to=…)` or `history(important_only=true)` instead. Not worth implementing until an arctic-embed probe shows non-zero date-query recall.

**Reopen triggers:**
1. Arctic-embed probe (10–20 date-phrased queries vs nodes with `occurred_at` set) shows non-zero recall
2. A new retrieval mechanism makes embedding dates load-bearing

Note: `occurred_at` surfacing in lean/digest lines is already implemented and working. This story is only about embedding the date in the vector text.

Refs: `stories/occurred-at-embedding.md`, Recordari STORY-272 done node `story-272-done-occurred-at-embedded-occurred-d-month-yyyy-surfaced-in-nodesordigest-result-lines-54817be7`

## Acceptance criteria

- [ ] Define acceptance criteria
