# decision_type enum — standing rules + orient rules section

**Status:** COMPLETE — shipped in v1.27.0

**Shared-surface node:** `standing-rules-are-decisions-tag-bfadff8e`

---

## Why

The `transient` boolean is replaced by a three-value `decision_type` enum:
`transient | decision | standing`. `standing` is new — a constraint the user judges
to hold until explicitly revised. Orient surfaces `standing` nodes in a dedicated
`rules` section before `declared_spine`, ordered by total inbound edge count descending.

The `governed_by` relationship type is also added to the `connect` enum so agents
can explicitly link work back to the standing rule that governs it.

---

## Acceptance criteria

- `remember` accepts `decision_type` with values `transient`, `decision`, `standing`;
  defaults to `decision` when omitted.
- `revise` can change `decision_type` on an existing node (single and batch).
- `orient` returns a `rules` section as the first section in the response, containing
  `standing` nodes ordered by total inbound edge count DESC, using lean format
  (id + label + truncated why_matters). Omitted entirely when no standing nodes exist.
- `connect` accepts `governed_by` as a valid relationship type.
- `audit(mode=stale)` uses `decision_type = 'transient'` for transient detection.
- Node JSON includes `decision_type` field on all responses.
- `Transient bool` is removed from the `Node` Go struct and from `AddNode`/`UpdateNode`
  signatures. The `transient` SQLite column is dropped in migration v12 via
  `ALTER TABLE nodes DROP COLUMN transient` — it qualifies for SQLite's native drop
  (no PRIMARY KEY, no UNIQUE, no index, no partial-index WHERE clause, no FK reference).
  Tools handler still accepts `transient: true` from old clients and maps it
  to `decision_type = "transient"`.
- Existing tests that used the `transient` param are updated to use `decision_type`.

---

## Changes

### 1. DB migration v12

Add to the `migrations` slice in `db/db.go` (version 12):

```go
{
    Version: 12,
    SQL: `ALTER TABLE nodes ADD COLUMN decision_type TEXT NOT NULL DEFAULT 'decision';
UPDATE nodes SET decision_type = 'transient' WHERE transient = 1;`,
},
```

### 2. db/db.go — Node struct

Remove `Transient bool`. Add `DecisionType string` in its place:

```go
DecisionType string `json:"decision_type,omitempty"`
```

The `transient` SQLite column is kept but no longer selected. After scanning
`decision_type`, no backfill is needed — the migration handles legacy rows.

### 3. db/db.go — All SELECT queries on nodes

Every query that selects node columns must append `, decision_type` to the column list
and add `&n.DecisionType` to the corresponding `Scan()` call. Affected functions:

- `GetNode`
- `searchNodesLike` (via `scanNodeRows`)
- `searchNodesSemantic`
- `RecentChanges`
- `Timeline` (via `scanNodeRows`)
- `GetHistoryForMemoryID`
- `FindDisconnected`
- `ListArchived`
- `FindDrift`
- `GetStandingNodes` (new — see §5)
- `UpdateNode` (the initial fetch and the return scan)
- `UpdateNodesBatch`
- Any other function that calls `scanNodeRows` or inline-scans a `nodes` row

`scanNodeRows` is a shared helper — update it once. Remove `transient` from all
SELECT column lists and Scan calls. Add `decision_type` in its place.

### 4. db/db.go — AddNode

Remove `transient bool` parameter. Add `decisionType string` parameter (default
`"decision"` when empty). Insert `decision_type` into the INSERT; keep inserting
`0` for the `transient` column for schema compatibility.

### 5. db/db.go — GetStandingNodes (new function)

```go
func (s *Store) GetStandingNodes(domain string) ([]Node, error) {
    domain = s.ResolveAlias(domain)
    rows, err := s.db.Query(`
        SELECT n.id, n.label, n.description, n.why_matters, n.domain,
               n.created_at, n.updated_at, n.occurred_at, n.archived_at,
               n.tags, n.decision_type,
               COUNT(e.id) AS inbound_count
        FROM nodes n
        LEFT JOIN edges e ON e.to_memory = n.id
        WHERE n.domain = ? AND n.archived_at IS NULL AND n.decision_type = 'standing'
        GROUP BY n.id
        ORDER BY inbound_count DESC`,
        domain,
    )
    // ... scan into []Node (inbound_count is scanned into a throwaway int64)
}
```

Cap at 20 results (same as declared_spine); standing sections larger than that
should be maintained by the user.

### 6. db/db.go — UpdateNode signature

```go
func (s *Store) UpdateNode(
    id string,
    label, description, whyMatters, tags *string,
    occurredAt *time.Time,
    decisionType *string,   // replaces transient *bool; nil means "leave unchanged"
) (*Node, error)
```

Remove `transient *bool` parameter. Add `decisionType *string`. When
`decisionType != nil`, validate against the three-value enum before setting;
return an error for any other value.

