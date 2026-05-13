#!/usr/bin/env bash
# memoryweb_lib.sh — shared helpers for memoryweb hook scripts.
# Source this file from hook scripts; do not execute directly.

# memoryweb_json_escape — JSON-safe-encode a string.
# Usage:  memoryweb_json_escape "raw string"
# Result: sets the global variable _esc to the encoded form.
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
