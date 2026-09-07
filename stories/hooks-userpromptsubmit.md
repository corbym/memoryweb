# hooks: UserPromptSubmit hook — orient nudge + auto-recall

Adds `memoryweb_userpromptsubmit_hook.sh` — a UserPromptSubmit hook with two
independent behaviours:

1. **Orient nudge** (`session_orient_enabled`): detects whether `orient()` was called
   earlier in the session via transcript-read; if not, injects a brief nudge via
   `additionalContext`. Also writes a per-session context file for PostCompact reinject.
2. **Auto-recall** (`auto_recall`): extracts keywords from the current user message,
   runs `memoryweb search`, and injects the top results as `additionalContext` so the
   agent has relevant memories without needing to call search explicitly.

Also adds a `memoryweb search` CLI subcommand (required by auto-recall).

Closes shared-surface decisions
`decision-userpromptsubmit-orient-nudge-hook-uses-transcript-read-to-detect-orient-call-no-flag-file-no-network-call-86c973f6`,
`decision-userpromptsubmit-writes-rcd-orient-ctx-session-id-json-so-postcompact-can-call-orient-domain-x-topic-y-instead-of-bare-orient-35ae7c98`,
and shared-surface option
`option-user-enabled-auto-recall-hook-per-message-context-injection-via-userpromptsubmit-inspired-by-mnema-pattern`.

---

## Motivation

### Orient nudge

memoryweb's standing rule requires agents to call `orient()` at session start. Today
the rule lives only in the skill document — there is no mechanical enforcement. When an
agent skips orient, the session proceeds without domain context and filing decisions are
made blind.

Recordari solved this (shared-surface 2026-09-06) with a UserPromptSubmit hook that
reads the session transcript to check for an orient call. If none is found, it injects
a brief nudge in `additionalContext`. The detection is stateless: the transcript is the
state — no flag file, no network call.

The hook also writes a small context file
(`~/.memoryweb/hook_state/mw_orient_ctx_<session_id>.json`) recording the domain seen
in the first orient call, so the PostCompact hook (see `stories/hooks-postcompact-reinject.md`)
can call `orient(domain=X)` after context compaction rather than a bare orient.

Controlled by the `session_orient_enabled` option (see `stories/hooks-options-cli.md`).

### Auto-recall

The core weakness of any hook-backed memory system is that retrieval is
prompt-discipline only: if the agent doesn't call `search`, relevant memories are never
surfaced. Recordari STORY-315 solved this with a per-prompt auto-recall hook that
injects relevant memories without the agent having to ask. For Recordari this called the
hosted API; for memoryweb the local binary can do the same via `memoryweb search`.

On each user prompt the hook extracts the first 8 words of the message (sufficient for
keyword matching), runs `memoryweb search --lean --limit 3`, and injects the results as
`additionalContext`. The agent receives the most relevant memories before it responds,
with no explicit retrieval step. Gracefully skipped when search returns nothing, the
binary is unavailable, or `auto_recall=false`.

