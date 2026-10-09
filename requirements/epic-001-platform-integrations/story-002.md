# STORY-002: Port memoryweb hooks to OpenCode as an event-bus plugin

**Type:** feature

## Goal

Ports the memoryweb hook suite (periodic save sweep, subagent orphan audit, compaction filing) to OpenCode as a native event-bus plugin. Currently all hook enforcement exists only for Claude Code sessions. Closes the platform gap and extends the same memory-integrity guarantee to OpenCode users.

Implements all four lifecycle hooks as `hooks/memoryweb_opencode_plugin.ts`:
- PreCompact/Stop → save sweep nudge
- PostCompact → reinject orient context
- UserPromptSubmit → orient-nudge + auto-recall

`memoryweb setup --opencode` installs the plugin idempotently. Reads the same `~/.memoryweb/config.json` option keys as the shell hooks.

Pre-implementation: verify exact OpenCode event names against plugin API docs before writing any code.

Refs: `stories/hooks-opencode-port.md`, shared-surface goal `port-memoryweb-hooks-to-opencode-as-an-event-bus-plugin-periodic-save-sweep-subagent-orphan-audit-compaction-filing-509688a5`

## Acceptance criteria

- [ ] AC-STORY-002-d88f901a: Verify exact OpenCode event names and payload shapes against plugin API docs before writing any code
- [ ] AC-STORY-002-daf2e1f6: `memoryweb setup --opencode` installs the plugin into the OpenCode plugin directory; idempotent on second run
- [ ] AC-STORY-002-034d128d: Orient nudge fires on the first prompt of a session when `session_orient_enabled=true` and no orient call is detected
- [ ] AC-STORY-002-804d37b3: Auto-recall fires when `auto_recall=true`; skipped silently when false (default)
- [ ] AC-STORY-002-fac3fc6d: PostCompact reinject fires after compaction when `reinject_on_compact=true`; skipped when false (default)
- [ ] AC-STORY-002-2700c61a: PreCompact/Stop nudge fires unconditionally
- [ ] AC-STORY-002-f9cc59a4: Plugin reads the same `~/.memoryweb/config.json` option keys as the shell hooks
- [ ] AC-STORY-002-af4bac3b: Uses `~/.memoryweb/hook_state/` directory for context files so Stop/PostCompact pair works as in Claude Code
- [ ] AC-STORY-002-9da3d2c1: `TestOpencodePlugin_OrientNudgeEmitted`, `TestOpencodePlugin_AutoRecallInjected`, `TestOpencodePlugin_PreCompactNudge` pass
- [ ] AC-STORY-002-da768a78: `go test ./...` green
- [ ] AC-STORY-002-0729788d: `docs/memoryweb-skill.md` updated to document the OpenCode plugin; AGENTS.md updated
