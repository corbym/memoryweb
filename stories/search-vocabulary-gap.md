# Search Vocabulary Gap

> **COMPLETE** — shipped prior to 2026-05-23.
> - Description leads with vocabulary constraint ("Queries must use vocabulary that appears in the stored label...").
> - `query` property description explains the constraint explicitly.
> - `SearchResult.Truncated bool` present in `db/db.go`; both `searchNodesSemantic` and `searchNodesLike` use limit+1 over-fetch pattern.
> - Fallback strategy hint ("scope to a domain then use recall on a related memory and follow its connections") in description.

Fixes three gaps in the `search` tool surface that cause agents to silently miss nodes
that exist, with no way to know results were incomplete.

---

## Motivation

A Claude Desktop instance failed to find a specific node via three different queries.
Investigation identified three compounding gaps:

1. The tool description gives no hint that queries must use vocabulary that appears in
   stored content — LLMs naturally query by intent and get zero results even when the
   node exists.
2. The `SearchResult` has no `truncated` field. With a default limit of 10, a relevant
   node ranked 11th is silently omitted with no signal to the caller.
3. The description gives no fallback strategy for when direct search misses.

Silent misses are especially bad for knowledge tools: the caller has no error to recover
from — it just proceeds without the relevant context.

---

## Changes

### 1. Tool description — vocabulary guidance

Update the `search` tool description to lead with the vocabulary constraint. Current
description begins with a plain summary. New description must include, near the top:

> "Query using words that appear in the stored label, description, or why_matters —
> not words that describe what you are looking for. Use nouns and proper names from
> the domain, not intent phrases."

Example to include in description:
- Wrong: "search for the decision about database indexing"
- Right: "database index performance"

### 2. query property description

Current: `"Search text"`

Replace with something like:
> "Words that appear in the stored content. Use vocabulary from the domain, not
> intent descriptions. Try multiple short queries with different terms if the first
> returns nothing."

### 3. truncated field in SearchResult

In `db/db.go`, update `SearchNodes` to over-fetch `limit+1` results. If the count
returned equals `limit+1`, set `truncated: true` on the result and return only the
first `limit` items. This lets the caller know there are more results without fetching
all of them.

Add `Truncated bool` to the search response struct in `tools/tools.go`. Include it
in the JSON response even when false (so the caller always sees the field).

### 4. Fallback strategy hint in description

After the vocabulary guidance, add a recovery path for when search fails:

> "If search returns nothing: (1) scope to a domain with the domain parameter;
> (2) call recall on a known related node ID and follow its edges; (3) call orient
> to list all nodes and scan by label."

---

## Acceptance criteria

- `search` tool description includes explicit vocabulary constraint guidance.
- `search` tool description includes the fallback strategy (domain-scope → recall →
  orient).
- `query` property description is more than two words — it explains the vocabulary
  constraint.
- `SearchNodes` over-fetches by one and sets `truncated: true` when results are cut.
- JSON response always includes a `truncated` field.
- Test: `TestSearch_TruncatedSignal` — add N+1 nodes, search with limit=N, assert
  `truncated: true` in the response and that exactly N results are returned.
- Test: `TestSearch_NoTruncation` — add N nodes, search with limit=N+1, assert
  `truncated: false`.
- `go test ./...` green.

---

## Files

- `db/db.go` — `SearchNodes` over-fetch logic
- `tools/tools.go` — `search` tool description, `query` property description,
  `SearchResult` response struct with `Truncated` field
- `tools/tools_test.go` — new truncation tests

## References

- shared-surface node: `search-tool-gives-no-vocabulary--639d6489`
