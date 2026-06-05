# generics: JSON single-vs-batch dispatch in tools

**Status:** COMPLETE

---

## Why

`addNode`, `addEdge`, and `updateNode` in `tools/tools.go` each repeat the same
"peek for an `items` field, route to batch handler or fall through to single
handler" boilerplate:

```go
var peek struct {
    Items json.RawMessage `json:"items"`
}
if err := json.Unmarshal(args, &peek); err != nil {
    return nil, err
}
if len(peek.Items) > 0 && string(peek.Items) != "null" {
    return h.addNodesBatch(peek.Items)
}
// ... single-item unmarshal follows
```

This pattern appears three times with different inner types and handlers. A
generic dispatcher would make adding future batch-capable tools a one-liner.

---

## Proposed helper

```go
// dispatchBatch peeks the "items" field of args.
// If present and non-null, it calls batchFn with the raw items array.
// Otherwise it calls singleFn with the full args.
// Returns (result, wasBatch, error).
func dispatchBatch[T any](
    args json.RawMessage,
    singleFn func(json.RawMessage) (T, error),
    batchFn  func(json.RawMessage) (T, error),
) (T, bool, error)
```

Callers then look like:
```go
result, _, err := dispatchBatch(args, h.addNodeSingle, h.addNodesBatch)
```

---

## Tradeoffs

The three handlers all return `*ToolResult` so `T = *ToolResult` works cleanly.
The main risk is that the peek logic is straightforward enough that abstracting
it makes the code harder to scan at a glance. Assess whether the savings (3×
removed peek blocks) outweigh the indirection before implementing.

The `processRelatedTo` union-type decoder (string ID vs object with relationship)
is a different pattern and should NOT be folded into this helper.

---

## Acceptance criteria

- All existing tool tests pass.
- `dispatchBatch` lives in `tools/tools.go` or a new `tools/util.go`.
- Three peek-and-route blocks replaced with `dispatchBatch` calls.
- `processRelatedTo` is unchanged.

---

## Related

- `generics-wave1.md` (wave 1 — db utility generics)
