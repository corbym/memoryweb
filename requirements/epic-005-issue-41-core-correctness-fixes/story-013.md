# STORY-013: Hardcoded English keywords: move drift/placeholder keyword lists into config table

**Type:** bug

## Goal

db/audit.go hardcodes English-only LIKE patterns in three places:
- Rule 3 (line 395): superseded labels — 'old', 'deprecated', 'replaced', 'legacy', 'previous'
- Rule 4 (line 425): stale open questions — 'open question', 'unresolved', 'tbd', 'todo'
- FindPlaceholders (line 781): placeholder detection — 'tbd', 'todo', 'fixme', 'open question', 'pending', 'placeholder', 'decide'
- db/lifecycle.go:180 legacyResolvedLabel: `RESOLVED` prefix

Also: Rule 8 completion signals in FindDrift (line 578) and FindPlaceholders use 'complete', 'shipped', 'resolved', 'done', 'closed'.

Agents filing in other languages get no signal; labels legitimately containing 'old' or 'previous' get false positives.

Fix:
- Add config keys: `drift_superseded_keywords`, `drift_open_question_keywords`, `drift_completion_keywords`, `drift_placeholder_keywords`, `lifecycle_resolved_prefix` — each a JSON array of strings stored in the existing config table (migration 15).
- Write a helper loadKeywordList(st *Store, key string, defaults []string) []string that reads from config, falling back to the current hardcoded values as defaults. Call it at the top of FindDrift and FindPlaceholders.
- legacyResolvedLabel: load the prefix from config key `lifecycle_resolved_prefix` (default "RESOLVED"); the function already calls strings.HasPrefix.
- No schema migration needed — config table already exists.
- Add tests: set a custom keyword list, assert drift surfaces nodes with the custom keyword and not the English defaults.

Do not add more hardcoded keyword rules (issue #41 explicitly asks for this to stop).

Refs: db/audit.go:395, db/audit.go:425, db/audit.go:578, db/audit.go:781, db/lifecycle.go:180, db/store.go (config table access)

## Acceptance criteria

- [ ] AC-STORY-013-9802cc91: Setting drift_superseded_keywords in config changes which labels Rule 3 flags — only configured words are matched, not the English defaults
- [ ] AC-STORY-013-af59276e: Setting drift_open_question_keywords in config changes which labels and descriptions Rule 4 matches
- [ ] AC-STORY-013-61801b55: Setting drift_completion_keywords in config changes which target-node labels Rule 8 and FindPlaceholders treat as completion signals
- [ ] AC-STORY-013-6acd7c4c: Setting drift_placeholder_keywords in config changes which labels FindPlaceholders treats as placeholder signals
- [ ] AC-STORY-013-231697c9: Setting lifecycle_resolved_prefix in config changes what label prefix is treated as a legacy resolved marker
- [ ] AC-STORY-013-75abc4e2: When a config key is absent or empty, the existing English defaults apply — no behaviour change for existing deployments
- [ ] AC-STORY-013-916039d7: No new hardcoded English keyword patterns are introduced anywhere in the change
- [ ] AC-STORY-013-e37c3977: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-16T18:18:32Z -->
Dev notes:
- Add loadKeywordList(st *Store, key string, defaults []string) []string in db/store.go or db/util.go. Reads from the config table (migration 15, already exists); falls back to defaults when key absent or empty.
- Config keys (all stored as JSON arrays of strings): drift_superseded_keywords, drift_open_question_keywords, drift_completion_keywords, drift_placeholder_keywords, lifecycle_resolved_prefix.
- Call sites: FindDrift Rule 3 (db/audit.go:395), Rule 4 (db/audit.go:425), Rule 8 completion signals (db/audit.go:578); FindPlaceholders placeholder-keyword WHERE clause (db/audit.go:781); legacyResolvedLabel (db/lifecycle.go:180).
- SQL LIKE patterns must be built dynamically from the loaded list at query time, not compiled into constants.
- No schema migration needed — config table already exists.
- Default values must exactly match the current hardcoded strings so existing deployments see no behaviour change.
