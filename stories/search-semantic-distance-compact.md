# search: preserve semantic_distance in compact (2+ result) rendering

**Status:** COMPLETE — (commit de5044e)

Fixes `search` dropping `semantic_distance` from results when there are 2 or more
matches. The score is currently only included in the single-result full-object response
— the case where it matters least. In multi-result compact rendering, agents have no
distance signal to distinguish a close match from a weak one. Closes shared-surface
finding `finding-search-drops-semantic-distance-in-the-2-result-compact-rendering-the-score-survives-only-where-it-is-useless-0ec8a3ef` (state=contested).

---

## Motivation

`search` returns results in two formats:

- **1 result** → full JSON object (all fields, including `semantic_distance`)
- **2+ results** → compact text lines: `"[id] label — excerpt (domain, node_kind)"`

`semantic_distance` is only present in the single-result case. In multi-result
responses — the common case — agents cannot distinguish a highly relevant result
(distance 0.05) from a weak match (distance 0.45). The primary purpose of the
`semantic_distance` field is ranking disambiguation in multi-hit result sets, so the
current behaviour inverts its utility: the score is present precisely where it is not
needed, and absent where it is.

The fix is to append the distance to compact lines when semantic search was used:

```
[abc1] Decision: use WAL mode — WAL mode required for concurrent readers (memoryweb-meta, decision)  0.12
[def2] SQLite concurrency model — readers never block writers in WAL mode (memoryweb-meta, finding)  0.38
```

---

## Design

### Compact line format (with distance)

```
[id] label — excerpt (domain, node_kind)  dist
```

Distance is appended right-aligned after two spaces, formatted to 2 decimal places.
Only present when the result carries a non-zero `semantic_distance` (i.e., when Ollama
was used). LIKE-only results omit it (distance is 0 or absent).

### Single-result behaviour

No change: single result continues to return the full object including `semantic_distance`.

### Result struct

`SearchResult` (or its lean equivalent in `tools/lean.go`) already carries
`SemanticDistance float64`. The compact renderer in `tools/search.go` (or wherever
lean lines are built) must include it.

---

## Changes

### 1. `tools/lean.go` — extend `toLeanEntry` / compact line builder

Locate where compact lines are formatted (the `"[%s] %s — %s (%s, %s)"` pattern).
Append distance when non-zero:

```go
if e.SemanticDistance > 0 {
    return fmt.Sprintf("[%s] %s — %s (%s, %s)  %.2f",
        e.ID, e.Label, excerpt, e.Domain, e.NodeKind, e.SemanticDistance)
}
return fmt.Sprintf("[%s] %s — %s (%s, %s)", e.ID, e.Label, excerpt, e.Domain, e.NodeKind)
```

### 2. `tools/definitions.go` — update `search` description

In the description, where it documents the compact line format, add:

> When semantic search is active, each line appends the distance score (e.g. `0.12`)
> so you can distinguish close matches from weak ones.

### 3. `docs/memoryweb-skill.md` — Layer 2 quick reference

Update the `search` tool entry to note that multi-result compact lines include
`semantic_distance` when Ollama is running.

### 4. Tests

**`tools/search_test.go`**:
- `TestSearch_CompactLinesIncludeDistance` — insert two nodes with distinct labels;
  search with semantic results mocked to return distinct distances; assert both compact
  lines contain the distance formatted to 2 decimal places.
- `TestSearch_CompactLinesNoDistanceWhenZero` — mock results with distance 0 → lines
  do NOT contain a trailing number (LIKE-only fallback path).
- `TestSearch_SingleResultFullObject` — single result still returns full object with
  `semantic_distance` field present (regression guard).

Note: the existing semantic search tests mock Ollama; follow the same mock pattern
in `tools/search_test.go` (or `db/search_test.go`) to inject controlled distances.

---

## Acceptance criteria

- Multi-result compact lines include `  0.12` (2 decimal places) when semantic search
  is active and distance is non-zero.
- LIKE-only results (distance = 0 or absent) do not include a trailing number.
- Single-result full-object response continues to include `semantic_distance` field.
- `docs/memoryweb-skill.md` updated to document the new compact line format.
- `TestSearch_CompactLinesIncludeDistance` passes.
- `TestSearch_CompactLinesNoDistanceWhenZero` passes.
- `TestSearch_SingleResultFullObject` passes (regression guard).
- Before merging: update `docs/memoryweb-skill.md` Layer 2 `search` entry to document the distance score in compact lines. Check skill-sync standing rule (`before-shipping-any-tool-schema-parameter-or-description-change...`).
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `tools/lean.go` | Append distance to compact line when non-zero |
| `tools/definitions.go` | Update `search` description for compact line format |
| `docs/memoryweb-skill.md` | Layer 2: note distance in multi-result compact lines |
| `tools/search_test.go` | 3 new test cases |

---

## References

- Shared-surface finding: `finding-search-drops-semantic-distance-in-the-2-result-compact-rendering-the-score-survives-only-where-it-is-useless-0ec8a3ef` (state=contested)
- Related: `tools/lean.go` — compact line builder
- Related: `db/search.go` — `SearchNodes`, `SemanticDistance` field on results
- Before shipping: re-check `docs/memoryweb-skill.md` against tool surface changes per
  the skill-sync standing rule (`before-shipping-any-tool-schema-parameter-or-description-change-check-it-against-docs-memoryweb-skill-md-and-update-the-skill-if-anything-in-it-is-now-stale-connect-each-such-change-back-here-1f87b2c3`)
