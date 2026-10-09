# Schema Staleness Defence

> **COMPLETE** — all three fixes shipped prior to 2026-05-23.
> - Fix A: alias detection in `connect` for `from_node`/`to_node` — `TestConnect_RejectsLegacyFromNodeKey` and `TestConnect_BatchRejectsLegacyKeys` pass.
> - Fix B: `server_version` field in orient response — present in `tools/tools.go` line ~912.
> - Fix C: `notifications/tools/list_changed` emitted after `notifications/initialized` — in `main.go`.

Defends against silent data loss when an agent's context window holds a stale tool schema
after a parameter rename. Three fixes in priority order.

---

## Motivation

After the tier-2 vocabulary rename of `connect`'s `from_node`/`to_node` →
`from_memory`/`to_memory`, an agent mid-session sent the old parameter names. Go's
`json.Unmarshal` silently ignored the unknown fields; the call returned success but no
edge was created. Three factors compounded:

1. `json.Unmarshal` silently drops unknown keys — stale schema produces invisible data loss.
2. The agent's context window retained the old schema from before a server restart.
3. No freshness signal exists in session-start tools; no `notifications/tools/list_changed`
   is emitted.

Silent success on a mutation that does nothing is worse than an error — the agent continues,
believing the edge exists.

---

## Three fixes

### Fix A — Alias detection in `connect` handler (highest leverage)

In `tools/tools.go`, unmarshal the raw `connect` call into a `map[string]json.RawMessage`
before binding to the typed struct. If `from_node` or `to_node` keys are present, return
an error immediately:

```
"stale schema: 'from_node'/'to_node' are no longer valid — use 'from_memory'/'to_memory'.
 Your tool schema may be out of date. Reload tool definitions and retry."
```

This is the standard practice for any MCP tool that renames parameters: detect old names
explicitly rather than letting `json.Unmarshal` silently zero the fields.

The detection should be in the `handleAddEdge` function, applied to both single-call and
batch modes.

### Fix B — `server_version` field in `orient` response

Agents call `orient` at session start. Including the current server version in the orient
response gives agents a staleness signal: if the version has changed since their last
session, tool schemas may have changed.

In `tools/tools.go`, add `server_version string` to the `orient` response JSON. Populate
it from `main.Version` (already set by the release build).

The `orient` tool description should note: "server_version in the response indicates the
current binary version — if it differs from a prior session, call tools/list to refresh
your tool definitions."

### Fix C — Emit `notifications/tools/list_changed` after `notifications/initialized`

After `notifications/initialized` is received, emit a `notifications/tools/list_changed`
notification. This is the proper MCP mechanism to force clients to refresh their cached
tool list on reconnect.

In `main.go`, in the `notifications/initialized` branch of `dispatch`, write the
notification to stdout:

```json
{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}
```

(No `id` field — this is a notification, not a request.)

---

## Acceptance criteria

- `TestConnect_StaleParameterNames_ReturnsError`: call `connect` with `from_node`/`to_node`
  keys; assert the response is an error containing "stale schema" and "from_memory".
- `TestConnect_CorrectParameterNames_Succeeds`: call with `from_memory`/`to_memory`; assert
  success and edge created.
- `TestOrient_IncludesServerVersion`: orient response includes a `server_version` field
  (non-empty string).
- Fix A and B must not break any existing `connect` or `orient` tests.
- Fix C: the `notifications/tools/list_changed` output can be verified manually via MCP
  integration test (wire layer is not unit-tested per CLAUDE.md accepted gap).
- `go test ./...` green.

---

## Files

- `tools/tools.go` — `handleAddEdge` alias detection (Fix A), orient response struct (Fix B)
- `main.go` — `notifications/initialized` branch, emit list_changed (Fix C)
- `tools/tools_test.go` — new tests for Fix A and Fix B

## Note on recordari

Recordari has the same exposure: any parameter rename in a recordari tool will produce
identical silent failures. The alias detection pattern (Fix A) should be applied there
in the same change or as a follow-on story.

## References

- shared-surface node: `parameter-rename-causes-silent-f-e1fb61cf`
