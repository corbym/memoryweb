# Cross-Domain Connect UX

> **COMPLETE** — shipped 2026-05-23.
> - Change 1: `EdgeSuggestion.Domain` field added in `db/db.go`; `SuggestEdges` sets `Domain: targetDomain` on every suggestion. `TestSuggestedConnections_IncludesDomain` added.
> - Change 2: orphan warnings updated to include `domain=<domain>` guidance in both `handleAddNode` and `handleAddNodes`.
> - Change 3: `AddEdge` looks up from-node's domain and includes it in the "node not found" error. `TestConnect_CrossDomain_ErrorMentionsDomain` and `TestConnect_SameDomain_Succeeds` added.

Fixes three related gaps that cause agents to fail silently when connecting nodes across
domains.

---

## Motivation

`connect` resolves node IDs within a domain scope. When a `to_id` lives in a different
domain than the `from_id`, the call fails with a generic "node not found" error. This
creates a silent failure chain:

1. `remember` returns `suggested_connections` with node IDs but no domain field.
2. The agent calls `connect` using those IDs, omitting the domain parameter.
3. `connect` returns "node not found" — no indication of which domain was searched or where
   the target actually lives.
4. The agent gives up or retries identically. The node is left orphaned, defeating the
   orphan warning that prompted the connect call.

The orphan warning after `remember` makes this worse: it instructs agents to "call connect
now" without mentioning the domain constraint, actively leading agents into the failing call.

---

## Three changes

### Change 1 — Add `domain` to `suggested_connections` entries

In `tools/tools.go`, each entry in the `suggested_connections` array currently contains
`id` and `reason`. Add `domain` as a third field.

In `db/db.go`, `SuggestConnections` (or equivalent) must return the domain of each
candidate node. Update the query to include `nodes.domain` in the SELECT.

With this change, an agent can check whether the suggested target is in the same domain
before calling `connect`. If it's in a different domain, the agent knows to either skip
the connection or use a cross-domain approach once one exists.

### Change 2 — Improve the orphan warning message

After `remember`, when `orphan_warning` is triggered (see also orphan-nudge.md),
include the domain context:

> "No connections were made. Call `connect` with `domain=<this_domain>` to link these
> memories. Suggested connections in other domains cannot be connected directly — check
> their domain field first."

If this story and orphan-nudge.md are run together, the `orphan_warning` field from
orphan-nudge.md is the right place to add this language.

### Change 3 — Improve `connect` "node not found" error

In `tools/tools.go`, `handleAddEdge`, when a node lookup fails, include the domain that
was searched in the error message:

> "node not found: 'target-id-here' was not found in domain 'source-domain'. If this
> node is in a different domain, cross-domain connections are not yet supported — search
> for it first to confirm its domain."

This makes the error recoverable: the agent knows exactly what was searched and why it
failed.

---

## Acceptance criteria

- `TestSuggestedConnections_IncludesDomain`: call `remember`; assert each entry in
  `suggested_connections` includes a non-empty `domain` field.
- `TestConnect_CrossDomain_ErrorMentionsDomain`: create two nodes in different domains;
  attempt to connect them; assert the error message contains the domain that was searched.
- `TestConnect_SameDomain_Succeeds`: sanity-check that same-domain connect still works
  after the error message change.
- `remember` orphan warning message (or description) mentions the `domain` parameter for
  `connect` — can be verified as a string assertion.
- `go test ./...` green.

---

## Files

- `db/db.go` — `SuggestConnections` returns domain per candidate
- `tools/tools.go` — `suggested_connections` response struct, `handleAddEdge` error
  message, orphan warning wording
- `tools/tools_test.go` — new tests

## References

- shared-surface node: `suggested-connections-in-remembe-2330807b`
- shared-surface node: `orphan-warning-after-remember-do-7bf26874`
- shared-surface node: `connect-fails-silently-when-targ-e86973ca`
