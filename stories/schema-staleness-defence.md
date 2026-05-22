# Story: Schema staleness defence — silent parameter failures and session drift

**Discovered:** 2026-05-22  
**Domain:** memoryweb  
**Trigger:** Agent mid-session failed to create edges after tier2 deployed,
because it was sending `from_node`/`to_node` against a binary that now expected
`from_memory`/`to_memory`.

---

## Root causes

### 1. Go `json.Unmarshal` silently ignores unknown fields

When the `connect` handler unmarshals its arguments, Go silently drops any key
not present in the target struct. An agent sending `from_node`/`to_node` (old
schema) receives a success response — the tool returns `{"id": "...", ...}` with
a zeroed-out edge — and never learns the edge was not created.

This is the highest-severity failure mode because:
- No error is surfaced to the caller
- The operation appears to succeed
- The data loss is invisible until the agent calls `recall` or `orient`

### 2. Agent context window retains stale schema summaries

MCP clients typically call `tools/list` once at session start and cache the
result. In long sessions, the agent's context window may have absorbed a schema
summary (from an early `tools/list` call) that is now stale. When the server
binary is updated mid-session (or between sessions in a long conversation), the
agent continues using old parameter names.

### 3. No visible freshness signal in session-start tools

`orient` is called at the start of every session, but its response contains no
server version. If the binary has been updated, the agent has no signal that
the tool surface may have changed and should be re-fetched.

### 4. No MCP tool-change notification

The MCP spec supports `notifications/tools/list_changed` — a server-sent
notification that tells the client to re-fetch `tools/list`. memoryweb never
emits this. When a new binary session starts after `notifications/initialized`,
the server could send this notification to prompt clients to refresh.

---

## Fixes (in order of impact)

### Fix 1 — Alias detection in `connect` handler (HIGH)

In `addEdge` and `addEdgesBatch`, inspect the raw JSON for the presence of
`from_node` or `to_node` keys (the retired names). If found — and the new names
are absent — return a `ToolResult{IsError: true}` with a message like:

> "Unknown parameter 'from_node'. The connect tool uses 'from_memory' and
> 'to_memory'. Call tools/list to refresh your schema."

This turns a silent zero-value failure into an actionable error. It costs
roughly 10 lines per handler and requires no client changes.

**Scope:** `tools/tools.go` — `addEdge` and `addEdgesBatch`.

**Tests:**
- `TestConnect_RejectsLegacyFromNodeKey` — single mode with `from_node` key returns IsError with the migration hint
- `TestConnect_RejectsLegacyToNodeKey` — same for `to_node`
- `TestConnect_BatchRejectsLegacyKeys` — batch mode item with `from_node`/`to_node` returns IsError

### Fix 2 — Server version in `orient` response (MEDIUM)

Add a `server_version` field to the `orient` response JSON. The `Handler`
already holds `h.version`. Agents call `orient` at every session start; seeing
a version bump is a hint that the tool schema may have changed and
`tools/list` should be re-fetched.

**Scope:** `tools/tools.go` — `summariseDomain` response struct.

**Tests:**
- `TestOrient_ResponseIncludesServerVersion` — orient response contains
  `server_version` field matching the version the handler was created with

### Fix 3 — Emit `notifications/tools/list_changed` after session init (LOW)

After receiving `notifications/initialized` from the client, emit a
`notifications/tools/list_changed` notification on stdout. This is the correct
MCP mechanism for signalling schema changes. Most MCP clients (including Claude
Desktop) will respond by re-calling `tools/list`, ensuring the agent's in-context
schema is always current.

Cost: 4 lines in `main.go`. The `notifications/initialized` handler already
exists; it just `continue`s.

**Scope:** `main.go` — `notifications/initialized` handler.

**Tests:**
Wire-layer tests are not in scope (per accepted gap in CLAUDE.md). Manual
verification: Claude Desktop should show refreshed tool names after a server
restart.

---

## Implementation order

1. Write failing tests for Fix 1 and Fix 2.
2. Implement Fix 1 (alias detection in `addEdge` + `addEdgesBatch`).
3. Implement Fix 2 (server_version in `summariseDomain` response).
4. Implement Fix 3 (`notifications/tools/list_changed` in main.go).
5. Run `go test ./...` — must be green.
6. Commit, push, tag.

---

## Note on recordari

Recordari almost certainly has the same vulnerability. Any parameter rename in
a recordari tool (e.g. renaming `from_id` → `from_memory` in a future cleanup)
would produce identical silent failures if Go's json.Unmarshal is used there
too. The alias detection pattern in Fix 1 should be documented as a standard
practice for any MCP tool that renames parameters.

---

## Out of scope

- General parameter alias infrastructure (a generic `renamed_from` annotation in
  `Property` that auto-detects stale usage across all tools). That's a larger
  framework change. Fix 1 is surgical and sufficient for the known regression.
- Detecting staleness for tools other than `connect`. The `connect` rename was
  the only one that produced a silent success (because the empty fields were
  silently accepted by `AddEdge` and an empty-field edge returned). Other
  parameter renames in `remember`, `revise` etc. fail with a DB error or return
  empty results rather than silently succeeding.
