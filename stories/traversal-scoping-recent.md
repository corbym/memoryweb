# recent: tags + memory_id scoping parameters

**Status:** PENDING

**Shared-surface node:** `traversal-tool-scoping-consisten-c691512d`

---

## Why

`history` and `significance` both accept `tags` and `memory_id` for subgraph scoping.
`recent` has neither. The use cases are coherent:

- `recent(tags="TDD")` — "show me recent changes to nodes in my TDD workstream"
- `recent(memory_id=X)` — "show me recent activity in the neighbourhood of node X"

Without these parameters, `recent` is the only traversal tool that cannot be scoped
to a workstream, creating asymmetry that agents must work around.

---

## Acceptance criteria

- `recent` accepts `tags` (comma-separated string) — when supplied, only returns
  nodes where any tag matches; semantics are OR (same as history and significance).
- `recent` accepts `memory_id` (string) — when supplied, scopes results to the BFS
  neighbourhood of the named node (depth 2 by default), then orders by `updated_at` DESC.
- `tags` and `memory_id` are independent — supplying both applies both filters
  (tags filtered within the memory_id neighbourhood).
- `group_by_domain` mode applies the `tags` filter per-domain if `tags` is supplied
  and no `memory_id` is given.
- `group_by_domain` is ignored when `memory_id` is supplied (memory_id-scoped results
  span at most one domain anchor, grouping adds no value; document this in description).
- Existing behaviour (no tags, no memory_id) is unchanged.

---

## Changes

### 1. db/db.go — RecentChangesByTags (new function)

```go
func (s *Store) RecentChangesByTags(domain string, tags []string, limit int) ([]Node, error)
```

Query: `SELECT ... FROM nodes WHERE archived_at IS NULL [AND domain = ?] AND <tagFilter> ORDER BY updated_at DESC LIMIT ?`

Use `tagFilter("tags", tags, nil, nil)` from `db/util.go` to build the WHERE clause.
`domain` is optional (empty = cross-domain).

### 2. db/db.go — RecentChangesForMemoryID (new function)

```go
func (s *Store) RecentChangesForMemoryID(memoryID string, depth, limit int) ([]Node, error)
```

1. Call `s.neighbourhoodIDs(memoryID, depth)` to get the BFS ID set.
2. Build a query: `SELECT ... FROM nodes WHERE id IN (?) AND archived_at IS NULL ORDER BY updated_at DESC LIMIT ?`
3. Use the standard SQLite IN-list approach (build a `?,?,?` placeholder string).

### 3. db/db.go — RecentChangesForMemoryIDByTags (new function, or augment §2)

Either a fourth function or add an optional `tags []string` parameter to
`RecentChangesForMemoryID`. Simpler: a single function
`RecentChangesScoped(memoryID string, depth int, domain string, tags []string, limit int) ([]Node, error)`
that composes the neighbourhood filter (when memoryID != "") with the tag filter
(when tags is non-empty) and domain filter. Replaces all three new functions with
one composable one — choose whichever is cleaner.

### 4. tools/tools.go — `recent` args struct

```go
var a struct {
    Domain        string `json:"domain"`
    Limit         int    `json:"limit"`
    GroupByDomain bool   `json:"group_by_domain"`
    Tags          string `json:"tags"`   // new
    MemoryID      string `json:"memory_id"` // new
}
```

### 5. tools/tools.go — `recent` handler routing

```
if a.MemoryID != "":
    use RecentChangesForMemoryID (with optional tag filter)
elif a.Tags != "":
    use RecentChangesByTags (apply domain filter if supplied)
elif a.GroupByDomain:
    existing group-by-domain path (apply tag filter in the grouping loop if a.Tags != "")
else:
    existing RecentChanges path
```

### 6. tools/tools.go — `recent` input schema

Add to Properties:

```go
"tags": {Type: "string", Description: "Comma-separated tags to filter by. Only returns memories carrying at least one of the supplied tags. OR semantics."},
"memory_id": {Type: "string", Description: "Anchor node ID. Scopes results to the depth-2 BFS neighbourhood of this node, ordered by most-recently-updated first. group_by_domain is ignored when memory_id is supplied."},
```

### 7. tools/tools.go — `recent` description update

Add to the description: `Supply tags to scope to a workstream. Supply memory_id to scope to a node's neighbourhood.`

---

## Tests

### db/db_test.go

- `TestRecentChangesByTags_MatchesOneTag` — two nodes, one tagged "TDD", query tags="TDD", verify only the tagged node returned
- `TestRecentChangesByTags_OR_Semantics` — two nodes with different tags; query with both tags, verify both returned
- `TestRecentChangesByTags_DomainScoped` — two domains, same tag, domain filter, verify only one domain returned
- `TestRecentChangesForMemoryID_NeighbourhoodOnly` — 3 nodes: anchor, neighbour, unrelated; verify only anchor + neighbour in results

### tools/tools_test.go

- `TestRecent_TagsFilter` — add two nodes (one tagged "TDD"), call recent with tags="TDD", verify only tagged node returned
- `TestRecent_MemoryID_ScopesNeighbourhood` — anchor + neighbour + unrelated; recent with memory_id=anchor.ID, verify unrelated excluded
- `TestRecent_TagsAndMemoryID_Combined` — neighbourhood of 3 nodes, two tagged, one not; tags filter applied within neighbourhood
- `TestRecent_ExistingBehaviourUnchanged` — no tags, no memory_id: same results as before
