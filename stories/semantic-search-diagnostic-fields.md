# semantic search: expose SemanticAttempted and SemanticBestDist diagnostic fields (RC2)

**Status:** DONE

Adds `semantic_attempted` and `semantic_best_dist` to the search response so
agents and operators can distinguish "Ollama was off" from "Ollama ran but
found nothing" — previously both cases returned identical LIKE results with
no signal.

Closes shared-surface issue
`issue-semantic-search-like-fallback-is-agent-invisible-no-best-distance-log-no-signal-when-semantic-path-yields-empty-set-413274cd`.

---

## Motivation

When `searchNodesSemantic` returns an empty set it silently calls
`searchNodesLike`. The response carries no `semantic_distance` fields and no
indication that the semantic path was exercised. Server logs fire a line
(added in the RC1+RC3 story) but that information is invisible to callers.

Agents see LIKE results with no distance — identical to what they see when
Ollama is off. No way to distinguish:

- Bedrock/Ollama unavailable → semantic path never entered
- Semantic path entered, zero results within threshold → LIKE fallback taken
- Semantic path entered, results returned → vector ranking active

---

## Design

`SearchResult` gains two new JSON fields:

```go
SemanticAttempted bool     `json:"semantic_attempted,omitempty"`
SemanticBestDist  *float64 `json:"semantic_best_dist,omitempty"`
```

| Path taken | SemanticAttempted | SemanticBestDist |
|---|---|---|
| Embedding unavailable (Ollama off) | false / absent | nil / absent |
| Semantic path entered, results returned | true | dist of top-ranked node |
| Semantic path entered, zero results → LIKE fallback | true | best dist scanned (nil if no embedding rows at all) |

Both `leanSearchResult` and `digestSearchResult` (the MCP tool response
envelopes) propagate the fields. `toLeanSearchResult` and
`toDigestSearchResult` were updated in the same commit.

Note on `SemanticBestDist` when neighbourhood filtering is active:
`bestDist` is captured from the embedding scan before the `allowedIDs` filter
is applied. A non-nil `SemanticBestDist` alongside LIKE-only returned nodes
means the close match exists outside the requested neighbourhood — not that
the semantic result is wrong.

---

## Changes

| File | Change |
|------|--------|
| `db/search.go` | `SearchResult` struct gains `SemanticAttempted` + `SemanticBestDist`; both return paths in `searchNodesSemantic` populate them |
| `tools/lean.go` | `leanSearchResult` and `digestSearchResult` gain matching fields; `toLeanSearchResult` and `toDigestSearchResult` propagate them |
| `db/search_test.go` | `TestSearchNodesSemantic_DiagnosticFields` — three subtests: semantic_hit, semantic_miss_like_fallback, lexical_only_embed_unavailable |

---

## Acceptance criteria

- `SearchResult` gains `SemanticBestDist *float64` and `SemanticAttempted bool`, serialised as `semantic_best_dist` and `semantic_attempted` in JSON
- When semantic search returns results: `SemanticAttempted=true`, `SemanticBestDist` set to distance of top-ranked node
- When semantic search yields zero results (LIKE fallback): `SemanticAttempted=true`, `SemanticBestDist` set to best observed distance (nil if no embedding rows exist)
- When embedding unavailable (Ollama off / sqlite-vec absent): `SemanticAttempted=false`, `SemanticBestDist` nil
- Existing diagnostic log line preserved: `[memoryweb] semantic search: no results (best dist X.XXX); falling back to text search`
- `TestSearchNodesSemantic_DiagnosticFields` covers all three cases
- All existing db and tools tests pass

---

## Commits

- `1d0d3cd` feat(db): expose SemanticAttempted and SemanticBestDist in SearchResult (STORY-408)
- `b08f358` fix(db,tools): STORY-408 code-review fixes — propagate diagnostic fields through MCP layer
