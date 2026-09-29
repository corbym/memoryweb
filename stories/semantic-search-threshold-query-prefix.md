# semantic search: fix threshold and add model-keyed query prefix (RC1+RC3)

**Status:** DONE

Removes the absolute `0.3` cosine-distance cutoff from `searchNodesSemantic`
and adds a model-keyed query prefix for asymmetric embedding models
(arctic-embed, mxbai-embed-large). These two changes **must land together**:
adding the prefix alone raises absolute distances to 0.54–0.64, which makes
the hard cutoff fire universally. Removing the cutoff alone improves recall
but leaves ranking broken for asymmetric models.

Closes shared-surface findings
`finding-semantic-search-0-3-absolute-threshold-silently-evicts-all-results-on-long-node-graphs-nearest-hit-0-345-0-438-for-263-word-median-nodes-with-arctic-embed-a3ecb048` (RC1)
and
`finding-embed-sends-raw-query-without-arctic-embed-asymmetric-inference-prefix-ranking-collapses-to-hub-nodes-without-it-781c784b` (RC3),
and resolves shared-surface issue
`issue-semantic-search-like-fallback-is-agent-invisible-no-best-distance-log-no-signal-when-semantic-path-yields-empty-set-413274cd`.

---

## Motivation

### RC1 — Hard-coded 0.3 threshold

`db/search.go:139`:

```go
const semanticDistanceThreshold = 0.3
```

`searchNodesSemantic` fetches rows `ORDER BY dist ASC` and breaks at the first
row exceeding the threshold. When the result set is empty it silently calls
`searchNodesLike`. There is no log of the best distance observed, no env
override, and no signal in the response to indicate the semantic path was
exercised.

Measured on a 1218-node graph with arctic-embed (default model), median node
text 263 words: nearest hit for any natural-language query was **0.345–0.438**.
The semantic path is never exercised on this graph. Against label-only
documents (the short-node style this codebase exercises in tests), keyword
queries land at 0.11–0.12 — which is why the threshold went unnoticed.

### RC3 — No query-side prefix for asymmetric models

`db/search.go:43`:

```go
embedding, err := embed(query)
```

`snowflake-arctic-embed` is trained asymmetrically: documents are indexed bare,
but queries need `"Represent this sentence for searching relevant passages: "`
prepended. Without the prefix, the intended node ranked 24th–595th across
probes; with it, 1st or 2nd for 5 of 6 probes. The same convention applies to
`mxbai-embed-large`; `bge-m3` requires no prefix.

Stored document embeddings are correct as-is (bare = correct for documents).
No backfill is needed — only the query call path changes.

### Why they must land together

| State | Effect |
|-------|--------|
| Current (no prefix, 0.3 cutoff) | Semantic set empty for long-node graphs |
| Add prefix only | Distances rise to 0.54–0.64; semantic set even more universally empty |
| Remove cutoff only | Recall improves; ranking broken for arctic-embed (hub collapse) |
| Both together | Correct ranking + no artificial result floor |

---

## Design

### Threshold replacement

Remove the early-break guard and return all rows up to `limit` ordered by
distance. The `fetch = limit + 1` pattern for truncation detection is
unchanged.

```go
// Before: early-exit guard
if dist > semanticDistanceThreshold {
    break
}

// After: remove the guard entirely; results are already ORDER BY dist ASC
```

Optionally expose `MEMORYWEB_SEMANTIC_THRESHOLD` as an env var so operators
with unusual models can still cap results. If provided, apply it as the
existing break; if absent (the new default), no cap.

### Query prefix

Add `queryPrefix()` in `db/embeddings.go`:

```go
// queryPrefix returns the inference-time prefix for the current embedding
// model. arctic-embed and mxbai-embed-large are asymmetric: documents are
// indexed bare, queries need this prefix prepended. bge-m3 and unknown models
// use no prefix.
func queryPrefix() string {
    switch embeddingModel() {
    case "snowflake-arctic-embed", "snowflake-arctic-embed:latest",
         "mxbai-embed-large", "mxbai-embed-large:latest":
        return "Represent this sentence for searching relevant passages: "
    default:
        return ""
    }
}
```

Apply in `SearchNodes` only (the query path):

```go
// db/search.go — SearchNodes
queryText := queryPrefix() + query
embedding, err := embed(queryText)
```

`embedTextForNode` is unchanged — documents never get the prefix.

### Diagnostic log

