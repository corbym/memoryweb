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
  | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
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
