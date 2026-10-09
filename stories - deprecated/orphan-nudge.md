# Orphan Nudge

> **COMPLETE** — shipped 2026-05-23.
> - Change A: `orphan_warning` field on `handleAddNode` and `handleAddNodes` responses (shipped prior to this session).
> - Change B: `runDream()` calls `FindDisconnected` and lists isolated nodes (shipped prior to this session).
> - Change C: connect imperative hoisted to top of `remember` description. Wording: "After filing, call connect for every suggested_connections entry before ending your session. Orphaned memories lose context immediately." Shipped as part of `tool-description-quality-pass`. Covered by `TestRemember_ConnectInstructionAtTop`.

---

Three changes to reduce the rate of single-node orphan filings.

---

## Motivation

Stats show the dominant filing failure mode is a session that calls `remember` once or
twice and exits without connecting anything. The connect instruction in the `remember`
description is positioned at the end, after all parameter docs — short-task agents
(remember × 1-2, orient × 1, exit) complete their task before reaching it.

Three contact points for the nudge:
1. **At filing time** — handler response warns immediately when the filed node has no edges.
2. **At session end** — the dream hook lists disconnected nodes in its stop-reason block.
3. **In the description** — move and strengthen the connect instruction.

---

## Change A — Handler `orphan_warning` field

In `tools/tools.go`, add an `orphan_warning` string field to the `addNode` and `addNodes`
(batch) handler responses.

- When the filed node(s) have zero edges after filing (no `related_to` was provided and no
  existing edges exist), set `orphan_warning` to:
  > "No connections were made. Call connect now to link this memory before ending your
  > session — orphaned memories lose context immediately."
- When connections exist (related_to was used, or edges already exist), set `orphan_warning`
  to empty string `""` so it doesn't clutter normal responses.
- For batch mode, warn if **any** filed node ended up with zero edges.

Implementation note: `orphan_warning` must be computed after the write completes, not
before. Use `store.GetNodeEdgeCount(id)` (or equivalent) to check edge count.

### Acceptance criteria for Change A

- `TestAddNode_OrphanWarning_PresentWhenNoEdges`: add a node with no `related_to`; assert
  `orphan_warning` is non-empty in the response.
- `TestAddNode_OrphanWarning_AbsentWhenConnected`: add a node with a valid `related_to`;
  assert `orphan_warning` is `""` in the response.
- `TestAddNodes_OrphanWarning_PresentWhenAnyOrphaned`: batch-add two nodes, one with
  `related_to` and one without; assert `orphan_warning` is non-empty.
- `TestAddNodes_OrphanWarning_AbsentWhenAllConnected`: batch-add two nodes both with
  `related_to`; assert `orphan_warning` is `""`.

---

## Change B — Dream orphan section

In `main.go`, in the `runDream()` function, add a "Disconnected memories" section.

After the existing dream sections, call `store.FindDisconnected("", 10)` (all domains,
limit 10). If any isolated nodes are found, append to the dream output:

```
## Disconnected memories (no connections)
The following memories have no connections and will not appear in graph traversals.
Consider calling connect or archive if no longer relevant:

- [domain] label (id: node-id)
- ...
```

If no isolated nodes exist, skip the section entirely (no empty header).

This surfaces orphans in the pre-compact hook's stop-reason block at session end, giving
the agent a final prompt to connect or clean up before exiting.

### Acceptance criteria for Change B

- `TestDream_IncludesDisconnectedSection`: seed a domain with one isolated node; call
  `runDream()`; assert the output contains "Disconnected memories" and the node label.
- `TestDream_OmitsDisconnectedSectionWhenNone`: seed a domain with all nodes connected;
  assert the dream output does not contain "Disconnected memories".

---

## Change C — Description tightening

In `tools/tools.go`, update the `remember` tool description:

**Move** the connect instruction to the **top** of the description body — before the
parameter list.

**Replace** the current advisory wording with an imperative:

> "After filing, call `connect` for every entry in `suggested_connections` before ending
> your session. Orphaned memories lose context immediately and accumulate as graph drift."

Apply the same imperative wording to the batch (`items`) mode description.

### Acceptance criteria for Change C

- The word "connect" appears before the first parameter description in the `remember`
  tool description (positional check via string index assertion in a test, or manual
  verification).
- The description does not use "review" or "consider" in the connect instruction — it
  must be imperative.

---

## Files

- `tools/tools.go` — `handleAddNode`, `handleAddNodes`, `remember` description
- `main.go` — `runDream()`
- `tools/tools_test.go` — Change A and C tests
- `main_test.go` or `setup_test.go` — Change B tests (wherever `runDream` tests live)

## References

- memoryweb node: `orphan-nudge-plan-handler-warnin-0cc73d5c`
- memoryweb node: `remember-tool-connect-instructio-30aa92d0`
