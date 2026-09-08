#!/usr/bin/env bash
# memoryweb_subagent_start_hook.sh — SubagentStart hook for Claude Code.
# Orients the sub-agent into the parent session's memoryweb scope (domain +
# topic from mw_orient_ctx_<parent>.json) and injects a memoryweb dream digest
# into the sub-agent's starting context.
set -euo pipefail

# shellcheck source=memoryweb_lib.sh
source "$(dirname "$0")/memoryweb_lib.sh"

MEMORYWEB_BIN="${MEMORYWEB_BIN:-memoryweb}"
MEMORYWEB_DB="${MEMORYWEB_DB:-${HOME}/.memoryweb.db}"
STATE_DIR="${MEMORYWEB_HOOK_STATE_DIR:-${HOME}/.memoryweb/hook_state}"
mkdir -p "${STATE_DIR}"

# Consume stdin (Claude Code sends a JSON payload; not needed here).
json=$(cat)

# Parent session discovery. SubagentStart payloads carry the parent session's
# transcript_path (the subagent's own transcript lives under subagents/, not in
# this field), so its basename is the session id that owns the orient ctx file.
# Prefer an explicit parent_session_id when Claude Code starts sending one.
parent_session_id=$(printf '%s' "${json}" \
  | grep -o '"parent_session_id"[[:space:]]*:[[:space:]]*"[^"]*"' \
  | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
if [ -z "${parent_session_id}" ]; then
  transcript_path=$(printf '%s' "${json}" \
    | grep -o '"transcript_path"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
  if [ -n "${transcript_path}" ]; then
    parent_session_id=$(basename "${transcript_path}" .jsonl 2>/dev/null || true)
  fi
fi
parent_session_id=$(printf '%s' "${parent_session_id}" | tr -cd 'a-zA-Z0-9_-')

printf '%s subagent_start_hook parent_session=%s\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${parent_session_id:-unknown}" \
  >> "${STATE_DIR}/hook.log"

# Respect subagent_orient_enabled option (default: disabled — opt-in).
if ! memoryweb_option_enabled "subagent_orient_enabled" "false"; then
  printf '{"continue":true}\n'
  exit 0
fi

# Orient hint from the parent's orient context file (written by the
# UserPromptSubmit hook), so the sub-agent orients into the same domain+topic
# the parent session is working in instead of a bare cross-domain orient().
orient_hint=""
if [ -n "${parent_session_id}" ]; then
  ctx_file="${STATE_DIR}/mw_orient_ctx_${parent_session_id}.json"
  if [ -f "${ctx_file}" ]; then
    domain=$(grep -o '"domain"[[:space:]]*:[[:space:]]*"[^"]*"' "${ctx_file}" 2>/dev/null \
      | grep -o '"[^"]*"$' | tr -d '"' || true)
    topic=$(grep -o '"topic"[[:space:]]*:[[:space:]]*"[^"]*"' "${ctx_file}" 2>/dev/null \
      | grep -o '"[^"]*"$' | tr -d '"' || true)
    if [ -n "${domain}" ]; then
      orient_hint="orient(domain=\"${domain}\""
      [ -n "${topic}" ] && orient_hint="${orient_hint}, topic=\"${topic}\""
      orient_hint="${orient_hint})"
    fi
  fi
fi

dream_digest=""
if command -v "${MEMORYWEB_BIN}" >/dev/null 2>&1; then
  dream_digest=$("${MEMORYWEB_BIN}" dream --db "${MEMORYWEB_DB}" 2>/dev/null || true)
fi

additional=""
if [ -n "${orient_hint}" ]; then
  additional="This sub-agent session inherits the parent session's memoryweb scope. Call ${orient_hint} before working to load the domain context."
fi
if [ -n "${dream_digest}" ]; then
  if [ -n "${additional}" ]; then
    additional="${additional}\n\nmemoryweb context for this sub-agent session:\n\n${dream_digest}"
  else
    additional="memoryweb context for this sub-agent session:\n\n${dream_digest}"
  fi
fi

if [ -n "${additional}" ]; then
  memoryweb_json_escape "${additional}"
  printf '{"hookSpecificOutput":{"hookEventName":"SubagentStart","additionalContext":"%s"}}\n' "${_esc}"
else
  printf '{"continue":true}\n'
fi