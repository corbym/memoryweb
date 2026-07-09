# remember: reinforce decision→finding linkback rule in the tool description

**Status:** OPEN

**Shared-surface node:** `issue-remember-s-tool-description-doesn-t-reinforce-the-decision-finding-linkback-rule-it-lives-only-in-the-skill-document-595914a3`

**Recordari precedent:** STORY-180 (shipped the equivalent fix in Recordari's `remember` description — this story is the memoryweb-side counterpart, never filed)

---

## Why

The rule "a decision derived from evidence should connect to its finding(s) via
`depends_on` or `caused_by`" exists today only in the recordari skill document
(Layer 1 rule 3 / Layer 2 relationship-types table). It is not in `remember`'s live
MCP tool description on either product.

Confirmed still missing on memoryweb: `tools/definitions.go`'s `remember` description
covers search-first domain inference, `suggested_connections` follow-up, and the full
`node_kind` taxonomy — but says nothing about linking a filed decision back to the
finding(s) it depends on. Recordari shipped the equivalent fix as STORY-180
(2026-07-04); memoryweb's copy was never filed.

Skill-only instructions are the least reliable layer per the skill's own stated
philosophy ("Why this shape" — position and framing determine agent compliance;
instructions agents never reach get skipped). A host that doesn't load the skill
(or an agent that never reads Layer 2) has no way to learn this rule at all. Moving
it into the tool description makes it survive regardless of skill availability.

---

## Change

### `tools/definitions.go` — `remember` description

Add one sentence to the `node_kind` guidance paragraph, immediately after the
`'finding': an empirical observation.` clause:

> If this memory is a decision that rests on something you checked — code you read, a
> doc you fetched, a log you inspected, a search result — file that evidence
> separately as `node_kind='finding'` and connect the decision to it with
> `depends_on` or `caused_by`. Don't let the decision's description silently absorb
> the evidence as prose.

Apply the same addition to the batch-mode `items` property description's node_kind
clause, kept terse (batch descriptions are already dense — one clause, not a full
paragraph).

---

## Acceptance criteria

- `remember`'s single-mode description explicitly instructs connecting a decision to
  its supporting finding(s) via `depends_on`/`caused_by`.
- Batch-mode `items` property description carries the same instruction, briefly.
- `TestListTools_*` description-quality tests (forbidden-words, no stale tool
  references) still pass — no structural vocabulary leaked in.
- `go test ./...` green.

---

## Files (expected)

- `tools/definitions.go` — `remember` tool description, `items` property description

---

## References

- Shared-surface node: `issue-remember-s-tool-description-doesn-t-reinforce-the-decision-finding-linkback-rule-it-lives-only-in-the-skill-document-595914a3`
- Related, distinct issue: `issue-agents-fold-source-material-code-docs-logs-search-results-into-a-decision-s-description-instead-of-filing-it-as-node-kind-finding-f63da6de` (evidence folded into description — a filing-discipline problem this linkback fix doesn't by itself solve, since an agent could still file the finding separately and just never connect it)
- Recordari: STORY-180 (shipped 2026-07-04)
