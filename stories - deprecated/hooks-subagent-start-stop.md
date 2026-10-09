# hooks: SubagentStart + SubagentStop hook surface expansion

**Status:** COMPLETE — (commit dc3d251)

Adds two new hook scripts — `memoryweb_subagent_start_hook.sh` and
`memoryweb_subagent_stop_hook.sh` — and registers them in `memoryweb setup`.
Mirrors Recordari STORY-151's SubagentStart/SubagentStop coverage. Closes the
shared-surface goal `memoryweb-hook-surface-should-expand-to-subagentstart-subagentstop-mirroring-recordari-story-151-6a645963`.

---

## Motivation

memoryweb currently hooks `Stop` (periodic every-15-message filing trigger) and
`PreCompact` (thorough filing before context compaction). When a main agent delegates
work to a sub-agent, neither hook fires for the sub-agent's session: nodes filed
during a delegated task are silently orphaned, and the sub-agent starts cold with no
orientation about what has already been decided.

Recordari STORY-151 solved this for the hosted product by adding:

- **SubagentStart** — injects a concise domain digest into the sub-agent's context so
  it starts oriented, mechanically enforcing the "orient on topics before
  implementation begins" standing rule without requiring prompt discipline.
- **SubagentStop** — prompts the sub-agent to connect any orphaned nodes before it
  exits, so the main graph stays coherent across delegated tasks.

Platform coverage (confirmed full-tier): Claude Code, Codex, Cursor, VS Code/GitHub
Copilot. Not viable on Claude Desktop or Claude web.

Filed: 2026-09-02. Shared-surface goal:
`memoryweb-hook-surface-should-expand-to-subagentstart-subagentstop-mirroring-recordari-story-151-6a645963`.

---

## Changes

### 1. `hooks/memoryweb_subagent_start_hook.sh`

New hook script registered under the `SubagentStart` Claude Code hook event.

**Behaviour:**
1. Sources `memoryweb_lib.sh` for `memoryweb_json_escape`.
2. Runs `memoryweb dream --db "$MEMORYWEB_DB"` (best-effort; skipped silently if
   unavailable or if the binary is not on `PATH`).
3. Returns `{"continue": true, "additionalContext": "<escaped_dream_output>"}` when
   dream output is non-empty — this injects the digest into the sub-agent's initial
   context before its first tool call.
4. Returns `{"continue": true}` with no `additionalContext` when dream output is
   empty or unavailable (graceful no-op).

```bash
#!/usr/bin/env bash
# memoryweb_subagent_start_hook.sh — SubagentStart hook for Claude Code.
# Injects a memoryweb dream digest into the sub-agent's starting context.
set -euo pipefail

source "$(dirname "$0")/memoryweb_lib.sh"

MEMORYWEB_BIN="${MEMORYWEB_BIN:-memoryweb}"
MEMORYWEB_DB="${MEMORYWEB_DB:-${HOME}/.memoryweb.db}"
STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
mkdir -p "${STATE_DIR}"

# Consume stdin (Claude Code sends a JSON payload; not needed here).
json=$(cat)

# Log invocation.
printf '%s subagent_start_hook\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  >> "${STATE_DIR}/hook.log"

# Respect subagent_orient_enabled option (default: disabled — opt-in).
if ! memoryweb_option_enabled "subagent_orient_enabled" "false"; then
  printf '{"continue":true}\n'
  exit 0
fi

dream_digest=""
if command -v "${MEMORYWEB_BIN}" >/dev/null 2>&1; then
  dream_digest=$("${MEMORYWEB_BIN}" dream --db "${MEMORYWEB_DB}" 2>/dev/null || true)
fi

memoryweb_json_escape "${dream_digest}"

if [ -n "${_esc}" ]; then
  printf '{"continue":true,"additionalContext":"memoryweb context for this sub-agent session:\\n\\n%s"}\n' "${_esc}"
else
  printf '{"continue":true}\n'
fi
```

### 2. `hooks/memoryweb_subagent_stop_hook.sh`

New hook script registered under the `SubagentStop` Claude Code hook event.

**Behaviour:**
1. Sources `memoryweb_lib.sh`.
2. Uses a per-session flag file (keyed on `session_id` from the JSON payload) to
   implement the same re-entry guard as the existing Stop and PreCompact hooks: block
   on first fire to prompt filing, clear the flag and allow on second fire.
3. On first fire: returns `{"continue": false, "stopReason": "<prompt>"}` instructing
   the sub-agent to check for orphaned nodes via the `audit` tool and connect any
   before exiting.
4. On second fire (after filing): clears the flag, returns `{"continue": true}`.

