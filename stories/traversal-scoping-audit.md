# audit: tags + memory_id scoping parameters

**Status:** COMPLETE — v1.28.0 (commit bf280cd)

**Shared-surface node:** `traversal-tool-scoping-consisten-c691512d`

---

## Why

`history` and `significance` both accept `tags` and `memory_id`. `audit` has neither.
The coherent use cases are:

- `audit(mode=stale, tags="TDD")` — "what's drifting in my TDD workstream?"
- `audit(mode=stale, memory_id=X)` — "what's drifting around this node?"
- `audit(mode=orphans, tags="TDD")` — "which TDD-tagged nodes have no connections?"

Note: `memory_id` is not meaningful for `mode=orphans` (orphans have no edges; BFS
from an anchor would never reach them) or `mode=archived` (archived nodes may have
been in a neighbourhood but tracking that is not worth the complexity). `memory_id`
applies to `mode=stale` only.

`tags` applies to all three modes.

---

## Acceptance criteria

- `audit(mode=stale)` accepts `tags` (comma-separated) — only surfaces stale candidates
  carrying at least one of the supplied tags.
- `audit(mode=stale)` accepts `memory_id` — scopes candidates to the depth-2 BFS
  neighbourhood of the named node.
- `audit(mode=orphans)` accepts `tags` — only surfaces orphan nodes carrying at least
  one of the supplied tags. Does not accept `memory_id` (silently ignored or documented
  as not applicable).
- `audit(mode=archived)` accepts `tags` — only surfaces archived nodes carrying at
  least one of the supplied tags.
- Existing behaviour (no tags, no memory_id) is unchanged.

---

## Changes

### 1. db/db.go — FindDrift signature

```go
func (s *Store) FindDrift(domain string, limit int, tags []string, memoryID string, depth int) ([]DriftCandidate, error)
```

Add `tags []string` and `memoryID string, depth int` parameters. When `memoryID != ""`,
fetch the neighbourhood IDs first (`neighbourhoodIDs(memoryID, depth)`) and add
`AND id IN (...)` to the query. When `tags` is non-empty, apply `tagFilter()`. The
`domain` param continues to work as before; it is applied alongside any other filter.

Callers outside this story: check `FindDrift` call sites and update signatures.

### 2. db/db.go — FindDisconnected signature

```go
func (s *Store) FindDisconnected(domain string, tags []string) ([]Node, error)
```

Add `tags []string`. When non-empty, add tag filter clause (use `tagFilter()`).
No `memory_id` parameter — orphans have no connections by definition.

### 3. db/db.go — ListArchived signature

```go
func (s *Store) ListArchived(domain string, tags []string) ([]Node, error)
```

Add `tags []string`. When non-empty, add tag filter clause.

### 4. tools/tools.go — `audit` args struct and routing

The `auditTool` handler currently parses only `mode`. Expand to also parse `tags`,
`memory_id`, and `depth`, then pass them through to the three sub-handlers.

```go
var full struct {
    Mode     string `json:"mode"`
    Tags     string `json:"tags"`
    MemoryID string `json:"memory_id"`
    Depth    int    `json:"depth"`
    Domain   string `json:"domain"`
    Limit    int    `json:"limit"`
}
```

Pass `full` as `args` to the sub-handlers, which already parse domain/limit and
will be updated to also parse tags/memory_id.

### 5. tools/tools.go — `drift` handler (mode=stale)

Add `Tags` and `MemoryID` to the args struct; pass to `FindDrift`.

### 6. tools/tools.go — `findDisconnected` handler (mode=orphans)

Add `Tags` to the args struct; pass to `FindDisconnected`. Parse but ignore
`MemoryID` (add a comment explaining it is nonsensical for orphans).

### 7. tools/tools.go — `listArchived` handler (mode=archived)

Add `Tags` to the args struct; pass to `ListArchived`.

### 8. tools/tools.go — `audit` input schema

Add to Properties:

```go
"tags": {Type: "string", Description: "Comma-separated tags. Only surfaces candidates carrying at least one of the supplied tags. OR semantics. Applies to all three modes."},
"memory_id": {Type: "string", Description: "Anchor node ID. Scopes stale candidates to the depth-2 BFS neighbourhood of this node. Applies to mode=stale only; ignored for orphans and archived."},
```

### 9. tools/tools.go — `audit` description update

Add a note: `Supply tags to scope to a workstream. Supply memory_id (mode=stale only) to scope to a node's neighbourhood.`

---

## Tests

### db/db_test.go

- `TestFindDrift_TagsFilter` — two stale candidates, one tagged "TDD"; query tags="TDD", verify only tagged returned
- `TestFindDisconnected_TagsFilter` — two orphans, one tagged "review"; query tags="review", verify only tagged returned
- `TestListArchived_TagsFilter` — two archived nodes, one tagged "spike"; query tags="spike", verify only tagged returned
- `TestFindDrift_MemoryID_NeighbourhoodOnly` — anchor + two neighbours + one unrelated stale node; memory_id=anchor, verify unrelated excluded

### tools/tools_test.go

- `TestAudit_Stale_TagsFilter` — two stale nodes (one tagged), audit(mode=stale, tags=X), verify only tagged surfaced
- `TestAudit_Orphans_TagsFilter` — two orphans (one tagged), audit(mode=orphans, tags=X), verify only tagged surfaced
- `TestAudit_Archived_TagsFilter` — two archived (one tagged), audit(mode=archived, tags=X), verify only tagged surfaced
- `TestAudit_Stale_MemoryID` — neighbourhood contains one stale candidate; outside has another; memory_id scopes correctly
- `TestAudit_ExistingBehaviourUnchanged` — no tags, no memory_id: same results as before
