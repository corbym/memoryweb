# semantic search: per-field node embedding (RC2)

**Status:** DONE

Replaces `embedTextForNode`'s single concatenated string with separate
embeddings for `label`, `why_matters`, and `description`. Queries are scored
against all three vectors and ranked by the minimum distance. This fixes the
silent truncation of `why_matters` caused by arctic-embed's 512-token window.

Prerequisite: `stories/semantic-search-threshold-query-prefix.md` (RC1+RC3)
should land first, since this story produces distances in the same 0.3–0.5
range that the threshold fix makes usable.

Closes shared-surface finding
`finding-embedtextfornode-concatenates-all-three-fields-and-overflows-arctic-embed-512-token-window-why-matters-never-embedded-for-263-word-nodes-fbdc8541` (RC2).

---

## Motivation

`db/embeddings.go:106`:

```go
func embedTextForNode(label, description, whyMatters string) string {
    return label + " " + description + " " + whyMatters
}
```

arctic-embed's context window is 512 tokens. For a 263-word median node,
`label + description` already reaches 465 tokens, leaving 47 tokens of a
49-token `why_matters`. For nodes above the median the field is cut entirely.

The field the README and shared-surface contract call "the most important for
retrieval" is the first to be dropped when the window is exceeded.

Measured distances for the three embed strategies on 263–428 word nodes with
plain queries:

| Strategy | Keyword queries | Question queries |
|----------|-----------------|-----------------|
| Current (concatenated, truncated) | 0.26–0.42 | 0.45–0.50 |
| Label only | 0.11–0.12 | 0.18–0.33 |
| why_matters only | 0.12–0.37 | 0.25–0.39 |
| Label + why_matters | 0.21–0.34 | 0.40–0.50 |
| **Min(label, why_matters)** | **0.11–0.12** | **0.18–0.33** |

The minimum over label and why_matters dominates. Reordering the concatenation
(why_matters first) was measured and makes no meaningful difference (0.44–0.47
vs 0.45–0.50 for questions).

---

## Design

### Option A: two vectors per node (label + why_matters) — recommended first step

Embed `label` and `why_matters` separately. Skip `description` — it is the
longest field and the measurements show it does not improve recall beyond the
other two. This avoids tripling Ollama call count at write time.

Schema: add a `label_embedding` column to `node_embeddings`, or store two rows
per node with a `field` discriminator column. A `field` discriminator is more
extensible:

```sql
-- New virtual table (or extend existing with a field column)
CREATE VIRTUAL TABLE node_embeddings_v2 USING vec0(
    node_id TEXT NOT NULL,
    field   TEXT NOT NULL,  -- 'label' | 'why_matters'
    embedding float[1024]
);
```

At query time, score each field vector against the query embedding and take
the minimum distance per node before ranking.

### Option B: label-only embedding — lighter mitigation

Embed only `label`. Label alone brings keyword queries to 0.11–0.12, the
best single-field result. It drops question-query coverage vs why_matters
(0.18–0.33 for why_matters-only on questions). This is a schema-free
incremental improvement: `embedTextForNode` just returns `label`.

Option B is a viable first step if Option A's schema migration is deferred.

### doctor diagnostic

Add a warning to the `doctor` subcommand when any stored node's
`label + ' ' + description + ' ' + why_matters` would exceed the model's
`context_length` from `/api/show`. Sample the longest 10% of nodes and report
how many would overflow. This surfaces the truncation problem for operators
without requiring the full per-field migration.

---

## Changes (Option A)

### 1. `db/migrations.go` — migration v16

