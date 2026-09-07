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
