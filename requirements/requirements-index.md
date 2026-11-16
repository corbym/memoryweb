# Requirements Index

## EPIC-001: Platform Integrations — `draft`

| Story | Title | Status | Type |
|-------|-------|--------|------|
| [STORY-002](./epic-001-platform-integrations/story-002.md) | Port memoryweb hooks to OpenCode as an event-bus plugin | draft | feature |
| [STORY-007](./epic-001-platform-integrations/story-007.md) | Add option to disable dream injection in the stop hook | done | feature |

## EPIC-002: Observability & CI — `draft`

| Story | Title | Status | Type |
|-------|-------|--------|------|
| [STORY-003](./epic-002-observability-ci/story-003.md) | Stats validation for description-only release batches | draft | chore |
| [STORY-004](./epic-002-observability-ci/story-004.md) | Remove self-signed cert fallback in release workflow | deferred | bug |

## EPIC-003: Deferred — `deferred`

| Story | Title | Status | Type |
|-------|-------|--------|------|
| [STORY-005](./epic-003-deferred/story-005.md) | Add occurred_at to embedding text for date-query recall | deferred | feature |
| [STORY-006](./epic-003-deferred/story-006.md) | HTTP MCP Bridge — memoryweb serve subcommand | deferred | feature |

## EPIC-004: CLI & Database Tooling — `draft`

| Story | Title | Status | Type |
|-------|-------|--------|------|
| [STORY-008](./epic-004-cli-database-tooling/story-008.md) | memoryweb import — cross-database merge CLI command | done | feature |

## EPIC-005: Issue #41 — Core correctness fixes — `draft`

| Story | Title | Status | Type |
|-------|-------|--------|------|
| [STORY-009](./epic-005-issue-41-core-correctness-fixes/story-009.md) | Label budget: soft-truncate label at 120 chars in lean/digest output | done | bug |
| [STORY-010](./epic-005-issue-41-core-correctness-fixes/story-010.md) | orient(topic=...) without domain silently drops topic — return error or run workspace-wide search | done | bug |
| [STORY-011](./epic-005-issue-41-core-correctness-fixes/story-011.md) | Misdomain check: run KNN check on all filings, not only new-domain creation | draft | bug |
| [STORY-012](./epic-005-issue-41-core-correctness-fixes/story-012.md) | Staleness clock: measure transient and open-question rules from updated_at, not created_at | draft | bug |
| [STORY-013](./epic-005-issue-41-core-correctness-fixes/story-013.md) | Hardcoded English keywords: move drift/placeholder keyword lists into config table | draft | bug |
| [STORY-014](./epic-005-issue-41-core-correctness-fixes/story-014.md) | orient(topic=X) without domain runs workspace-wide semantic search | draft | feature |
