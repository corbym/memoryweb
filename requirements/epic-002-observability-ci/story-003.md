# STORY-003: Stats validation for description-only release batches

**Type:** chore

## Goal

Ops/measurement story — not a code feature unless stats tooling gaps block it.

Description and skill changes can ship with green tests but silently fail to change agent behaviour. The only signal is production stats (orphan rate, orient call patterns, connect-after-remember compliance). Post-v1.39.0 re-baseline was deferred and is now overdue.

After each description-only release batch, run a checklist against `memoryweb-stats` JSONL:
- Orphan rate (session + node level) — expect ↓ after finding-linkback + connect imperative
- `connect` calls per `remember` — expect ↑
- `audit(mode=conflicts)` after filing — expect ↑
- New-domain creation rate — expect ↓ or misdomain warnings visible

Document baseline date + release version + window length. File finding in memoryweb-meta; revise description if metric flat after 3 weeks.

Refs: `stories/description-only-stats-validation.md`, `stats/stats.go`, `MEMORYWEB_STATS_FILE`, orphan baseline `orphan-rate-baseline-28-recent-s-2faef552`

## Acceptance criteria

- [ ] AC-STORY-003-61acfd8c: Script or doc section in README/stats documents which JSONL fields to query for each metric
- [ ] AC-STORY-003-3484fd48: Post-v1.39.0 baseline run completed and filed as a finding in memoryweb-meta
- [ ] AC-STORY-003-7b151f1b: Baseline documents: date, release version, window length
- [ ] AC-STORY-003-8fc103ac: Standing reminder added to release process node or story checklist
- [ ] AC-STORY-003-31ac58f0: If any metric is flat after 3 weeks, a revision to the relevant tool description is filed as a candidate
