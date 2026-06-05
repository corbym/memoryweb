# generics: wave 1 — nullTimeToPtr, scanRows, inClause, filter, mapSlice

**Status:** COMPLETE

---

## Why

Five mechanical patterns repeat throughout `db/db.go` with no variation except the
types involved. Eliminating them with targeted generics reduces the cognitive load
of reading the file, removes copy-paste risk, and adds proper error propagation to
loops that currently swallow scan errors silently.

---

## Changes

### `db/util.go` — five new helpers

```go
// nullTimeToPtr returns &nt.Time when valid, nil otherwise.
func nullTimeToPtr(nt sql.NullTime) *time.Time

// scanRows iterates rows calling scan for each row, returns accumulated results.
// Caller closes rows.
func scanRows[T any](rows *sql.Rows, scan func(*sql.Rows) (T, error)) ([]T, error)

// inClause returns "?,?,?" and items as []any for SQL IN clauses.
// Returns ("", nil) for an empty slice.
func inClause[T any](items []T) (string, []any)

// filter returns a new slice containing only items for which keep returns true.
func filter[T any](items []T, keep func(T) bool) []T

// mapSlice transforms []T to []U by applying f to each element.
func mapSlice[T, U any](items []T, f func(T) U) []U
```

### `db/db.go` — package-level `scanNodeRow`

Add `scanNodeRow(*sql.Rows) (Node, error)` next to `scanNodeRows`. It scans
the standard 11-column node projection (same column order as `scanNodeRows`):
`id, label, description, why_matters, domain, created_at, updated_at,
occurred_at, archived_at, tags, decision_type`. Uses `nullTimeToPtr`.

Simplify `scanNodeRows` to: `return scanRows(rows, scanNodeRow)`.

Remove the `scanSingle` closure inside `FindDrift` — it is identical to
`scanNodeRow`. Call `scanNodeRow` directly in rules 2-5.

### Callsite updates

| Pattern replaced | Locations |
|---|---|
| `nullTimeToPtr` (2-line NullTime blocks) | `GetNode`, `searchNodesSemantic`, `bestMatch`, `GetNodeNeighbourhood` QueryRow, `FindDrift` rule 1 and rule 6, `GetStandingNodes`, `getSignificanceByNodeIDs`, `GetSignificance` ScoredNode scan, `scanNode` |
| `scanRows(rows, scanNodeRow)` / `scanNodeRows` | `Timeline`, `RecentChanges` inline loop, `FindDisconnected` inline loop, `GetNodeNeighbourhood` inline loop, `FindPossibleDuplicates` (with `filter` post-filter), `GetStandingNodes` (inline scanner with extra `inboundCount`), `FindDrift` rule 6 (inline scanner with extra `inboundCount`), `GetHistoryForMemoryID` inline loop |
| `inClause` | `searchNodesLike` (`ph` string), `collectEdges`, `materialisePath`, `RecentChanges` neighbourhood IN clause, `GetNodeNeighbourhood` `nArgs`+`eArgs`, `FindDrift` edge-enrich args, `getSignificanceByNodeIDs` `nodeArgs`+`structArgs` preamble |
| `mapSlice` | `extractNodes` body, `wrapNodes` body, `FindDrift` edge-enrich ID extraction, `collectEdges` ID extraction from `[]Node` |
| `filter` | `searchNodesSemantic` allowed-ID post-filter, `FindDrift` neighbourhood post-filter, `FindDrift` tag post-filter, `FindPossibleDuplicates` label post-filter |

---

## Acceptance criteria

- All existing tests pass without modification.
- No new tests required (pure refactor).
- `db/util.go` imports: `database/sql`, `strings`, `time`.
- `scanRows`, `inClause`, `filter`, `mapSlice` are generic (Go 1.18+).
- `nullTimeToPtr` and `scanNodeRow` are plain functions (no type parameter needed).
- The `scanSingle` closure in `FindDrift` is removed; rules 2-5 call `scanNodeRow`.
- `extractNodes` and `wrapNodes` still exist as package-level functions (callers outside `db` may use them); their bodies become single `mapSlice` calls.

---

## Related

- `generics-optional-field-update.md` (item 3 — UpdateNode/UpdateNodesBatch field blocks)
- `generics-json-batch-dispatch.md` (item 6 — batch/single JSON dispatch)
