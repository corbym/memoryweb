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
  | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
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
