# hooks: port memoryweb hooks to OpenCode as an event-bus plugin

**Status:** TODO

Ports the memoryweb hook suite (periodic save sweep, subagent orphan audit,
compaction filing) to OpenCode as a native event-bus plugin. Currently all hook
enforcement exists only for Claude Code sessions. This closes the platform gap and
extends the same memory-integrity guarantee to OpenCode users.

Closes shared-surface goal
`port-memoryweb-hooks-to-opencode-as-an-event-bus-plugin-periodic-save-sweep-subagent-orphan-audit-compaction-filing-509688a5`.

---

## Motivation

memoryweb's Claude Code hooks enforce three memory-integrity behaviours:

1. **PreCompact / Stop** — prompts the agent to file knowledge before context
   compaction or session end.
2. **PostCompact** — reinjects orient context so the agent doesn't start the
   compacted session cold.
3. **UserPromptSubmit** — orient-nudge (detects missing orient and injects a
   reminder) + auto-recall (injects relevant memories per prompt).

All three are implemented as shell scripts registered in `.claude/settings.json`
(see `stories/hooks-options-cli.md`, `stories/hooks-postcompact-reinject.md`,
`stories/hooks-userpromptsubmit.md`). They exist only for Claude Code.

OpenCode has an event-bus plugin system (`opencode.plugin.ts` / `opencode.plugin.js`)
that exposes the same lifecycle events under different names. Implementing a
parallel plugin closes the gap for the growing share of memoryweb users who work
in OpenCode sessions.

---

## Design

### OpenCode event mapping

| memoryweb / Claude Code hook | OpenCode event equivalent |
|------------------------------|---------------------------|
| `PreCompact` | `context:before_compact` (or equivalent compact lifecycle event) |
| `Stop` (session end) | `session:end` |
| `PostCompact` | `context:after_compact` |
| `UserPromptSubmit` | `prompt:before_submit` |

> **Pre-implementation step:** verify the exact event names and payload shapes
> against the OpenCode plugin API documentation before writing any code. Event
> names above are illustrative; use the authoritative names.

### Plugin structure

Implement as a single TypeScript (or JavaScript) file
`hooks/memoryweb_opencode_plugin.ts` that:

1. Registers for each lifecycle event.
2. Reads the memoryweb config file (`~/.memoryweb/config.json`) to check opt-in
   flags — same keys as the Claude Code hooks: `session_orient_enabled`,
   `auto_recall`, `reinject_on_compact`.
3. Calls `memoryweb` CLI subcommands for each behaviour:
   - Orient nudge: transcript inspection equivalent or flag-file fallback (OpenCode
     may not expose the session transcript as a JSONL file; adapt the detection
     strategy accordingly).
   - Auto-recall: `memoryweb search --lean --limit 3 --query "<first-8-words>"`.
   - PostCompact reinject: `memoryweb dream --db <db>` + inject domain hint.
   - PreCompact / Stop: inject "file everything important before this session ends"
     nudge in the event response.
4. Uses the same `~/.memoryweb/hook_state/` directory for context files
   (`mw_orient_ctx_<session_id>.json`) so the Stop/PostCompact pair works the
   same way as in Claude Code.

### Setup integration

`memoryweb setup` (the idempotent setup command, see
`stories/setup-idempotency.md`) should detect whether OpenCode is installed and,
if so, offer to install the plugin. Add a `--opencode` flag or an interactive
prompt. Idempotency rules are the same as for Claude Code hook registration.

---

## Changes

### 1. `hooks/memoryweb_opencode_plugin.ts`

New file. Implements all four event handlers. TypeScript preferred for OpenCode
compatibility; compile to JS if the plugin system requires it. Reads memoryweb
options via a thin wrapper that mirrors `memoryweb_read_option` / `memoryweb_option_enabled`
from `hooks/memoryweb_lib.sh`.

### 2. `main.go` — extend `runSetup` for OpenCode

Add an `--opencode` flag to the `setup` subcommand. When passed (or when
`--all-platforms` is used), install `memoryweb_opencode_plugin.ts` into the
OpenCode plugin directory (detected via `OPENCODE_PLUGINS_DIR` env var or
`~/.opencode/plugins/`).

### 3. `hooks/hooks_test.go` — plugin smoke tests

At minimum:
- `TestOpencodePlugin_OrientNudgeEmitted` — simulate a `prompt:before_submit`
  event with no orient in session state → nudge output.
- `TestOpencodePlugin_AutoRecallInjected` — `auto_recall=true`, stub binary returns
  results → recall section in output.
- `TestOpencodePlugin_PreCompactNudge` — `context:before_compact` event → nudge
  injected.

---

## Acceptance criteria

- `memoryweb setup --opencode` installs the plugin into the OpenCode plugin
  directory; idempotent on second run.
- Orient nudge fires on the first prompt of a session when `session_orient_enabled=true`
  and no orient call is detected.
- Auto-recall fires when `auto_recall=true`; skipped silently when false (default).
- PostCompact reinject fires after compaction when `reinject_on_compact=true`; skipped
  when false (default).
- PreCompact/Stop nudge fires unconditionally (like the Claude Code counterpart).
- Plugin reads the same `~/.memoryweb/config.json` option keys as the shell hooks.
- All new plugin tests pass.
- `go test ./...` green (Go-side setup changes only; plugin itself may have its
  own test harness).
- Before merging: update `docs/memoryweb-skill.md` to document the OpenCode plugin
  and add a note to AGENTS.md.

---

## Pre-implementation design note

The orient-nudge detection strategy differs between platforms:

- **Claude Code**: reads the session `.jsonl` transcript file from
  `~/.claude/projects/<session_id>.jsonl`, grep for `"name":"orient"`.
- **OpenCode**: may expose session history via a plugin API call, or may not
  expose it at all. If no transcript read is available, fall back to a flag-file
  approach: write `~/.memoryweb/hook_state/mw_orient_seen_<session_id>` when an
  orient call is detected (requires the plugin to also intercept tool responses),
  or accept that nudge will fire every prompt until orient is called (less precise
  but safe).

Decide the strategy before implementation and record it as a decision node in
memoryweb-meta.

---

## Files

| File | Change |
|------|--------|
| `hooks/memoryweb_opencode_plugin.ts` | New — all four event handlers |
| `main.go` | `runSetup` — `--opencode` flag, plugin install |
| `hooks/hooks_test.go` | 3+ new plugin smoke tests |

---

## References

- Shared-surface goal: `port-memoryweb-hooks-to-opencode-as-an-event-bus-plugin-periodic-save-sweep-subagent-orphan-audit-compaction-filing-509688a5`
- Related option (portable Stop hook): `memoryweb-periodic-stop-hook-every-15-turns-is-portable-to-recordari-as-a-mid-session-orphan-audit-trigger-ba574484`
- Claude Code counterparts: `stories/hooks-userpromptsubmit.md`, `stories/hooks-postcompact-reinject.md`, `stories/hooks-options-cli.md`
- Setup story: `stories/setup-idempotency.md`
