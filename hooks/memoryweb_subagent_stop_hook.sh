#!/usr/bin/env bash
# memoryweb_subagent_stop_hook.sh — SubagentStop hook for Claude Code.
# Prompts the sub-agent to connect orphaned nodes before it exits.
set -euo pipefail

# shellcheck source=memoryweb_lib.sh
source "$(dirname "$0")/memoryweb_lib.sh"

STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
mkdir -p "${STATE_DIR}"

json=$(cat)

session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 \
  | grep -o '"[^"]*"$' \
  | tr -d '"' || true)
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

printf '{"decision":"block","reason":"Before ending: call audit(mode=orphans) and connect any nodes that have no edges yet. Orphaned nodes filed during this sub-agent session are invisible to search and significance -- connecting them is required before you exit. When done, stop again."}\n'