Add a new virtual table `node_embeddings_fields`:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS node_embeddings_fields USING vec0(
    node_id TEXT NOT NULL,
    field   TEXT NOT NULL,
    embedding float[1024]
);
CREATE INDEX IF NOT EXISTS idx_nef_node ON node_embeddings_fields(node_id);
```

Keep `node_embeddings` for backward compatibility during the transition.
`BackfillEmbeddings` writes to both until `node_embeddings` is retired in a
future migration.

### 2. `db/embeddings.go`

- Replace `embedTextForNode` with `embedFieldsForNode(label, description, whyMatters string) map[string]string`
  that returns `{"label": label, "why_matters": whyMatters}` (Option A) or
  `{"label": label}` (Option B).
- Add `storeFieldEmbeddings(id string, fields map[string][]float32) bool`.
- Update `BackfillEmbeddings` to call both the old `storeEmbedding` (for
  backward compat) and `storeFieldEmbeddings`.
- Add `AddNode` / `UpdateNode` write path to call `storeFieldEmbeddings`
  alongside the existing `storeEmbedding` call.

### 3. `db/search.go` — `searchNodesSemantic`

Replace the single `vec_distance_cosine(e.embedding, ?)` join with a subquery
that takes the minimum distance across fields:

```sql
SELECT n.id, ...,
       MIN(vec_distance_cosine(ef.embedding, ?)) AS dist
FROM node_embeddings_fields ef
JOIN nodes n ON n.id = ef.node_id
WHERE n.archived_at IS NULL
  [domain / nodeKind filters]
GROUP BY n.id
ORDER BY dist ASC
LIMIT ?
```

Keep `node_embeddings` as a fallback for nodes not yet migrated (nodes
embedded before the migration, or nodes on older binary versions).

### 4. `main.go` — `doctor` subcommand

Add a `context_length` check: fetch the model's `context_length` from Ollama
`/api/show`, sample the top-10% longest nodes by field byte length, and report
any that would overflow.

### 5. `db/embeddings_test.go`

- `TestEmbedFieldsForNode_ReturnsLabelAndWhyMatters` — verify two embeddings
  are stored per node with the field discriminator.
- `TestSearchNodesSemantic_PerFieldMinDistance` — integration test: a
  node whose `why_matters` is the relevant field should rank above a node
  whose `label` matches but whose `why_matters` is off-topic.

---

## Acceptance criteria

- After backfill with the new binary, each node has two rows in
  `node_embeddings_fields` (field = `label` and `why_matters`).
- A search query semantically matching a node's `why_matters` (not its label)
  returns that node in the top-3 results.
- Nodes not yet migrated (still only in `node_embeddings`) continue to work
  via the fallback join.
- `doctor` reports a warning when nodes exist whose field text would overflow
  the model's `context_length` from `/api/show`.
- All new embedding and search tests pass.
- `go test ./...` green.
- Before merging: update `docs/memoryweb-skill.md` if backfill instructions
  reference the embedding strategy.

---

## Files

| File | Change |
|------|--------|
| `db/migrations.go` | v16: `node_embeddings_fields` virtual table |
| `db/embeddings.go` | `embedFieldsForNode`, `storeFieldEmbeddings`, updated `BackfillEmbeddings` |
| `db/nodes.go` | `AddNode` / `UpdateNode` write both embedding tables |
| `db/search.go` | `searchNodesSemantic`: min-distance subquery over `node_embeddings_fields` |
| `main.go` | `doctor`: context_length overflow warning |
| `db/embeddings_test.go` | 2 new test cases |

---

## References

- Shared-surface finding RC2: `finding-embedtextfornode-concatenates-all-three-fields-and-overflows-arctic-embed-512-token-window-why-matters-never-embedded-for-263-word-nodes-fbdc8541`
- memoryweb-meta story node: `story-per-field-node-embedding-separate-vectors-for-label-why-matters-description-rc2-41e72e14`
- Recordari parallel finding: `finding-recordari-embeds-all-4-fields-not-label-only-retrieval-gap-vs-memora-and-security-implication-61e95fae`
- Prerequisite story: `stories/semantic-search-threshold-query-prefix.md`
- Source: `db/embeddings.go:106` (embedTextForNode), `db/search.go:162` (semantic query)
