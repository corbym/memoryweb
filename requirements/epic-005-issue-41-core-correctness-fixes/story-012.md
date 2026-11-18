# STORY-012: Staleness clock: measure transient and open-question rules from updated_at, not created_at

**Type:** bug

## Goal

db/audit.go FindDrift: Rule 6 (transient nodes, line 493) uses `created_at < cutoff7`; Rule 4 (stale open questions, line 429) falls back to `created_at` when occurred_at is null. A node revised yesterday is still flagged if it was created 8+ days ago. The 7-day and 30-day windows are hardcoded constants.

Fix (minimum viable — this story):
- Rule 6: change the query predicate from `created_at < ?` to `COALESCE(updated_at, created_at) < ?` so a revision restarts the transient clock.
- Rule 4: change the ageFilter constant from `(occurred_at IS NULL AND created_at < ?)` to `(occurred_at IS NULL AND COALESCE(updated_at, created_at) < ?)`.
- Add tests: create a node, revise it on day 8, assert it no longer appears in audit(mode=stale).

Deferred to a follow-up story (do not implement here):
- Optional review_after column on nodes.
- revise(verified=true) clock-reset mechanism.
- Configurable windows (the config table already exists for future use).

Refs: db/audit.go:429 (Rule 4 ageFilter), db/audit.go:493 (Rule 6 query)

## Acceptance criteria

- [ ] AC-STORY-012-20cd729b: A transient node revised within the last 7 days does not appear in audit(mode=stale), regardless of when it was created
- [ ] AC-STORY-012-1b856387: An open-question node revised within the last 30 days does not appear in audit(mode=stale), regardless of when it was created
- [ ] AC-STORY-012-2b55f048: A transient node not revised for more than 7 days still appears in audit(mode=stale)
- [ ] AC-STORY-012-1717e0df: An open-question node not revised for more than 30 days still appears in audit(mode=stale)
- [ ] AC-STORY-012-5c16d235: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-16T18:18:29Z -->
Dev notes:
- Rule 6 fix (db/audit.go:493): change `created_at < ?` to `COALESCE(updated_at, created_at) < ?` in the transient-node query.
- Rule 4 fix (db/audit.go:429): change the ageFilter constant's occurred_at-null branch from `(occurred_at IS NULL AND created_at < ?)` to `(occurred_at IS NULL AND COALESCE(updated_at, created_at) < ?)`.
- The 7-day and 30-day cutoff constants remain unchanged in this story.
- Deferred to a follow-up story: optional review_after column, revise(verified=true) clock-reset, configurable window durations.