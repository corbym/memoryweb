# revise: add transient field

**Shared-surface node:** `revise-tool-transient-field-miss-ae1c95cc`

---

## Why

The `revise` tool exposes `label`, `description`, `why_matters`, `tags`, and `occurred_at`
as updatable fields but not `transient`. Once a node is created, there is no way to
change its `transient` flag via MCP. The only workaround is `forget` + `remember`, which
destroys all existing edge connections.

Two legitimate needs:

- **Promote out of transient**: a ticket note filed as `transient=true` turns into a
  permanent decision. The agent should be able to clear the flag without re-filing.
- **Mark transient after the fact**: a node filed without the flag is later recognised as
  short-lived. The agent should be able to set `transient=true` without losing its
  connections.

The `forget` + `remember` workaround is destructive and unacceptable for well-connected
nodes.

---

## Changes

### 1. Single mode schema — add `transient` property

In `tools/tools.go`, add to the `revise` single-mode `Properties` map:

```go
"transient": {Type: "boolean", Description: "Set to true for short-lived knowledge; set to false to promote a transient memory to permanent. Optional — omit to leave the current value unchanged."},
```

### 2. Batch items schema — add `transient` to items JSON

In the `items` property `Items` raw JSON, add `"transient":{"type":"boolean"}` alongside
the existing fields:

```json
{"type":"object","properties":{"id":{"type":"string"},"label":{"type":"string"},"description":{"type":"string"},"why_matters":{"type":"string"},"tags":{"type":"string"},"occurred_at":{"type":"string"},"transient":{"type":"boolean"}},"required":["id"]}
```

### 3. Handler — wire the field

In the `handleRevise` single-mode args struct, add:

```go
Transient *bool `json:"transient"`
```

Use a pointer so `nil` means "omit" and `false` is a valid update. Pass it through to
the `UpdateNode` call. Check how `UpdateNode` handles partial updates — if it currently
only updates non-zero fields, add explicit `transient` handling.

In the `updateNodesBatch` handler, same treatment per item.

### 4. DB layer

Confirm `UpdateNode` in `db/db.go` can update `transient`. If the current SQL does not
include `transient` in the `SET` clause, add it conditionally when the caller provides
the field.

---

## Acceptance criteria

- `revise` single mode with `transient: false` on a node that was `transient: true`
  clears the flag; subsequent `recall` shows `transient: false`.
- `revise` single mode with `transient: true` on a non-transient node sets it;
  subsequent `recall` shows `transient: true`.
- Omitting `transient` from a `revise` call leaves the existing value unchanged.
- Batch mode supports the same field with the same semantics.
- A `revise` call that changes `transient` preserves all existing edge connections.
- `TestRevise_TransientUpdatable` covers these cases in `tools/tools_test.go`.
