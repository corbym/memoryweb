#!/usr/bin/env bash
# memoryweb_userpromptsubmit_hook.sh — UserPromptSubmit hook for Claude Code.
# Two behaviours: orient nudge (session_orient_enabled) + auto-recall (auto_recall).
set -euo pipefail

# shellcheck source=memoryweb_lib.sh
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
  # MCP tools are serialised in the transcript with an mcp__<server>__ prefix,
  # so a bare "name":"orient" never matches a real session. Accept an optional
  # mcp__<server>__ prefix (e.g. mcp__memoryweb__orient).
  name_re='"name"[[:space:]]*:[[:space:]]*"(mcp__[A-Za-z0-9_-]+__)?orient"'
  if grep -Eq "${name_re}" "${transcript}" 2>/dev/null; then
    orient_found=true
    # Records other than the orient call itself (attachments, tool results) can
    # mention the tool name without carrying a domain. Extract the domain from
    # the LAST orient line that actually has a non-empty domain field — "the
    # last domain the session oriented to".
    domain_seen=$(grep -E "${name_re}" "${transcript}" 2>/dev/null \
      | grep -E '"domain"[[:space:]]*:[[:space:]]*"[^"]+"' \
      | tail -1 \
      | grep -o '"domain"[[:space:]]*:[[:space:]]*"[^"]*"' \
      | grep -o '"[^"]*"$' | tr -d '"' || true)
  fi
  fi

  # Write (or refresh) the context file whenever a domain-carrying orient is
  # detected, so PostCompact re-orients into the domain the session currently
  # works in. Never persist an empty domain.
  if "${orient_found}" && [ -n "${domain_seen}" ]; then
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
# Claude Code silently discards a bare top-level additionalContext; it must be
# nested under hookSpecificOutput with the matching hookEventName.
if [ -n "${additional_parts}" ]; then
  memoryweb_json_escape "${additional_parts}"
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"%s"}}\n' "${_esc}"
else
  printf '{"continue":true}\n'
fi
