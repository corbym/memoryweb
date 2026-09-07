#!/usr/bin/env bash
# memoryweb_lib.sh — shared helpers for memoryweb hook scripts.
# Source this file from hook scripts; do not execute directly.

# memoryweb_json_escape — JSON-safe-encode a string.
# Usage:  memoryweb_json_escape "raw string"
# Result: sets the global variable _esc to the encoded form.
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

memoryweb_json_escape() {
  local _dq='"'
  _esc="${1//$'\\'/\\\\}"
  _esc="${_esc//$_dq/\\\"}"
  _esc="${_esc//$'\n'/\\n}"
  _esc="${_esc//$'\t'/\\t}"
  _esc="${_esc//$'\r'/\\r}"
  _esc="${_esc//$'\b'/\\b}"
  _esc="${_esc//$'\f'/\\f}"
}