When the semantic set is empty (after removing the cutoff, this means the
query embedded successfully but returned zero rows — e.g. no embeddings exist
yet), log the best distance observed:

```go
if len(results) == 0 {
    if len(allDists) > 0 {
        log.Printf("[memoryweb] semantic search: no results (best dist %.3f); falling back to text search", allDists[0])
    }
    return st.searchNodesLike(query, domain, limit, allowedIDs, nodeKinds, true)
}
```

Collect `allDists` during the scan loop before the threshold check is removed,
or simply retain the first `dist` value seen.

---

## Changes

### 1. `db/search.go`

- Remove `const semanticDistanceThreshold = 0.3` (or gate it behind
  `MEMORYWEB_SEMANTIC_THRESHOLD`).
- Remove the `if dist > semanticDistanceThreshold { break }` early-exit in
  `searchNodesSemantic`.
- Apply `queryPrefix()` to the query text before calling `embed()` in
  `SearchNodes`.
- Add best-distance log when the semantic set is empty.

### 2. `db/embeddings.go`

- Add `queryPrefix() string` function with the model-keyed switch.

### 3. `db/search_test.go`

- `TestSearchNodesSemantic_TopKNoThreshold` — verify that results beyond the
  old 0.3 cutoff are now returned when embeddings exist.
- `TestQueryPrefix_ArcticEmbed` — assert the correct prefix string for
  `snowflake-arctic-embed` and `snowflake-arctic-embed:latest`.
- `TestQueryPrefix_BGE` — assert empty prefix for `bge-m3`.
- `TestQueryPrefix_Unknown` — assert empty prefix for an unknown model name.

> **Note on test fixtures:** current test nodes are label-sized and pass easily
> under the old threshold. The new tests for top-k behaviour need nodes whose
> embedded text produces distances in the 0.3–0.6 range with realistic queries.
> Use `MEMORYWEB_OLLAMA_ENDPOINT=disabled` for unit tests; integration tests
> require a live Ollama instance (skip with `t.Skip` when unavailable).

---

## Acceptance criteria

- A search on a graph of 250+ word nodes returns semantic results (non-nil
  `semantic_distance` on at least one hit) when Ollama is running.
- With Ollama disabled (`MEMORYWEB_OLLAMA_ENDPOINT=disabled`), behaviour is
  unchanged — falls back to LIKE.
- When `MEMORYWEB_SEMANTIC_THRESHOLD` is set, it acts as the cutoff; when
  unset, no cutoff is applied.
- `queryPrefix()` returns the correct prefix for `snowflake-arctic-embed`,
  `snowflake-arctic-embed:latest`, `mxbai-embed-large`, `mxbai-embed-large:latest`,
  and `""` for `bge-m3` and any unknown model.
- The query prefix is applied to the `embed()` call in `SearchNodes` only —
  `embedTextForNode` and `BackfillEmbeddings` are unchanged.
- A debug log line at `[memoryweb] semantic search: no results (best dist X)`
  fires when the semantic set is empty and at least one distance was observed.
- All four new `TestQueryPrefix_*` and `TestSearchNodesSemantic_TopKNoThreshold`
  cases pass.
- No existing tests broken.
- `go test ./...` green.
- Before merging: update `docs/memoryweb-skill.md` if the search behaviour
  description references the 0.3 threshold.

---

## Files

| File | Change |
|------|--------|
| `db/search.go` | Remove threshold constant and early-exit guard; apply `queryPrefix()`; add diagnostic log |
| `db/embeddings.go` | Add `queryPrefix() string` |
| `db/search_test.go` | 4–5 new test cases |

---

## References

- Shared-surface finding RC1: `finding-semantic-search-0-3-absolute-threshold-silently-evicts-all-results-on-long-node-graphs-nearest-hit-0-345-0-438-for-263-word-median-nodes-with-arctic-embed-a3ecb048`
- Shared-surface finding RC3: `finding-embed-sends-raw-query-without-arctic-embed-asymmetric-inference-prefix-ranking-collapses-to-hub-nodes-without-it-781c784b`
- Shared-surface issue (silent fallback): `issue-semantic-search-like-fallback-is-agent-invisible-no-best-distance-log-no-signal-when-semantic-path-yields-empty-set-413274cd`
- memoryweb-meta story node: `story-fix-semantic-search-threshold-and-query-prefix-for-asymmetric-models-rc1-rc3-674d8298`
- Source: `db/search.go:139` (threshold constant), `db/search.go:43` (embed call), `db/embeddings.go:106` (embedTextForNode)
