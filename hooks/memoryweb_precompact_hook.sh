#!/usr/bin/env bash
# memoryweb_precompact_hook.sh — PreCompact hook for Claude Code.
# Prompts the model to file everything important before context compaction.
set -euo pipefail

STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"

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
printf '%s precompact_hook session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

if [ -z "${session_id}" ]; then
  printf '{"continue":true}\n'
  exit 0
fi

compacting_flag="${STATE_DIR}/${session_id}.compacting"

# Re-entry: model has just filed. Allow compaction to proceed.
if [ -f "${compacting_flag}" ]; then
  rm -f "${compacting_flag}"
  printf '{"continue":true}\n'
  exit 0
fi

# First run: block and request a thorough filing pass.
touch "${compacting_flag}"
printf '{"continue":false,"stopReason":"Context is about to compact. This is your last chance to file anything important. Call remember_all for every significant decision, finding, or open question from this session that is not already in memoryweb. Add edges. Be thorough \xe2\x80\x94 anything not filed now may be lost. When done, continue."}\n'

