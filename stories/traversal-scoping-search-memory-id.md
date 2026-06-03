# search: memory_id scoping parameter

**Status:** PENDING

**Shared-surface node:** `traversal-tool-scoping-consisten-c691512d`

---

## Why

`history` and `significance` both accept `memory_id` to scope results to a BFS
neighbourhood. `search` does not. The use case:

- `search(query="architecture", memory_id=X)` — "find memories about architecture
  that are topologically related to node X"

This is especially useful when the same terminology appears in multiple unrelated
workstreams. Scoping the search to a neighbourhood disambiguates without needing to
know the exact domain or label.

Note: `search` already covers `tags` via the query parameter (the LIKE search scans
the `tags` column). No dedicated `tags` parameter is needed.

---

## Acceptance criteria

- `search` accepts `memory_id` (string) — when supplied, restricts candidate nodes
  to the depth-2 BFS neighbourhood of the named node before running the query.
- Semantic search (when Ollama is available) and LIKE fallback both respect the
  neighbourhood filter.
- The existing `domain`, `limit`, `exact` parameters continue to work alongside
  `memory_id`. If both `domain` and `memory_id` are supplied, both constraints apply
  (neighbourhood AND domain; though neighbourhood is already domain-clipped by
  `neighbourhoodIDs`, this is a no-op in practice).
- Existing behaviour (no `memory_id`) is unchanged.

---

## Changes

### 1. db/db.go — SearchNodes / SearchNodesExact signatures

```go
func (s *Store) SearchNodes(query, domain string, limit int, memoryID string) (*SearchResult, error)
func (s *Store) SearchNodesExact(query, domain string, limit int, memoryID string) (*SearchResult, error)
```

Add `memoryID string` parameter. When non-empty:

1. Call `s.neighbourhoodIDs(memoryID, 2)` to get the BFS ID set.
2. Pass the ID set as an additional filter to `searchNodesLike` and
   `searchNodesSemantic`.

### 2. db/db.go — searchNodesLike / searchNodesSemantic

Add `allowedIDs []string` parameter. When `allowedIDs` is non-empty, add
`AND id IN (?,?,...)` to the WHERE clause. For semantic search, filter the returned
results to only those whose ID is in the allowed set before applying the distance
threshold (or add the IN clause to the SQL query).

### 3. db/db.go — orientWithTopic

`orientWithTopic` calls `SearchNodes` internally. Update the call to pass empty
`memoryID` so the signature change doesn't break it.

### 4. tools/tools.go — `searchNodes` handler

Add `MemoryID string \`json:"memory_id"\`` to the args struct. Pass it to
`SearchNodes` / `SearchNodesExact`.

### 5. tools/tools.go — `search` input schema

Add to Properties:

```go
"memory_id": {Type: "string", Description: "Anchor node ID. Restricts search candidates to the depth-2 neighbourhood of this node. Useful for disambiguating the same term across workstreams."},
```

---

## Complexity note

The semantic search path requires filtering returned embedding results to the
neighbourhood set. The simplest approach is a post-filter: run the vector search,
then discard any result whose ID is not in `allowedIDs`. This may return fewer
results than `limit` but avoids the complexity of injecting an IN clause into the
virtual table query. Document the `truncated` field will not accurately reflect this
filtering — a separate pass may return more results up to `limit` if needed (out of
scope for this story).

---

## Tests

### db/db_test.go

- `TestSearchNodesLike_MemoryID_NeighbourhoodOnly` — 3 nodes: anchor, neighbour
  (tagged "arch"), unrelated (also tagged "arch"); search query="arch" with
  memoryID=anchor; verify unrelated node excluded
- `TestSearchNodes_MemoryID_EmptyReturnsFallback` — memoryID="" behaves identically
  to existing behaviour

### tools/tools_test.go

- `TestSearch_MemoryID_ScopesResults` — anchor + neighbour matching query + unrelated
  matching query; search with memory_id=anchor.ID, verify unrelated excluded
- `TestSearch_ExistingBehaviourUnchanged` — no memory_id, same results as before
