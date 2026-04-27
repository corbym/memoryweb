#!/usr/bin/env bash
# memoryweb_save_hook.sh — Stop hook for Claude Code.
# Periodically prompts the model to file session findings to memoryweb.
set -euo pipefail

SAVE_INTERVAL="${MEMORYWEB_SAVE_INTERVAL:-15}"
STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
PROJECTS_DIR="${MEMORYWEB_PROJECTS_DIR:-${HOME}/.claude/projects}"
MEMORYWEB_DB="${MEMORYWEB_DB:-${HOME}/.memoryweb.db}"
DREAM_BIN="${MEMORYWEB_DREAM_BIN:-memoryweb}"

mkdir -p "${STATE_DIR}"

# Read the JSON payload from Claude Code.
json=$(cat)

# Extract session_id.
session_id=$(printf '%s' "${json}" \
  | grep -o '"session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 \
  | grep -o '"[^"]*"$' \
  | tr -d '"')

# Log every invocation.
printf '%s save_hook session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

if [ -z "${session_id}" ]; then
  printf '{"continue":true}\n'
  exit 0
fi

count_file="${STATE_DIR}/${session_id}.count"
saving_flag="${STATE_DIR}/${session_id}.saving"

# Get the saved message count from the last trigger.
last_saved=0
if [ -f "${count_file}" ]; then
  _v=$(cat "${count_file}" 2>/dev/null) && [ -n "${_v}" ] && last_saved="${_v}" || true
fi

# Count human messages in the session transcript.
current_count=0
transcript=$(find "${PROJECTS_DIR}" -name "${session_id}.jsonl" 2>/dev/null | head -1)
if [ -n "${transcript}" ] && [ -f "${transcript}" ]; then
  current_count=$(grep -c '"role"[[:space:]]*:[[:space:]]*"human"' "${transcript}" 2>/dev/null || true)
  current_count=${current_count:-0}
fi

delta=$((current_count - last_saved))

# Below threshold — allow without filing.
if [ "${delta}" -lt "${SAVE_INTERVAL}" ]; then
  printf '{"continue":true}\n'
  exit 0
fi

# Re-entry: model has just filed. Reset count and allow.
if [ -f "${saving_flag}" ]; then
  rm -f "${saving_flag}"
  printf '%d' "${current_count}" > "${count_file}"
  printf '{"continue":true}\n'
  exit 0
fi

# Threshold reached: block and request filing.
touch "${saving_flag}"

# Capture dream digest for context (best-effort; skipped silently if unavailable).
dream_digest=""
if [ -x "${DREAM_BIN}" ] || command -v "${DREAM_BIN}" >/dev/null 2>&1; then
  dream_digest=$("${DREAM_BIN}" dream --db "${MEMORYWEB_DB}" 2>/dev/null || true)
fi

# JSON-escape the digest: \  →  \\   then  "  →  \"   then  newlines  →  \n
_esc="${dream_digest//$'\\'/\\\\}"
_esc="${_esc//$'"'/\\\"}"
_esc="${_esc//$'\n'/\\n}"

if [ -n "${_esc}" ]; then
  printf '{"continue":false,"stopReason":"File significant findings from this session to memoryweb now.\\n\\n%s\\n\\nCall add_nodes with any decisions made, bugs found or fixed, design choices, or open questions. Add edges connecting related nodes. Use domain appropriate to the work. Focus on why_matters \xe2\x80\x94 skip anything you cannot explain the significance of. When done, continue."}\n' "${_esc}"
else
  printf '{"continue":false,"stopReason":"File significant findings from this session to memoryweb now. Call add_nodes with any decisions made, bugs found or fixed, design choices, or open questions. Add edges connecting related nodes. Use domain appropriate to the work. Focus on why_matters \xe2\x80\x94 skip anything you cannot explain the significance of. When done, continue."}\n'
fi