### 7. db/db.go — NodeUpdateInput struct

Add `DecisionType *string` field.

### 8. tools/tools.go — `remember` input schema

Add to single-mode `Properties`:

```go
"decision_type": {
    Type: "string",
    Description: "Lifespan of this memory. 'decision' (default) — a normal decision or finding. 'transient' — short-lived; audit(mode=stale) flags it after 7 days. 'standing' — a constraint or rule that holds until explicitly revised; appears in orient's rules section ordered by how often work references it.",
    Enum: []string{"decision", "transient", "standing"},
},
```

Remove or deprecate the existing `transient` boolean property from the schema
(keep parsing it for backward compat, but map `transient: true` → `decision_type:
"transient"` in the handler). Do NOT remove the `transient` parse — old clients
may still send it.

### 9. tools/tools.go — `remember` handler (addNode / addNodesBatch)

Wire `DecisionType string` through to `AddNode`. When `args.Transient == true`
and `args.DecisionType == ""`, set `DecisionType = "transient"` for backward compat.

### 10. tools/tools.go — `revise` input schema

Add `decision_type` to single-mode Properties and to the `items` batch JSON schema:

Single mode:
```go
"decision_type": {
    Type: "string",
    Description: "Change the lifespan: 'decision', 'transient', or 'standing'.",
    Enum: []string{"decision", "transient", "standing"},
},
```

Batch items JSON — add `"decision_type": {"type": "string", "enum": ["decision", "transient", "standing"]}` alongside existing fields.

### 11. tools/tools.go — `revise` handler (updateNode / updateNodesBatch)

Add `DecisionType *string \`json:"decision_type"\`` to the args struct.
Pass it through to `UpdateNode` / `NodeUpdateInput.DecisionType`.

### 12. tools/tools.go — orient response

The orient response struct gains a `Rules []leanEntry \`json:"rules,omitempty"\`` field.
Populate it by calling `GetStandingNodes(domain)` and applying `leanEntry` formatting
(same truncation as significant/declared_spine). Place `rules` before `declared_spine`
in the response. Omit the field entirely when `GetStandingNodes` returns zero nodes
(`omitempty` handles this).

Update the `orient` description to document the `rules` section.

### 13. tools/tools.go — `connect` relationship enum

Add `"governed_by"` to the `Enum` slice on the `relationship` property, and add a
line to the description:

> `governed_by` — A is explicitly governed by standing rule B.

Update `TestListTools_NoStaleToolReferences` to add `governed_by` to the description
allowlist if any removed-tool test checks the connect description.

### 14. CLAUDE.md — update migration table

Add row: `| 12 | nodes: add decision_type TEXT column; migrate transient=1 → decision_type='transient' |`

---

## Tests

All tests must be written first (red), then the production code (green).

### db/db_test.go

- `TestAddNode_DecisionTypeDefaultsToDecision` — omit decision_type, verify `decision_type = 'decision'` in returned node
- `TestAddNode_DecisionTypeStanding` — pass `decision_type = 'standing'`, verify stored
- `TestGetStandingNodes_Empty` — domain with no standing nodes returns empty slice
- `TestGetStandingNodes_OrderedByInboundEdgeCount` — 3 standing nodes with 0, 1, 2
  inbound edges respectively; verify returned in descending count order
- `TestUpdateNode_DecisionType` — update a node from `decision` to `standing`; verify
- `TestUpdateNode_DecisionType_Invalid` — update with `"nonsense"` returns error

Existing tests updated to use `decision_type` (no longer pass `transient bool`):
- `TestAddNode_Transient_Persists` → `TestAddNode_DecisionTypeTransient_Persists`
- `TestAddNode_Transient_DefaultsFalse` → removed (decision is now the default)
- `TestFindDrift_*` tests updated to use `decision_type: "transient"` in `AddNode`
- tools_test.go `TestAddNode_Transient_PersistedAndReturned`, `TestDrift_*`,
  `TestDisconnectedExcludesTransient` updated similarly

### tools/tools_test.go

- `TestRemember_DecisionTypeStanding` — remember with `decision_type: "standing"`, recall, verify field
- `TestRemember_DecisionTypeBackcompat_Transient` — remember with legacy `transient: true`, verify `decision_type == "transient"` on recall
- `TestRevise_DecisionType` — remember as `decision`, revise to `standing`, recall, verify
- `TestOrient_RulesSection_StandingNodes` — add 2 standing nodes, orient, verify `rules` key present and non-empty
- `TestOrient_RulesSection_Absent_WhenNoStanding` — orient with only `decision` nodes, verify no `rules` key
- `TestOrient_RulesSection_OrderedByInboundEdgeCount` — 2 standing nodes, connect one inbound edge to the second; verify second appears first in rules
- `TestConnect_GovernedBy` — connect two nodes with `governed_by`, verify no error
