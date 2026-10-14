# STORY-007: Add option to disable dream injection in the stop hook

**Type:** feature

## Goal

The stop hook (`memoryweb_save_hook.sh`) currently captures a `memoryweb dream` digest and injects it into the block prompt every time the sweep interval fires. This adds latency and token cost even when the extra context is not useful. Add a config option (`dream_injection`, default `true`) that lets users turn the dream digest off independently of the stop hook itself. When the option is `false`/`0`/`off`, the hook still fires and blocks as normal but omits the dream capture and injects only the plain filing prompt.

## Behaviour
- New option key: `dream_injection` (boolean, default `true`)
- Read via `memoryweb_option_enabled "dream_injection" "true"` in `memoryweb_save_hook.sh`
- When disabled: skip the `memoryweb dream` subprocess call entirely; use the short plain prompt (the else-branch that already exists at line 100)
- When enabled: existing behaviour unchanged
- The `options` subcommand (in `main.go`) must list and document this key alongside `sweep_interval_turns`
- No change to the precompact or subagent-start hooks

## Acceptance criteria

- [x] AC-STORY-007-6be4d0f1: `memoryweb options set dream_injection false` writes the key to `~/.memoryweb/config.json`
- [x] AC-STORY-007-ed364150: Stop hook with `dream_injection=false` does not invoke `memoryweb dream`, does not include dream text in the block reason, but still blocks and prompts filing
- [x] AC-STORY-007-01e63181: Stop hook with `dream_injection=true` (or key absent) behaves exactly as before
- [x] AC-STORY-007-cee21aa5: `memoryweb options list` shows `dream_injection` with its current value and default
- [x] AC-STORY-007-2459b5da: Existing hook tests still pass; add a test covering the disabled-injection path

## Notes on measuring dream injection value
Dream injection gives the filing agent a list of recently-filed nodes and drift candidates so it can avoid duplicates and choose better connections. Hard to measure directly. Proxies:
- **Orphan rate**: run `audit(mode=orphans)` after sessions with and without injection; fewer orphans = better context
- **Duplicate rate**: compare how often `suggested_connections` returns a near-duplicate after a session
- **Connection density**: track average edge count on nodes filed in each mode (higher = richer context)
- The simplest test: turn it off for a week and check whether `audit(mode=orphans)` comes back with more noise after each session
<!-- backlog-mcp: 2026-10-14T09:33:11Z -->
Added `dream_injection` boolean option (default `true`) to `optionSpecs` in `main.go`. Wrapped the dream capture block in `memoryweb_save_hook.sh` with `memoryweb_option_enabled "dream_injection" "true"` — when false, the dream subprocess is skipped entirely and the plain filing prompt is used. Added `TestSaveHookDreamInjectionDisabled` to `hooks/hooks_test.go` and added `dream_injection` to the key list in `TestOptionsCmd_PrintDefaults` in `main_test.go`. All tests pass.

<!-- backlog-mcp: 2026-10-14T10:29:16Z -->
Post-completion code review (medium) found 5 issues — all fixed:
1. memoryweb_precompact_hook.sh did not honour dream_injection=false (binary invoked unconditionally) — fixed with same memoryweb_option_enabled guard.
2. TestOptionsCmd_PrintDefaults had no assertion that dream_injection's default prints as true — added.
3. TestSaveHookDreamInjectionDisabled negative assertion could pass vacuously if binary silently fails — restructured test to include a positive-control run first.
4. Dream header string "memoryweb dream" hardcoded in tests — extracted as dreamDigestHeader const; applied to all three affected assertions.
5. No round-trip test for `options set dream_injection false` → config.json — added TestOptionsCmd_SetDreamInjectionFalse.
