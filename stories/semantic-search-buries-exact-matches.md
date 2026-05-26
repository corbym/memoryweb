# Semantic Search Buries Exact Substring Matches

When Ollama is running, `search` silently ignores exact substring matches when
semantic ranking returns any results within the relevance threshold. A unique
token like a ticket number (`TNOVA-231`) that would match exactly under LIKE is
swamped by semantically similar nodes, and the correct node is never surfaced.

---

## Motivation

A Claude agent needed to find the node for `TNOVA-231` to archive it. It queried:

```
search("TNOVA-231 conflict minerals", domain="sedex")
```

Ollama was running, so `searchNodesSemantic` ran. All completed-ticket nodes in
`sedex` are semantically adjacent — they share the same vocabulary space (ticket
numbers, project names, status words). Several nodes came back within the
`semanticDistanceThreshold = 0.3` cosine window, with `TNOVA-228` (the
most-connected ticket node) ranking highest because its embedding was marginally
closer. The exact node `TNOVA-231` was not in the results at all.

The LIKE fallback in `searchNodesLike` was never triggered. The guard condition
is:

```go
if len(results) == 0 {
    return s.searchNodesLike(query, domain, limit)
}
```

Because semantic results existed, the LIKE path was skipped entirely — even
though `"TNOVA-231"` is a unique string present verbatim in exactly one node's
label.

The agent's workaround was to use `history` with a known date range, which
surfaced the correct node immediately.

---

## Root cause

`searchNodesSemantic` treats semantic results as definitive. Once any node
clears the `semanticDistanceThreshold`, the LIKE path is dead. There is no
mechanism to express "I know this exact string is in the label — bypass
semantic ranking".

The threshold itself (`0.3`) is calibrated for concept-level recall, not
identifier lookup. Ticket numbers, short IDs, and other unique codes all live
in dense semantic neighbourhoods with many structurally similar nodes.

---

## Changes

### 1. Add `exact` parameter to `search` tool

Add a boolean `exact` parameter to the `search` tool definition in
`tools/tools.go`:

```go
"exact": {
    Type: "boolean",
    Description: "When true, bypass semantic ranking and use pure substring (LIKE) matching only. Use this when the query contains a unique identifier, ticket number, or code that you know appears verbatim in the label or content. Semantic scoring actively harms recall for exact lookups.",
},
```

Wire it through `searchNodes` in `tools/tools.go`:

```go
var a struct {
    Query  string `json:"query"`
    Domain string `json:"domain"`
    Limit  int    `json:"limit"`
    Exact  bool   `json:"exact"`
}
```

When `Exact` is true, call `searchNodesLike` directly, skipping the semantic
path entirely:

```go
if a.Exact {
    nodes, err = h.store.SearchNodesLike(a.Query, a.Domain, a.Limit)
} else {
    nodes, err = h.store.SearchNodes(a.Query, a.Domain, a.Limit)
}
```

`searchNodesLike` is already exported (lowercase in the receiver), so expose it
as `SearchNodesLike` or route via `SearchNodes` with a flag. Choose whichever
keeps the `Store` API clean.

### 2. Update `search` tool description

Append to the description (after the existing Ollama note):

> "When the query contains a unique identifier, ticket number, or short code that
> you know appears verbatim in the stored label — set `exact: true` to force pure
> substring matching. Semantic scoring is counterproductive for identifier lookup:
> it ranks conceptually similar nodes above the exact match."

Also update the `query` property description to hint at this:

> "Terms to search for. Must use vocabulary that appears in the stored label,
> description, why_matters, or tags. For unique identifiers or ticket numbers
> known to appear verbatim, also set exact: true."

### 3. (Optional / lower priority) Hybrid merge in `searchNodesSemantic`

An alternative or complementary fix: after collecting semantic results, run a
LIKE check on the original query. If LIKE returns any nodes not already present
in the semantic results, inject them at the front of the result list. This would
make exact matches "win" automatically without requiring the caller to know to
set `exact: true`.

This is lower priority than the explicit flag because the flag is predictable
and auditable. Automatic merging has edge cases (duplicate suppression, limit
arithmetic) and changes ranking semantics in ways that may surprise.

---

## Test cases

- `search("TNOVA-231 conflict minerals", domain="sedex")` with Ollama running
  and multiple semantically similar ticket nodes present: the node whose label
  contains `TNOVA-231` must appear in results.
- `search("TNOVA-231", exact=true)`: returns only LIKE matches; no
  `semantic_distance` field on results.
- `search("TNOVA-231", exact=false)` (explicit false): behaves identically to
  the current default (semantic when Ollama is up).
- `search("conflict minerals")` with no `exact`: existing semantic behaviour
  unchanged.
