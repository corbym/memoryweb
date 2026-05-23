# remember Batch related_to

Adds `related_to` support to `remember`'s batch mode so agents can connect nodes at filing
time without a separate `connect` call.

---

## Motivation

`remember`'s `related_to` parameter (auto-connect at filing time) only works in single mode.
Batch mode (`items` array) requires a separate `connect` call after filing. Short-task agents
consistently skip this second call, producing orphans.

Stats evidence: binder domain, 12 orphan sessions May 8–22, all following the pattern
`remember × 1-2 / orient × 1 / exit` with no connect call. Batch mode is the lowest-friction
fix: connect at filing time with zero extra tool calls.

The two-call pattern (remember then connect) is structurally unreliable for agents with
short task scopes. `related_to` in batch mode collapses it to one call.

---

## Change

### Schema update

Each item in the `items` array currently accepts: `label`, `description`, `why_matters`,
`tags`, `domain`, `occurred_at`, `transient`.

Add `related_to` as an optional field on each item. Accept the same two forms as the
existing single-mode `related_to`:

- String: `"related_to": "node-id"` — creates a `connects_to` edge to the given ID
- Object: `"related_to": {"id": "node-id", "relationship": "caused_by"}` — creates an
  edge with the specified relationship type

Also accept an array of either form to connect to multiple nodes at filing time:
`"related_to": ["id-one", {"id": "id-two", "relationship": "depends_on"}]`

Invalid IDs in `related_to` must be silently skipped (consistent with existing single-mode
behaviour). Do not fail the whole batch item if one `related_to` ID is bad.

### Handler update

In `tools/tools.go`, `handleAddNodes` (batch handler): after writing each node, process
its `related_to` entries using the same edge-creation path as the single-mode handler.
The edge creation must be wrapped in the same transaction as the node write (audit
atomicity contract — see CLAUDE.md).

### Description update

Update the `items` array property description in the `remember` schema to mention
`related_to`:

> "Each item supports the same fields as single-mode remember, including related_to for
> connecting at filing time. Use related_to to avoid a separate connect call — especially
> important for short-task agents."

---

## Acceptance criteria

- `TestBatchRemember_RelatedToString`: batch-remember two nodes, one with `related_to`
  as a plain string ID; assert an edge exists from the filed node to the target.
- `TestBatchRemember_RelatedToObject`: batch-remember a node with `related_to` as
  `{"id": "...", "relationship": "caused_by"}`; assert edge exists with correct relationship.
- `TestBatchRemember_RelatedToArray`: batch-remember a node with `related_to` as an
  array of two entries; assert both edges exist.
- `TestBatchRemember_RelatedToInvalidId_Skipped`: batch-remember a node with an invalid
  `related_to` ID; assert the node was created (no error) and the bad edge was silently
  skipped.
- `TestBatchRemember_RelatedToAbsent_NoEdge`: batch-remember a node with no `related_to`;
  assert no edges were created for that node.
- `TestBatchRemember_OrphanWarning_AbsentWhenRelatedToUsed`: batch-remember with
  `related_to` on all items; assert `orphan_warning` is `""` in the response (coordinates
  with orphan-nudge.md).
- Existing single-mode `related_to` tests still pass — no regression.
- `go test ./...` green.

---

## Files

- `tools/tools.go` — `items` array item schema, `handleAddNodes` batch handler
- `db/db.go` — no change expected (edge creation uses existing `AddEdge` / `LogAudit`)
- `tools/tools_test.go` — new batch related_to tests

## References

- shared-surface node: `remember-batch-mode-related-to-n-6b24f584`
- memoryweb node: `remember-tool-connect-instructio-30aa92d0`
