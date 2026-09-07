# hooks: PostCompact context reinject hook

Adds `memoryweb_postcompact_hook.sh` — a PostCompact hook that fires *after* context
compaction and injects a brief orient-domain nudge so the agent does not start the
compacted session cold. Reads the context file written by the UserPromptSubmit hook
(see `stories/hooks-userpromptsubmit.md`) to call `orient(domain=X)` with the same
domain used earlier in the session.

---

## Motivation

memoryweb's existing PreCompact hook (`memoryweb_precompact_hook.sh`) blocks before
compaction to prompt the agent to file everything important. That covers knowledge
preservation. It does not cover context restoration: after compaction the agent has
lost the session's working context — which domain it was in, what it had just decided —
and must re-orient from scratch.

Recordari STORY-317 (`reinject_on_compact`) solved this with a PostCompact hook that
fires *after* the compactor runs and injects context via `additionalContext`. For
memoryweb, the parallel is: run `memoryweb dream --db <db>` to get the current digest
and include the domain from the UserPromptSubmit context file so the agent can call
`orient(domain=X)` immediately rather than calling bare `orient()` and re-reading
everything.

Controlled by the `reinject_on_compact` option (see `stories/hooks-options-cli.md`).

---

## Changes

### 1. `hooks/memoryweb_postcompact_hook.sh`

```bash
#!/usr/bin/env bash
# memoryweb_postcompact_hook.sh — PostCompact hook for Claude Code.
# Reinjects orient context after context compaction.
set -euo pipefail

source "$(dirname "$0")/memoryweb_lib.sh"

MEMORYWEB_BIN="${MEMORYWEB_BIN:-memoryweb}"
MEMORYWEB_DB="${MEMORYWEB_DB:-${HOME}/.memoryweb.db}"
STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
mkdir -p "${STATE_DIR}"

json=$(cat)

session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 | grep -o '"[^"]*"$' | tr -d '"')
session_id=$(printf '%s' "${session_id}" | tr -cd 'a-zA-Z0-9_-')

printf '%s postcompact_hook session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

# Respect reinject_on_compact option (default: disabled — opt-in).
if ! memoryweb_option_enabled "reinject_on_compact" "false"; then
  printf '{"continue":true}\n'
  exit 0
fi

if [ -z "${session_id}" ]; then
  printf '{"continue":true}\n'
  exit 0
fi

# Read domain from context file written by the UserPromptSubmit hook.
ctx_file="${STATE_DIR}/mw_orient_ctx_${session_id}.json"
domain=""
if [ -f "${ctx_file}" ]; then
  domain=$(grep -o '"domain"[[:space:]]*:[[:space:]]*"[^"]*"' "${ctx_file}" 2>/dev/null \
    | grep -o '"[^"]*"$' | tr -d '"' || true)
fi

# Build orient call hint.
orient_hint="orient()"
[ -n "${domain}" ] && orient_hint="orient(domain=\"${domain}\")"

# Capture dream digest for context (best-effort).
dream_digest=""
if command -v "${MEMORYWEB_BIN}" >/dev/null 2>&1; then
  dream_digest=$("${MEMORYWEB_BIN}" dream --db "${MEMORYWEB_DB}" 2>/dev/null || true)
fi

memoryweb_json_escape "${dream_digest}"

if [ -n "${_esc}" ]; then
  additional="Context was just compacted. Call ${orient_hint} to restore your working context before continuing.\\n\\n${_esc}"
else
  additional="Context was just compacted. Call ${orient_hint} to restore your working context before continuing."
fi

memoryweb_json_escape "${additional}"
printf '{"continue":true,"additionalContext":"%s"}\n' "${_esc}"
```

### 2. `main.go` — register PostCompact in `runSetup`

After the existing hook registrations:

```go
postcompactHook := filepath.Join(hooksDir, "memoryweb_postcompact_hook.sh")
```

Add to existence/executable check, then register:

```go
hooks["PostCompact"] = setupUpsertCommand(
    setupToSlice(hooks["PostCompact"]),
    postcompactHook,
    makeEntry(postcompactHook),
)
```

### 3. `hooks/hooks_test.go` — tests

- `TestPostCompactHook_WithContextFile` — ctx file present with a domain → output contains `"additionalContext"` naming the domain and "orient".
- `TestPostCompactHook_NoContextFile` — ctx file absent → output contains `"additionalContext"` with generic orient hint.
- `TestPostCompactHook_OptionDisabled` — `reinject_on_compact=false` in config → output is `{"continue":true}` with no `additionalContext`.
- `TestPostCompactHook_NoSessionID` — missing session_id → output is `{"continue":true}`.

---

## Acceptance criteria

- PostCompact hook is registered by `memoryweb setup`; idempotent on second run.
- `TestPostCompactHook_WithContextFile`: `additionalContext` names the domain from the file.
- `TestPostCompactHook_NoContextFile`: `additionalContext` present with generic orient hint.
- `TestPostCompactHook_OptionDisabled`: output is `{"continue":true}` only (default — opt-in).
- Before merging: update `docs/memoryweb-skill.md` to document the PostCompact hook and the `reinject_on_compact` option. Update `AGENTS.md` hook surface table.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `hooks/memoryweb_postcompact_hook.sh` | New |
| `main.go` | Register PostCompact in `runSetup` |
| `hooks/hooks_test.go` | 4 new test cases |

---

## References

- Shared-surface decision: `decision-userpromptsubmit-writes-rcd-orient-ctx-session-id-json-so-postcompact-can-call-orient-domain-x-topic-y-instead-of-bare-orient-35ae7c98`
- Recordari STORY-317: `reinject_on_compact` (PostCompact hook)
- Related: `stories/hooks-userpromptsubmit.md` (writes the context file this hook reads)
- Related: `stories/hooks-options-cli.md` (defines `reinject_on_compact`)
