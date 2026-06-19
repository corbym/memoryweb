# node_kind — rename decision_type, expand enum to full knowledge classifier

**Status:** COMPLETE — implemented 2026-06-19, awaiting version bump/release

**Shared-surface node:** `build-node-kind-rename-decision--4e2cd629`

---

## Why

`decision_type` was introduced as a lifespan/classification flag for propositional
knowledge (`transient`, `decision`, `standing`). It has grown into the universal
node classifier — but the name still says "decision" and the enum still only covers
propositional kinds. This breaks down immediately for referential knowledge: filing
a person, system, or organisation as a `decision` is semantically wrong, and there
is currently no correct value to use.

The rename and enum expansion are a coordinated migration across both memoryweb and
Recordari. Both products must carry the same field name and the same enum values so
the tool surface stays aligned.

---

## New enum

Old: `transient | decision | standing`

New: `transient | reference | issue | decision | option | assumption | finding | standing | goal`

Semantics:

| Kind | Meaning |
|------|---------|
| `reference` | Entity that exists in the world — person, system, org, document |
| `issue` | A problem, question, or tension to be resolved |
| `decision` | A settled choice or fact (default) |
| `option` | A candidate answer to an issue |
| `assumption` | An unverified epistemic precondition |
| `finding` | An empirical observation — discovered, verifiable |
| `standing` | A durable rule or constraint; appears in orient rules section |
| `goal` | A desired future state |
| `transient` | Short-lived state; surfaced by audit(mode=stale) after 7 days |

`decision` remains the default (zero-value maps to it). `standing` and `transient`
retain their existing semantics and query behaviour.

---

## Changes

### 1. Migration 13 — db/db.go

Append to the `migrations` slice:

```go
{
    version: 13,
    desc:    "nodes: rename decision_type column to node_kind",
    up: func(tx *sql.Tx) error {
        _, err := tx.Exec(`ALTER TABLE nodes RENAME COLUMN decision_type TO node_kind`)
        return err
    },
},
```

SQLite supports `RENAME COLUMN` since 3.25.0 (2018). No data migration required —
existing values (`transient`, `decision`, `standing`) are valid `node_kind` values.

### 2. Node struct — db/db.go

```go
// before
DecisionType string `json:"decision_type,omitempty"`

// after
NodeKind string `json:"node_kind,omitempty"`
```

### 3. SQL column references — db/db.go

Every occurrence of `decision_type` in SQL strings becomes `node_kind`. This covers:
- All `SELECT` column lists (~20 query sites)
- `INSERT INTO nodes (..., decision_type)` — two sites (`AddNode`, batch insert in
  `AddNodes`)
- `UPDATE nodes SET decision_type = ?` — `UpdateNode` and batch variant
- Filter clauses:
  - `decision_type = 'standing'` — `GetStandingNodes`, `GetOrphans`, stale standing
    check in `GetStaleDrift`
  - `decision_type = 'transient'` — stale transient check in `GetStaleDrift`
  - `decision_type != 'transient'` — orphan exclusion filter

### 4. Validation — db/db.go UpdateNode

The switch statement that validates the value must be replaced with a set-membership
check covering all nine values:

```go
validKinds := map[string]bool{
    "transient": true, "reference": true, "issue": true,
    "decision": true, "option": true, "assumption": true,
    "finding": true, "standing": true, "goal": true,
}
if !validKinds[*nodeKind] {
    return nil, fmt.Errorf("invalid node_kind %q: must be one of transient, reference, issue, decision, option, assumption, finding, standing, goal", *nodeKind)
}
```

### 5. AddNode / UpdateNodeInput — db/db.go

Rename parameter and struct field throughout:
- `AddNode(..., decisionType string)` → `AddNode(..., nodeKind string)`
- `UpdateNodeInput.DecisionType *string` → `UpdateNodeInput.NodeKind *string`
- All callers in `db.go` that set or read `DecisionType` → `NodeKind`

### 6. tool parameter — tools/tools.go

**Rename the parameter in both `remember` and `revise`:**

Old parameter name: `decision_type`  
New parameter name: `node_kind`

Update the `InputSchema` property key, its `Description`, and its `Enum` slice in
both tool definitions and in the batch `items` inline JSON schema string.

