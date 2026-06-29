# Strict decode — reject unknown tool input fields

**Status:** COMPLETE

**Shared-surface node:** `cross-cutting-gap-tool-input-silently-drops-unknown-fields-fixed-in-recordari-confirmed-present-in-memoryweb-263130a5`

**Recordari:** FIXED 2026-06-28 (`decodeParams` + `strictDecode` + table-driven negative test).

---

## Why

All memoryweb tool handlers decode arguments with plain `json.Unmarshal`, which silently
drops unknown top-level fields. Concrete incident: an agent called `orient` with
`{domains: [...]}` (array); orient only reads `domain` (string), so the unknown key was
dropped, `domain` stayed empty, and orient returned the no-domain bootstrap snapshot
with no error. The agent got a confusing full dump and no signal it had passed a bad
param.

Silent input-validation drop is a cross-product agent-experience bug: agents get
plausible-looking wrong results with no error, wasting turns and eroding trust.
Recordari is hardened; memoryweb is the open follow-up.

---

## Changes

### `tools/util.go` — shared decode wrapper

Port Recordari's pattern:

```go
func decodeParams(raw json.RawMessage, dst any, toolName string) error
```

Uses `json.Decoder` with `DisallowUnknownFields()`. On failure, return a validation
error naming the offending field and instructing the caller to refresh schema via
`tools/list`.

Add tool-specific hints where agents commonly confuse parameters — at minimum for
`orient`:

> orient takes a single `domain` string, not `domains`; call once per domain (or see
> `stories/orient-domains-array.md` when multi-domain ships).

### Migrate all tool handlers

grep shows ~13 tool files using plain `json.Unmarshal(args, &a)`. Replace every
read-path and write-path handler with `decodeParams`. Priority: `orient`, `search`,
`audit`, `remember`, `connect`, `revise`.

### Regression test

Add `tools/strict_decode_test.go` (or extend `tools/tools_test.go`): table-driven test
that calls `CallTool` for every registered tool with a bogus top-level field and
asserts `IsError` with a message naming the field.

---

## Acceptance criteria

- Every tool handler uses `decodeParams` (or equivalent `DisallowUnknownFields` path).
- `orient` with `{domains: ["x"]}` returns validation error naming `domains`, not bootstrap.
- `remember` with unknown field returns validation error, not silent drop.
- Table-driven test covers all tools in `ListTools()`.
- `go test ./...` green.

---

## Files (expected)

- `tools/util.go` — `decodeParams`, optional `strictDecode` helper
- All `tools/*.go` handler files — migrate unmarshaling
- `tools/strict_decode_test.go` — negative test table

---

## References

- Recordari: `internal/mcp/strict_decode_all_tools_test.go`, `decodeParams` in MCP layer
- Sibling gap: `stories/node-kind-filter.md` (same "two servers drift" class)
- Shared-surface: `parameter-rename-causes-silent-f-e1fb61cf` (same failure mode)
