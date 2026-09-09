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

# ── Orient scope capture + nudge ──────────────────────────────────────────────
# Capture the session's orient scope (last oriented domain + topic) whenever any
# consumer of mw_orient_ctx_<session>.json is enabled — the UserPromptSubmit
# nudge, PostCompact reinjection, or SubagentStart inheritance — so a quiet
# capture config (no per-prompt nudge) still powers the other two hooks.
capture=false
memoryweb_option_enabled "session_orient_enabled" "false" && capture=true
memoryweb_option_enabled "reinject_on_compact" "false" && capture=true
memoryweb_option_enabled "subagent_orient_enabled" "false" && capture=true

if [ "${capture}" = true ] && [ -n "${session_id}" ]; then
  ctx_file="${STATE_DIR}/mw_orient_ctx_${session_id}.json"

  orient_found=false
  domain_seen=""
  topic_seen=""
  transcript=$(find "${PROJECTS_DIR}" -name "${session_id}.jsonl" 2>/dev/null | head -1)
  if [ -n "${transcript}" ] && [ -f "${transcript}" ]; then
  # MCP tools are serialised in the transcript with an mcp__<server>__ prefix,
  # so a bare "name":"orient" never matches a real session. Accept an optional
  # mcp__<server>__ prefix (e.g. mcp__memoryweb__orient).
  name_re='"name"[[:space:]]*:[[:space:]]*"(mcp__[A-Za-z0-9_-]+__)?orient"'
  if grep -Eq "${name_re}" "${transcript}" 2>/dev/null; then
    orient_found=true
    # Records other than the orient call itself (attachments, tool results) can
    # mention the tool name without carrying a domain. Anchor on the LAST
    # orient line that actually has a non-empty domain field — "the last
    # domain the session oriented to" — and take its topic if present.
    orient_line=$(grep -E "${name_re}" "${transcript}" 2>/dev/null \
      | grep -E '"domain"[[:space:]]*:[[:space:]]*"[^"]+"' \
      | tail -1 || true)
    if [ -n "${orient_line}" ]; then
      domain_seen=$(printf '%s' "${orient_line}" \
        | grep -o '"domain"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | grep -o '"[^"]*"$' | tr -d '"' || true)
      topic_seen=$(printf '%s' "${orient_line}" \
        | grep -o '"topic"[[:space:]]*:[[:space:]]*"[^"]*"' \
        | head -1 | grep -o '"[^"]*"$' | tr -d '"' || true)
    fi
  fi
  fi

  # Write (or refresh) the context file whenever a domain-carrying orient is
  # detected, so PostCompact and SubagentStart re-orient into the domain and
  # topic the session currently works in. Never persist an empty domain.
  if "${orient_found}" && [ -n "${domain_seen}" ]; then
    # The values are raw JSON text off a transcript line (grepped, not parsed),
    # so they may carry characters that are invalid unescaped in JSON — a quote
    # truncates the grep match and leaves a trailing backslash. Escape before
    # embedding so no later JSON consumer reads a corrupted ctx file.
    memoryweb_json_escape "${domain_seen}"
    escaped_domain="${_esc}"
    if [ -n "${topic_seen}" ]; then
      memoryweb_json_escape "${topic_seen}"
      escaped_topic="${_esc}"
      printf '{"domain":"%s","topic":"%s"}\n' "${escaped_domain}" "${escaped_topic}" > "${ctx_file}"
    else
      printf '{"domain":"%s"}\n' "${escaped_domain}" > "${ctx_file}"
    fi
  fi

  if memoryweb_option_enabled "session_orient_enabled" "false" && ! "${orient_found}"; then
    additional_parts="memoryweb: orient() has not been called yet this session. Call orient() (and orient(domain=X) for the relevant domain) before answering or filing anything."
  fi
fi

# ── Auto-recall ───────────────────────────────────────────────────────────────
if memoryweb_option_enabled "auto_recall" "false" \
   && command -v "${MEMORYWEB_BIN}" >/dev/null 2>&1; then
  # Extract user message from payload (field: "message"). jq gives an exact
  # JSON decode (a message may legally start with, or embed, escaped quotes).
  # The jq-less fallback regex + sed handles embedded \" and \\ but cannot
  # survive control-char escapes — a documented limitation, not silent data loss.
  if command -v jq >/dev/null 2>&1; then
    user_msg=$(printf '%s' "${json}" \
      | jq -r '.message | if type == "string" then . else empty end' 2>/dev/null || true)
  else
    user_msg=$(printf '%s' "${json}" \
      | grep -oE '"message"[[:space:]]*:[[:space:]]*"((\\.|[^"\\])*)"' | head -1 \
      | sed -E 's/^"message"[[:space:]]*:[[:space:]]*"//; s/"$//; s/\\\\/\\/g; s/\\"/"/g' || true)
  fi
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
