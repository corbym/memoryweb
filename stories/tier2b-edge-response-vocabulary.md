# Story: Tier 2b — rename Edge response fields from `from_node`/`to_node` to `from_memory`/`to_memory`

**Discovered:** 2026-05-22  
**Domain:** memoryweb  
**Depends on:** stories/tier2-vocabulary-cleanup.md (must ship first)  
**Related nodes:** `tier-2-vocabulary-audit-property-1bff6521`, `connect-parameter-names-are-from-246e0b62`

---

## Background

After tier 2 (vocabulary-cleanup.md) ships, the tool surface will have an asymmetry:

- `connect` *input* parameters: `from_memory`, `to_memory`
- `Edge` struct *output* fields serialized in responses: `from_node`, `to_node`

Every tool that returns edges — `recall`, `orient`, `trace`, `why_connected`, `suggest_connections`, `visualise`, and the `connect` response itself — will still emit `from_node`/`to_node` in the JSON. An agent reading a `recall` response and then constructing a `disconnect` or follow-up `connect` call will see the old vocabulary in the response but be expected to use new vocabulary in the input.

This asymmetry is livable in the short term (agents read the connect schema to know what to send), but it should be resolved to keep the surface fully consistent.

---

## Scope

### In `db/db.go`

The `Edge` struct json tags:

```go
// Current
FromNode     string    `json:"from_node"`
ToNode       string    `json:"to_node"`

// After
FromNode     string    `json:"from_memory"`
ToNode       string    `json:"to_memory"`
```

No field renaming needed — only the json tags change. The Go field names `FromNode`/`ToNode` are internal and fine as-is.

### In `tools/tools_test.go`

Every test that unmarshals an `Edge` from a tool response and reads `from_node`/`to_node` must be updated. The inline struct definitions at lines ~2163–2164 and ~2198 use `json:"from_node"`/`json:"to_node"` — update to `json:"from_memory"`/`json:"to_memory"`.

Any test that asserts specific JSON keys in raw edge output strings must also be updated.

### In `db/db_test.go`

Any DB-layer test that reads `from_node`/`to_node` from edge responses. Scan for json struct tags or string assertions on these key names.

---

## Implementation order

1. Update `Edge` struct json tags in `db/db.go`.
2. Update all test struct definitions and assertions that reference `from_node`/`to_node` as response keys.
3. Run `go test ./...` — must be green.
4. Verify `orient`, `recall`, `trace` responses in a manual MCP session to confirm the rename is live.

---

## Note on the `EdgeInput` struct

`EdgeInput` in `db/db.go` is an internal batch-input struct — it has no json tags. It is populated from the handler struct (which after tier 2 uses `from_memory`/`to_memory` json tags). No change needed to `EdgeInput`.
