# significance: memory_id mode — neighbourhood-scoped importance analysis

**Shared-surface node:** `significance-tool-memory-id-mode-2dbc5fbc`

**Design decisions node (memoryweb-meta):** `significance-memory-id-mode-desi-124a42fe`

---

## Why

The current `significance` tool scopes to an entire domain. For workstream analysis —
"what's load-bearing in this cluster of related work?" — a domain scan returns too much
noise. An agent that knows an anchor node (a feature, a decision, a workstream root) has
no way to scope significance to the connected neighbourhood without a full domain scan.

`memory_id` mode fixes this: scope significance to the depth-2 neighbourhood of an anchor
node, domain-clipped. This answers a different question from domain mode:
- **domain mode**: "what is load-bearing across this whole domain right now?"
- **memory_id mode**: "what is load-bearing within this workstream?" — workstream defined
  by graph topology from a known anchor, not by metadata labels.

### Why depth 2, not depth 1

Depth 1 was considered and explicitly rejected. At depth 1 the only inbound edges counted
are those between the anchor and its direct neighbours — the bulk of real inbound signal
for neighbourhood nodes comes from outside, producing near-uniform low scores and making
the structural section near-useless. **Depth 2 is the minimum for meaningful scoring.**
Depth is a configurable parameter so callers can widen it, but the default must be 2 and
depth 1 must not be used as the default.

---

## Changes

### 1. DB — `neighbourhoodIDs` (private BFS helper)

New private function in `db/db.go`:

```go
func (s *Store) neighbourhoodIDs(nodeID string, depth int) (ids []string, anchorDomain string, err error)
```

BFS from `nodeID` for `depth` hops. Domain-clips: only follows edges to nodes where
`node.domain == anchorDomain` (the anchor's domain, resolved from the initial lookup).
Returns all IDs in the neighbourhood (including the anchor) and the resolved domain.

Algorithm:
1. Look up anchor node — get its domain. Error if not found or archived.
2. BFS round 0: seed with `{nodeID}`.
3. For each depth round: query edges where one endpoint is in the current frontier AND the
   other endpoint is in the anchor's domain AND not already visited. Expand frontier.
4. Collect all visited IDs. Return them + anchorDomain.

Cross-domain edges are not followed. A node reachable only via a cross-domain hop is
excluded.

### 2. DB — `GetSignificanceByNodeIDs`

New exported function in `db/db.go`:

```go
func (s *Store) GetSignificanceByNodeIDs(nodeIDs []string, domain string, recencyWindowDays int) (SignificanceResult, error)
```

Same logic as `GetSignificance` but replaces `WHERE domain = ?` with `WHERE id IN (...)`
in both the declared and structural queries. The `domain` parameter is used only for
logging (significance_log rows must record the resolved domain, not blank — blank domain
rows break decay-function stats analysis).

Key differences from `GetSignificance`:
- No `limit` parameter — the neighbourhood is naturally bounded, so all structural nodes
  are returned (no top-N cap).
- Uses `nodeIDs` as the scoping filter, not domain string.

### 3. Handler — `handleSignificance`

Update args struct:

```go
var a struct {
    Domain        string `json:"domain"`
    MemoryID      string `json:"memory_id"`
    Depth         int    `json:"depth"`
    Limit         int    `json:"limit"`
    RecencyWindow int    `json:"recency_window"`
}
```

Update guard:

```go
if a.Domain == "" && a.MemoryID == "" {
    return errorResult("domain or memory_id is required"), nil
}
```

Dispatch:

```go
if a.MemoryID != "" {
    if a.Depth <= 0 {
        a.Depth = 2
    }
    ids, anchorDomain, err := h.store.neighbourhoodIDs(a.MemoryID, a.Depth)
    // handle err ...
    res, err = h.store.GetSignificanceByNodeIDs(ids, anchorDomain, a.RecencyWindow)
} else {
    res, err = h.store.GetSignificance(a.Domain, a.Limit, a.RecencyWindow)
}
```

`memory_id` takes precedence if both are supplied — the `if a.MemoryID != ""` branch runs
first.

### 4. Schema — update `significance` tool definition

Add properties:

```go
"memory_id": {Type: "string", Description: "Optional — scope significance to a memory's neighbourhood (depth 2 by default, domain-clipped). Useful for workstream health checks when you know the anchor memory. Takes precedence over domain if both are supplied."},
"depth":     {Type: "integer", Description: "Neighbourhood depth when using memory_id (default 2). Depth 1 produces near-uniform low scores and must not be used. Increase only when the workstream is large and depth 2 under-represents it."},
```

Change `domain` description to reflect it is now optional:

```go
"domain": {Type: "string", Description: "Domain to analyse. Required unless memory_id is supplied."},
```

Remove `domain` from `Required`:

```go
Required: []string{},
```

Update tool description — add after the existing opening paragraph:

> Pass `memory_id` to scope significance to a single memory's neighbourhood (depth 2,
> domain-clipped) — useful for workstream health checks when you already know the anchor.
> Pass `domain` for a full domain scan. `memory_id` takes precedence if both are supplied.

---

## Tests

### Rename

`TestSignificance_IsErrorOnMissingDomain` → `TestSignificance_IsErrorWhenNeitherDomainNorMemoryIDProvided`

Behaviour unchanged — calling significance with neither domain nor memory_id is still an
error. Update the test name only.

### New tests

- `TestSignificance_MemoryIDMode_ReturnsAllFourSections`: add anchor node with several
  connected nodes; call significance with memory_id; verify four sections present and
  non-nil.
- `TestSignificance_MemoryIDMode_DomainClipped`: add anchor node with an edge to a node
  in a different domain; verify the cross-domain node does NOT appear in structural
  results.
- `TestSignificance_MemoryIDMode_Depth2Included`: add anchor → A → B (two hops); verify
  B is included in the result set (structural or declared) despite being two hops from
  anchor.
- `TestSignificance_MemoryIDMode_Depth1Excluded`: add anchor → A → B; call with
  `depth=1`; verify B is NOT included (only anchor and A).
- `TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain`: call with both domain and
  memory_id supplied; verify the result is the neighbourhood result, not the full domain
  result (fewer nodes in structural than the domain would return).
- `TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID`: pass a non-existent memory_id;
  verify IsError.
- `TestSignificance_MemoryIDMode_InSchemaWithDepth`: verify memory_id and depth appear
  in the schema; verify domain is NOT in the Required array.

---

## Out of scope

- Tags filter for significance is a separate deferred story
  (`significance-tags-filter-deferre-6838a8be`). Must not be bundled here.
- The `neighbourhoodIDs` helper is private — not exposed as a standalone MCP tool.
- No changes to `GetNodeNeighbourhood` (the visualise/recall helper) — it remains depth 1
  and returns full node + edge data for its different use case.
