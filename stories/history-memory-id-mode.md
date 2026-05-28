# history: memory_id mode

**Memoryweb-meta node:** `history-memory-id-mode-deferred--4fdf1767`

**Contract node:** `history-tool-memory-id-mode-cont-d4fceae4` (domain: `memoryweb-shared-surface`)

**Prior art:** `significance` memory_id mode (v1.21.0) — `neighbourhoodIDs` BFS helper
is reusable directly. `db.Store.Timeline` is the reference implementation for domain
mode; memory_id mode adds a node-set pre-filter before the same chronological query.

---

## Why

Domain-mode `history` answers "what happened in this domain, sorted by date". When a
caller already knows an anchor node — say, a core decision or component — the question
they actually want answered is "how did this workstream evolve?": the chronological
timeline scoped to the subgraph reachable from that anchor.

This completes the **workstream triad**:

| Tool | Question answered from an anchor |
|------|----------------------------------|
| `recall` / `search` | Find the anchor |
| `significance(memory_id)` | What is load-bearing in this workstream *right now*? |
| `history(memory_id)` | How did this workstream *evolve*? |

Without `history(memory_id)`, an agent can assess current health from an anchor but
cannot trace how it got there.

`history(memory_id)` is a small incremental story now that significance memory_id has
landed: `neighbourhoodIDs` already exists and can be reused unchanged.

---

## Changes

### 1. DB — `GetHistoryForMemoryID`

Add a new exported Store method:

```go
func (s *Store) GetHistoryForMemoryID(nodeID string, depth int, importantOnly bool, tags []string, from, to *time.Time, limit int) ([]Node, error)
```

Implementation:

1. Call `s.neighbourhoodIDs(nodeID, depth)` to get the node-set IDs and the anchor's
   domain. (This is the private BFS helper added in significance memory_id mode.)
2. If the node-set is empty, return `[]Node{}`.
3. Build a query equivalent to `Timeline` but scoped to `id IN (?, ?, ...)` instead of
   `domain = ?`. Apply all the same optional filters:
   - `important_only`: `AND occurred_at IS NOT NULL`
   - `tags`: same 4-binding whole-word LIKE pattern as `Timeline` and `GetSignificance`
   - `from` / `to`: `AND COALESCE(occurred_at, created_at) >= ?` / `<= ?`
   - `archived_at IS NULL` (mandatory)
4. Order by `COALESCE(occurred_at, created_at) ASC`.
5. Apply `LIMIT` if `limit > 0`.

Return type matches `Timeline`: `[]Node`.

### 2. Handler — parse `memory_id` from history args

The history handler already parses `domain`, `important_only`, `tags`, `from`, `to`,
`limit`. Add:

```go
MemoryID string `json:"memory_id"`
Depth    int    `json:"depth"`
```

Dispatch:

```go
if a.MemoryID != "" {
    if a.Depth <= 0 {
        a.Depth = 2
    }
    nodes, err = h.store.GetHistoryForMemoryID(a.MemoryID, a.Depth, a.ImportantOnly, tags, fromTime, toTime, a.Limit)
} else {
    nodes, err = h.store.Timeline(a.Domain, a.ImportantOnly, tags, fromTime, toTime, a.Limit)
}
```

`memory_id` takes precedence if both are supplied.

Update the domain-required guard: when `memory_id` is present, `domain` is not
required.

### 3. Schema — add `memory_id` and `depth` to history

```go
"memory_id": {Type: "string", Description: "Optional — scope the timeline to the neighbourhood of this memory (depth 2 by default, domain-clipped). Returns events in that workstream in chronological order. Takes precedence over domain if both are supplied."},
"depth":     {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2)."},
```

Remove `domain` from `Required` (or make the handler accept either `domain` or
`memory_id`).

### 4. Tool description addition

Add after the existing paragraph about `important_only`:

> Pass `memory_id` to scope the timeline to a single memory's neighbourhood (depth 2
> by default, domain-clipped) — answers "how did this workstream evolve?" from a known
> anchor. Combines with `important_only=true` for the decision spine of the workstream.

---

## Tests

- `TestHistory_MemoryIDMode_ReturnsChronological`: add several nodes connected to an
  anchor (some with `occurred_at`, some without); call history with the anchor's ID;
  verify results are ordered by `COALESCE(occurred_at, created_at) ASC`.
- `TestHistory_MemoryIDMode_DomainClipped`: add a node in a different domain connected
  to the anchor; verify it does not appear in results.
- `TestHistory_MemoryIDMode_ImportantOnly`: add nodes with and without `occurred_at`;
  call with `important_only=true`; verify only nodes with `occurred_at` are returned.
- `TestHistory_MemoryIDMode_TagsFilter`: add nodes with and without a tag in the
  neighbourhood; call with `tags="mytag"`; verify only tagged nodes are returned.
- `TestHistory_MemoryIDMode_TakesPrecedenceOverDomain`: supply both `memory_id` and
  `domain`; verify results are scoped to the neighbourhood, not the full domain.
- `TestHistory_MemoryIDMode_UnknownMemoryID`: supply an unknown `memory_id`; verify
  the result is an error (not an empty list).
- `TestHistory_MemoryIDMode_InSchema`: verify `memory_id` and `depth` are present as
  schema properties.

---

## Out of scope

- Pagination beyond `limit`. Same constraint as domain-mode `history`.
- `from` / `to` filtering interaction with `memory_id` mode is in scope (spec says it
  applies) but can be deferred to a follow-up if it adds significant complexity. Test
  with a basic case; skip edge cases.
- Cross-domain neighbourhood following. Domain-clipping is mandatory; this is not a
  future option.