Controlled by the `auto_recall` option (default: false — opt-in, same as Recordari, to
avoid injecting noise for users who haven't set up Ollama or who prefer explicit retrieval).

---

## Changes

### 1. `hooks/memoryweb_userpromptsubmit_hook.sh`

```bash
#!/usr/bin/env bash
# memoryweb_userpromptsubmit_hook.sh — UserPromptSubmit hook for Claude Code.
# Two behaviours: orient nudge (session_orient_enabled) + auto-recall (auto_recall).
set -euo pipefail

source "$(dirname "$0")/memoryweb_lib.sh"

MEMORYWEB_BIN="${MEMORYWEB_BIN:-memoryweb}"
MEMORYWEB_DB="${MEMORYWEB_DB:-${HOME}/.memoryweb.db}"
STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
PROJECTS_DIR="${MEMORYWEB_PROJECTS_DIR:-${HOME}/.claude/projects}"
mkdir -p "${STATE_DIR}"

json=$(cat)

session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 | grep -o '"[^"]*"$' | tr -d '"')
session_id=$(printf '%s' "${session_id}" | tr -cd 'a-zA-Z0-9_-')

printf '%s userpromptsubmit_hook session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

additional_parts=""

# ── Orient nudge ──────────────────────────────────────────────────────────────
if memoryweb_option_enabled "session_orient_enabled" "false" && [ -n "${session_id}" ]; then
  ctx_file="${STATE_DIR}/mw_orient_ctx_${session_id}.json"

  orient_found=false
  domain_seen=""
  transcript=$(find "${PROJECTS_DIR}" -name "${session_id}.jsonl" 2>/dev/null | head -1)
  if [ -n "${transcript}" ] && [ -f "${transcript}" ]; then
    if grep -q '"name"[[:space:]]*:[[:space:]]*"orient"' "${transcript}" 2>/dev/null; then
      orient_found=true
      domain_seen=$(grep '"name"[[:space:]]*:[[:space:]]*"orient"' "${transcript}" 2>/dev/null \
        | tail -1 \
        | grep -o '"domain"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | grep -o '"[^"]*"$' | tr -d '"' || true)
    fi
  fi

  # Write context file on first orient detection (PostCompact will read it).
  if "${orient_found}" && [ ! -f "${ctx_file}" ]; then
    printf '{"domain":"%s"}\n' "${domain_seen}" > "${ctx_file}"
  fi

  if ! "${orient_found}"; then
    additional_parts="memoryweb: orient() has not been called yet this session. Call orient() (and orient(domain=X) for the relevant domain) before answering or filing anything."
  fi
fi

# ── Auto-recall ───────────────────────────────────────────────────────────────
if memoryweb_option_enabled "auto_recall" "false" \
   && command -v "${MEMORYWEB_BIN}" >/dev/null 2>&1; then
  # Extract user message from payload (field: "message").
  user_msg=$(printf '%s' "${json}" \
    | grep -o '"message"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
  # Take first 8 words as search query.
  query=$(printf '%s' "${user_msg}" | tr -s ' \t' '\n' | head -8 | tr '\n' ' ' \
    | sed 's/[[:space:]]*$//' || true)

  if [ -n "${query}" ]; then
    recall_results=$("${MEMORYWEB_BIN}" search \
      --db "${MEMORYWEB_DB}" \
      --query "${query}" \
      --lean \
      --limit 3 2>/dev/null || true)
    if [ -n "${recall_results}" ]; then
      recall_section="memoryweb relevant memories:\n${recall_results}"
      if [ -n "${additional_parts}" ]; then
        additional_parts="${additional_parts}\n\n${recall_section}"
      else
        additional_parts="${recall_section}"
      fi
    fi
  fi
fi

# ── Emit ──────────────────────────────────────────────────────────────────────
if [ -n "${additional_parts}" ]; then
  memoryweb_json_escape "${additional_parts}"
  printf '{"continue":true,"additionalContext":"%s"}\n' "${_esc}"
else
  printf '{"continue":true}\n'
fi
```

**Note on payload field name:** The `"message"` field is expected from Claude Code's
UserPromptSubmit payload. Verify against Claude Code hook documentation before
implementation; adjust the field extraction if the actual key differs.

### 2. `main.go` — `memoryweb search` CLI subcommand

Auto-recall calls `memoryweb search --db <path> --query <q> --lean --limit N`.
Add `case "search":` to the `os.Args[1]` switch:

```go
case "search":
    searchCmd()
    return
```

Add to help text:
```
  search         Search nodes and print lean results (for scripting / hooks)
```

`searchCmd` / `runSearchCmd`:

```go
func searchCmd() {
    flags := flag.NewFlagSet("search", flag.ExitOnError)
    dbFlag   := flags.String("db",    "", "database path (default ~/.memoryweb.db)")
    query    := flags.String("query", "", "search terms")
    domain   := flags.String("domain","", "restrict to domain")
    limit    := flags.Int("limit",   10,  "max results")
    lean     := flags.Bool("lean",   false, "compact one-line output")
    flags.Parse(os.Args[2:])
    if err := runSearchCmd(os.Stdout, *dbFlag, *query, *domain, *limit, *lean); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}
```

`runSearchCmd` opens the DB, calls `store.SearchNodes`, and:
- Without `--lean`: prints one JSON object per result.
- With `--lean`: prints one compact line per result —
  `[id] label — why_matters excerpt (domain, node_kind)  dist` —
  same format as the MCP tool's compact multi-result rendering, with distance
  appended when non-zero (mirrors the fix in `stories/search-semantic-distance-compact.md`).

Exit 0 with empty output when no results (not an error — hooks must handle no output gracefully).

Tests in `main_test.go`:
- `TestSearchCmd_LeанOutput` — two nodes, `--lean` → two compact lines.
- `TestSearchCmd_NoResults` — query matches nothing → exit 0, empty stdout.
- `TestSearchCmd_MissingQuery` — `--query ""` → error.

### 3. `hooks/memoryweb_lib.sh` — add `memoryweb_read_option` and `memoryweb_option_enabled`

Add two helpers to the shared library. All hooks source `memoryweb_lib.sh`, so the helpers
are immediately available everywhere.

```bash
# memoryweb_read_option — read a value from ~/.memoryweb/config.json.
# Usage:  memoryweb_read_option "key" "default"
# Result: sets the global variable _opt to the value, or to default if absent/unreadable.
memoryweb_read_option() {
  local _key="$1" _default="$2"
  local _cfg="${HOME}/.memoryweb/config.json"
  _opt="${_default}"
  if [ -f "${_cfg}" ]; then
    local _raw
    _raw=$(grep -o "\"${_key}\"[[:space:]]*:[[:space:]]*[^,}]*" "${_cfg}" 2>/dev/null \
      | head -1 \
      | sed 's/.*:[[:space:]]*//' \
      | tr -d ' "' || true)
    [ -n "${_raw}" ] && _opt="${_raw}"
  fi
}

# memoryweb_option_enabled — returns 0 (true) when an option reads as truthy.
# Usage:  if memoryweb_option_enabled "key" "default"; then ...
memoryweb_option_enabled() {
  memoryweb_read_option "$1" "$2"
  case "${_opt}" in
    true|1|on) return 0 ;;
    *) return 1 ;;
  esac
}
```

### 4. `main.go` — register UserPromptSubmit in `runSetup`

After the existing `Stop` and `PreCompact` lines:

```go
userpromptsubmitHook := filepath.Join(hooksDir, "memoryweb_userpromptsubmit_hook.sh")
```

Add to the existence/executable check slice, then register:

```go
hooks["UserPromptSubmit"] = setupUpsertCommand(
    setupToSlice(hooks["UserPromptSubmit"]),
    userpromptsubmitHook,
    makeEntry(userpromptsubmitHook),
)
```

### 5. `hooks/hooks_test.go` — tests

**Orient nudge:**
- `TestUserPromptSubmitHook_OrientNotCalled` — transcript absent → output contains `"additionalContext"` and "orient".
- `TestUserPromptSubmitHook_OrientAlreadyCalled` — transcript has `"name":"orient"` → output is `{"continue":true}` with no nudge.
- `TestUserPromptSubmitHook_WritesContextFile` — orient detected → `mw_orient_ctx_<session_id>.json` created in STATE_DIR.
- `TestUserPromptSubmitHook_OrientOptionDisabled` — `session_orient_enabled=false` → no nudge even when orient not found.
- `TestUserPromptSubmitHook_NoSessionID` — missing session_id → `{"continue":true}`, no ctx file.

**Auto-recall:**
- `TestUserPromptSubmitHook_AutoRecallEnabled` — `auto_recall=true` in config, `MEMORYWEB_BIN` stub returns two lean lines for query → output contains `"additionalContext"` with "memoryweb relevant memories".
- `TestUserPromptSubmitHook_AutoRecallDisabled` — `auto_recall=false` (default) → no recall section in output.
- `TestUserPromptSubmitHook_AutoRecallNoResults` — binary returns empty output → `additionalContext` absent (or contains only orient nudge if that also fired).
- `TestUserPromptSubmitHook_BothNudgeAndRecall` — orient not called + auto_recall=true with results → `additionalContext` contains both the orient nudge and the recall section.

---

## Acceptance criteria

- UserPromptSubmit hook registered by `memoryweb setup`; idempotent on second run.
- `memoryweb search --lean --limit 3 --query "..."` prints compact lines and exits 0.
- `memoryweb search` with empty query → error.
- `memoryweb search` with no matching nodes → empty stdout, exit 0.
- Orient nudge fires when `session_orient_enabled=true` (opt-in; default false) and no orient in transcript.
- Auto-recall fires when `auto_recall=true` and binary is available; skipped silently when `auto_recall=false` (default).
- Both can fire in the same invocation; their output is merged into one `additionalContext`.
- All 9 hook tests + 3 search CLI tests pass.
- Before merging: update `docs/memoryweb-skill.md` to document the UserPromptSubmit hook, the `session_orient_enabled` and `auto_recall` options, and the `memoryweb search` CLI subcommand. Update `AGENTS.md` hook surface table.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `hooks/memoryweb_userpromptsubmit_hook.sh` | New — orient nudge + auto-recall |
| `hooks/memoryweb_lib.sh` | Add `memoryweb_read_option`, `memoryweb_option_enabled` |
| `main.go` | `searchCmd`/`runSearchCmd`; register UserPromptSubmit in `runSetup`; dispatch + help |
| `main_test.go` | `TestSearchCmd_*` (3 cases) |
| `hooks/hooks_test.go` | 9 new test cases |

---

## References

- Shared-surface decision: `decision-userpromptsubmit-orient-nudge-hook-uses-transcript-read-to-detect-orient-call-no-flag-file-no-network-call-86c973f6`
- Shared-surface decision: `decision-userpromptsubmit-writes-rcd-orient-ctx-session-id-json-so-postcompact-can-call-orient-domain-x-topic-y-instead-of-bare-orient-35ae7c98`
- Shared-surface option: `option-user-enabled-auto-recall-hook-per-message-context-injection-via-userpromptsubmit-inspired-by-mnema-pattern`
- Recordari STORY-315: `story-315-done-userpromptsubmit-hook-per-prompt-auto-recall-with-opt-in-via-auto-recall-user-option`
- orient-on-start standing rule: `orient-on-the-story-s-topics-bef-1526220c` (memoryweb-meta)
- Related: `stories/hooks-postcompact-reinject.md` (reads the context file this hook writes)
- Related: `stories/hooks-options-cli.md` (defines `session_orient_enabled` and `auto_recall`)
- Related: `stories/search-semantic-distance-compact.md` (defines the lean compact line format used by `memoryweb search --lean`)