```bash
#!/usr/bin/env bash
# memoryweb_subagent_stop_hook.sh — SubagentStop hook for Claude Code.
# Prompts the sub-agent to connect orphaned nodes before it exits.
set -euo pipefail

source "$(dirname "$0")/memoryweb_lib.sh"

STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
mkdir -p "${STATE_DIR}"

json=$(cat)

session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 \
  | grep -o '"[^"]*"$' \
  | tr -d '"')
session_id=$(printf '%s' "${session_id}" | tr -cd 'a-zA-Z0-9_-')

printf '%s subagent_stop_hook session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

# Respect subagent_audit_enabled option (default: disabled — opt-in).
if ! memoryweb_option_enabled "subagent_audit_enabled" "false"; then
  printf '{"continue":true}\n'
  exit 0
fi

if [ -z "${session_id}" ]; then
  printf '{"continue":true}\n'
  exit 0
fi

subagent_stop_flag="${STATE_DIR}/${session_id}.subagent_stop"

if [ -f "${subagent_stop_flag}" ]; then
  rm -f "${subagent_stop_flag}"
  printf '{"continue":true}\n'
  exit 0
fi

touch "${subagent_stop_flag}"

printf '{"continue":false,"stopReason":"Before ending: call audit(mode=orphans) and connect any nodes that have no edges yet. Orphaned nodes filed during this sub-agent session are invisible to search and significance — connecting them is required before you exit. When done, continue."}\n'
```

### 3. `main.go` — register both hooks in `runSetup`

In `runSetup`, after the existing `Stop` and `PreCompact` lines:

```go
subagentStartHook := filepath.Join(hooksDir, "memoryweb_subagent_start_hook.sh")
subagentStopHook  := filepath.Join(hooksDir, "memoryweb_subagent_stop_hook.sh")
```

Add both to the script existence/executable checks, then register:

```go
hooks["SubagentStart"] = setupUpsertCommand(setupToSlice(hooks["SubagentStart"]), subagentStartHook, makeEntry(subagentStartHook))
hooks["SubagentStop"]  = setupUpsertCommand(setupToSlice(hooks["SubagentStop"]),  subagentStopHook,  makeEntry(subagentStopHook))
```

### 4. `hooks/hooks_test.go` — add tests for both new scripts

Follow the existing pattern in `hooks_test.go`:

- `TestSubagentStartHook_NoMemoryweb` — when `MEMORYWEB_BIN` points to a non-existent
  binary, script must output `{"continue":true}` with no `additionalContext`.
- `TestSubagentStartHook_WithDreamDigest` — when `MEMORYWEB_BIN` points to a stub that
  prints "== memoryweb dream ==\nsome content", output must contain
  `"additionalContext"` and the escaped content.
- `TestSubagentStopHook_FirstFire` — first invocation with a valid `session_id` must
  output `{"continue":false,...}` containing "audit" and "orphans".
- `TestSubagentStopHook_SecondFire` — second invocation with the same `session_id`
  must output `{"continue":true}`.
- `TestSubagentStopHook_NoSessionID` — missing `session_id` must output
  `{"continue":true}` without creating a flag file.

---

## Acceptance criteria

- `memoryweb setup` installs four hook entries (Stop, PreCompact, SubagentStart,
  SubagentStop) into `settings.json`; running it twice leaves exactly four
  entries (idempotency — covered by the existing setup-idempotency story).
- `TestSubagentStartHook_NoMemoryweb`: output is `{"continue":true}`, no
  `additionalContext` key.
- `TestSubagentStartHook_WithDreamDigest`: output contains `"additionalContext"` with
  the dream content.
- `TestSubagentStopHook_FirstFire`: output contains `"continue":false` and the word
  "orphans".
- `TestSubagentStopHook_SecondFire`: output is `{"continue":true}` and the flag file
  is deleted.
- `TestSubagentStopHook_NoSessionID`: output is `{"continue":true}`, no flag file
  created.
- SubagentStart fires only when `subagent_orient_enabled=true` (default false — opt-in); digest injection skipped when disabled.
- SubagentStop fires only when `subagent_audit_enabled=true` (default false — opt-in); orphan prompt skipped when disabled.
- Before merging: update `docs/memoryweb-skill.md` to document the SubagentStart and SubagentStop hooks and the `subagent_orient_enabled` / `subagent_audit_enabled` options. Update `AGENTS.md` hook surface table.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `hooks/memoryweb_subagent_start_hook.sh` | New — SubagentStart hook |
| `hooks/memoryweb_subagent_stop_hook.sh` | New — SubagentStop hook |
| `main.go` | Register both hooks in `runSetup` (3 lines each: path, check, upsert) |
| `hooks/hooks_test.go` | 5 new test cases |

---

## References

- Shared-surface goal: `memoryweb-hook-surface-should-expand-to-subagentstart-subagentstop-mirroring-recordari-story-151-6a645963`
- Platform tier finding: `platform-tier-correction-cursor-and-vs-code-github-copilot-are-full-tier-for-userpromptsubmit-hook-not-degraded-as-previously-noted-3b454423`
- orient-on-start standing rule: `orient-on-the-story-s-topics-bef-1526220c` (memoryweb-meta)
- Related: `stories/setup-idempotency.md` (idempotency contract covers the new entries)
- Related: `stories/hooks-remember-all-retirement.md` (hook surface precedent)