New description for `remember`:
> "Classify this memory. 'decision' (default): a settled fact or choice. 'reference':
> an entity (person, system, org). 'issue': a problem or open question. 'option': a
> candidate answer to an issue. 'assumption': an unverified precondition. 'finding':
> an empirical observation. 'standing': a durable rule — appears in orient rules.
> 'goal': a desired future state. 'transient': short-lived state, surfaced by
> audit(mode=stale) after 7 days."

New `Enum`: `["transient","reference","issue","decision","option","assumption","finding","standing","goal"]`

**Backward-compat rejection of old key:** In `handleAddNode`, `handleAddNodes`,
`handleUpdateNode`, and `handleUpdateNodes`, check for the old key before processing
and return a clear error if present:

```go
var rawCheck map[string]json.RawMessage
json.Unmarshal(args, &rawCheck)
if _, ok := rawCheck["decision_type"]; ok {
    return errorResult("decision_type has been renamed to node_kind — use node_kind instead"), nil
}
```

Apply the same pattern inside the batch `items` array: unmarshal each item raw,
check for `decision_type` key presence, error immediately if found.

**Keep `transient` boolean backcompat:** The `transient bool` parameter continues to
map to `node_kind='transient'` as before. Do not remove this.

### 7. Go struct fields in tool handlers — tools/tools.go

Every anonymous struct that has `DecisionType string \`json:"decision_type"\`` must
be updated to `NodeKind string \`json:"node_kind"\``. This covers the argument
structs for `handleAddNode`, `handleAddNodes`, `handleUpdateNode`,
`handleUpdateNodes`.

### 8. Description references — tools/tools.go

The `transient` boolean parameter's description in both `remember` and `revise`
currently says "use decision_type='transient' instead" — update to `node_kind`.

The `revise` batch `items` description currently references `decision_type` —
update to `node_kind`.

### 9. CLAUDE.md

Add migration 13 to the migrations table.

---

## Acceptance criteria

- `TestAddNode_NodeKind_Default`: call `remember` with no `node_kind`; assert
  returned node has `"node_kind": "decision"`.
- `TestAddNode_NodeKind_AllValues`: for each of the nine kind values, call
  `remember` with that value; assert it round-trips correctly.
- `TestAddNode_DecisionType_Rejected`: call `remember` with `"decision_type":
  "decision"` (old key); assert IsError and message mentions `node_kind`.
- `TestRevise_NodeKind`: call `revise` with `node_kind=assumption`; assert updated.
- `TestRevise_DecisionType_Rejected`: call `revise` with old key; assert error.
- `TestUpdateNode_NodeKind_InvalidValue`: call `revise` with an unrecognised kind;
  assert error containing valid kinds list.
- `TestGetStandingNodes_UsesNodeKind`: existing `standing` nodes are still returned
  by orient rules section after migration.
- `TestGetOrphans_ExcludesReference`: file a `reference` node with no edges; confirm
  it is NOT excluded from orphan detection (only `transient` is excluded, not
  `reference`).
- `TestGetStaleDrift_TransientNodes`: existing transient stale detection still works.
- All existing `decision_type`-keyed tests updated to use `node_kind`.
- `go test ./...` green.

---

## Files

- `db/db.go` — migration 13, Node struct, all SQL, AddNode, UpdateNodeInput,
  UpdateNode validation
- `tools/tools.go` — parameter rename, backward-compat rejection, enum expansion,
  handler structs
- `tools/tools_test.go` — new tests, updated existing tests
- `CLAUDE.md` — migration table

---

## Notes

- The orphan exclusion filter (`node_kind != 'transient'`) remains correct — only
  `transient` nodes are excluded from orphan detection, not `reference` or any other
  kind.
- `reference` nodes should be omittable from `declared_spine` in a future orient
  refinement (they are entities, not decisions). That is out of scope here.
- This is the hard prerequisite for the computed-trust story.

---

## References

- Shared-surface node: `build-node-kind-rename-decision--4e2cd629`
- Related (prior decision_type work): `standing-rules-are-decisions-tag-bfadff8e`
- Dependent story: `stories/computed-trust.md`
