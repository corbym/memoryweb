# generics: optional field update blocks in UpdateNode / UpdateNodesBatch

**Status:** COMPLETE

---

## Why

`UpdateNode` and `UpdateNodesBatch` each repeat the same 4-line pattern once per
optional string field (label, description, why_matters, tags, decision_type —
five instances each, ~50 lines of duplication). The pattern:

```go
if label != nil {
    sets = append(sets, "label = ?")
    args = append(args, *label)
    if *label != cur.Label {
        changes = append(changes, fmt.Sprintf("label (was %q)", cur.Label))
    }
}
```

A generic `applyStringField` helper would eliminate this and make it trivial to
add new optional string fields in the future without risking a missed branch.

---

## Proposed helper

```go
// applyStringField appends a SQL SET clause and audit change entry for an
// optional string field update. If newVal is nil, nothing is appended.
func applyStringField(
    newVal *string, current string,
    col, fieldName string,
    sets, changes *[]string, args *[]interface{},
) {
    if newVal == nil {
        return
    }
    *sets = append(*sets, col+" = ?")
    *args = append(*args, *newVal)
    if *newVal != current {
        *changes = append(*changes, fmt.Sprintf("%s (was %q)", fieldName, current))
    }
}
```

`occurred_at` and `decision_type` are handled separately (they have extra
validation / formatting logic) and should NOT be moved into this helper.

The helper is not actually generic in the Go sense — it's a plain function
over `string`. Consider whether a generic `applyField[T comparable]` is worth
the abstraction; only if a non-string optional field (e.g. `*time.Time`) is
added in future does the type parameter pay off.

---

## Changes

### `db/db.go` — `UpdateNode`

Replace the five `if label != nil { ... }` blocks for label, description,
why_matters, tags, and decision_type with `applyStringField` calls. Keep the
`occurred_at` and `decision_type` (with validation switch) blocks unchanged
or fold `decision_type` in if validation is extracted first.

### `db/db.go` — `UpdateNodesBatch`

Same replacement in the batch path, which mirrors UpdateNode's field blocks.

---

## Acceptance criteria

- All existing tests pass without modification.
- `UpdateNode` and `UpdateNodesBatch` each shrink by ~15-20 lines.
- `applyStringField` lives in `db/util.go`.
- `occurred_at` update logic is unchanged.

---

## Related

- `generics-wave1.md` (wave 1 — utility generics)
