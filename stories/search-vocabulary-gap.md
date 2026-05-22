# Story: Fix `search` tool — vocabulary guidance, truncation signal, and fallback hint

**Discovered:** 2026-05-22  
**Reported by:** Claude Desktop instance  
**Domain:** memoryweb  

---

## Background

A Claude Desktop agent searched for a node about "top 5 AI ideas" using three
different queries:

1. `"product AI ideas submit"` — zero results
2. `"5 AI ideas product submission list"` — zero results
3. `"TerraNova AI hackathon ideas SAQ SMETA benchmarking"` — node not in top 10

The node existed. Its label was `"Decision: top 5 AI ideas source is Mikita's CPH
hackathon prep page"` with tags `ai-ideas top-5 mikita SAQ SMETA benchmarking
scoring`. It was eventually found by domain-scoping with label-adjacent vocabulary
and then traversing edges via `recall`.

All three issues in the bug report are real and reproducible from the code:

---

## Root causes (confirmed)

### 1. Tool description and query property give no vocabulary guidance

**Current `search` description:**
```
Search memories by text across label, description, why_matters, and tags. Only
live entries are returned; ...
```

**Current `query` property description:**
```
"Search text"
```

Neither tells the caller that text search is a *lexical* match — the query string
must contain words that literally appear in `label`, `description`, `why_matters`,
or `tags`. An LLM constructing queries from the *intent* of what it's looking for
("product AI ideas submit") rather than the *vocabulary* of the stored content
will get zero results even when the node exists.

The individual-word OR fallback in `searchNodesLike` does run when the full-phrase
LIKE returns nothing — but "product" and "submit" genuinely don't appear in the
node, so they're no help. The fallback improves recall but can't bridge a
vocabulary gap between query intent and stored content.

Semantic search (Ollama) should handle concept-level queries, but the description
doesn't explain that Ollama must be running for this to apply. A caller without
Ollama gets silent LIKE-only search.

### 2. No truncation signal

**Current `SearchResult` struct:**
```go
type SearchResult struct {
    Nodes []NodeResult `json:"nodes"`
    Edges []Edge       `json:"edges"`
}
```

The default limit is 10 (hard-coded in the handler). The response contains exactly
`min(matches, limit)` nodes with no indication of whether the list was truncated.

In the third search — which used terms matching the node's actual tags — the node
was in the result set but ranked 11th or lower, pushed out of the default 10 by
other nodes with higher recency scores. The caller had no signal that results were
cut off, no way to know that increasing the limit would help.

### 3. No fallback strategy hint

The description doesn't mention domain-scoping + `recall` + edge traversal as a
reliable fallback when direct search misses. This is the strategy that actually
found the node, but the caller had to discover it independently.

---

## Proposed changes

### A — Improve `search` tool description

Add vocabulary guidance, truncation hint, and fallback strategy to the description.
Something like:

> "Queries must use words likely to appear in the stored label, description,
> why_matters, or tags — not words that describe your intent conceptually. If
> results are empty or incomplete, try vocabulary from the node's likely label
> rather than your intent. If results may be truncated, increase limit. If search
> consistently misses, try: scope to a domain, then use recall on a related node
> and follow its connections."
>
> "When Ollama is not running, search is purely lexical (LIKE). Semantic
> (concept-level) matching only applies when Ollama is available."

### B — Improve `query` property description

Change from `"Search text"` to something like:

> "Text to search for. Must contain words that appear in the stored label,
> description, why_matters, or tags. Conceptual paraphrases that don't share
> vocabulary with the stored content will not match."

### C — Add `truncated` flag to `SearchResult`

Add `Truncated bool` to `SearchResult`. Set it to `true` in `searchNodesLike`
and `searchNodesSemantic` when the number of raw matches exceeds `limit`.

This requires a count query (or over-fetching by 1) in both search paths. The
simplest approach: fetch `limit + 1` rows; if `len(rows) > limit`, set
`Truncated = true` and return only the first `limit` rows.

Update the tool description to say: `"Includes truncated: true when results hit
the limit — if so, retry with a higher limit or narrower domain."`.

### D — Update `limit` property description

Change from `"Max results (default 10)"` to:

> "Max results (default 10). If the response includes truncated: true, there are
> more matches — retry with a higher limit or a domain scope."

---

## Implementation order

1. **Implement `truncated` flag** in `db/db.go` — `SearchResult` struct change +
   over-fetch by 1 in both `searchNodesLike` and `searchNodesSemantic`.
2. **Write tests** — `TestSearch_TruncatedFlag_SetWhenLimitExceeded`,
   `TestSearch_TruncatedFlag_NotSetWhenUnderLimit`.
3. **Update tool description** (change A) and property descriptions (changes B, D).
4. Check `TestListTools_PropertyDescriptionsNoForbiddenWords` still passes.
5. Run `go test ./...` — must be green.

---

## Out of scope

- Changing the default limit. 10 is fine; the `truncated` signal makes it safe.
- Changing the LIKE search algorithm or relevance scoring.
- Adding synonym expansion, stemming, or intent-to-vocabulary translation.
- Any changes to the semantic search threshold or Ollama integration.
