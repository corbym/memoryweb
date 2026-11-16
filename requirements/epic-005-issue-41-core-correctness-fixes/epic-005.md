# EPIC-005: Issue #41 — Core correctness fixes

## Goal

Five bugs and design gaps filed by Fable 2 in GitHub issue #41. In priority order: label budget, staleness clock, hardcoded English keywords, orient-topic/no-domain silent drop, misdomain check scope. Each is a self-contained fix with no cross-dependencies.

## Stories

- [x] [STORY-009](story-009.md) — Label budget: soft-truncate label at 120 chars in lean/digest output
- [x] [STORY-010](story-010.md) — orient(topic=...) without domain silently drops topic — return error or run workspace-wide search
- [ ] [STORY-011](story-011.md) — Misdomain check: run KNN check on all filings, not only new-domain creation
- [ ] [STORY-012](story-012.md) — Staleness clock: measure transient and open-question rules from updated_at, not created_at
- [ ] [STORY-013](story-013.md) — Hardcoded English keywords: move drift/placeholder keyword lists into config table
- [ ] [STORY-014](story-014.md) — orient(topic=X) without domain runs workspace-wide semantic search
