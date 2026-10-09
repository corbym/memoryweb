# fix: stale embeddings after revise (INSERT OR REPLACE rejected by vec0)

**Status:** DONE

Fixes a silent data-corruption bug where editing a node with `revise` left its
vectors permanently stale. Reported by Mikita (2026-10-07) on v1.56.1 with 1408
live nodes, all embedded.

---

## Root cause

`storeEmbedding` and `storeFieldEmbeddings` used `INSERT OR REPLACE INTO` on
sqlite-vec `vec0` virtual tables. sqlite-vec rejects the conflict-resolution
mechanism with:

```
UNIQUE constraint failed on node_embeddings primary key
UNIQUE constraint failed on node_whymatters_embeddings primary key
UNIQUE constraint failed on node_label_embeddings primary key
```

The error is logged to stderr and silently ignored — `revise` returns success,
`doctor` still reports 100% embedded, and the old vectors stay in all three
tables. Any node edited after its first embedding has stale vectors forever until
`backfill --force` is run.

The same failure could occur in `BackfillEmbeddings` when a concurrent revise
writes between the query and the insert.

---

## Fix

Replace `INSERT OR REPLACE INTO` with a transactional `DELETE` + `INSERT` in
both `storeEmbedding` and `storeFieldEmbeddings`. Each field gets its own
transaction in `storeFieldEmbeddings` to preserve the existing per-field
independence semantics (a failure on one field does not roll back the others).

`clearEmbeddings` was also made transactional (all three DELETEs in one
transaction) to be consistent with `ClearFieldEmbeddings`.

---

## Secondary fix: `backfill --force` now rebuilds all three tables

`backfill --force` previously called `ClearFieldEmbeddings` — clearing only
`node_label_embeddings` and `node_whymatters_embeddings`, leaving the combined
`node_embeddings` table intact. Nodes with stale combined embeddings (every node
edited since it was first embedded) could not be recovered this way.

`--force` now calls `ClearAllEmbeddings` (new exported method), which atomically
deletes all three tables before the backfill runs.

---

## Changes

| File | Change |
|------|--------|
| `db/embeddings.go` | `storeEmbedding` — `INSERT OR REPLACE` → transactional `DELETE + INSERT` |
| `db/embeddings.go` | `storeFieldEmbeddings` — `INSERT OR REPLACE` → per-field transactional `DELETE + INSERT` |
| `db/embeddings.go` | `clearEmbeddings` — wrapped all three DELETEs in a single transaction |
| `db/embeddings.go` | `ClearAllEmbeddings()` — new exported method (wraps `clearEmbeddings`) |
| `main.go` | `backfillCmd` — `--force` calls `ClearAllEmbeddings` instead of `ClearFieldEmbeddings`; updated help text |
| `db/embeddings_test.go` | `TestUpdateNode_ReEmbedsReplacesPreviousEmbedding` — regression: verifies blob is updated in `node_embeddings` after label change |
| `db/embeddings_test.go` | `TestUpdateNode_ReEmbedsReplacesPreviousFieldEmbeddings` — same regression for `node_label_embeddings` and `node_whymatters_embeddings` |

---

## Recovery for existing deployments

Anyone on v1.56.1 with Ollama running who has edited nodes since their first
backfill should run:

```sh
memoryweb backfill --force
```

This clears and rebuilds all three embedding tables. The fix ships as v1.57.0.

---

## Acceptance criteria

- After `UpdateNode` with a changed label, the blob in `node_embeddings` reflects the new text
- After `UpdateNode` with a changed label/why_matters, the blobs in `node_label_embeddings` and `node_whymatters_embeddings` reflect the new text
- `TestUpdateNode_ReEmbedsReplacesPreviousEmbedding` passes (was failing before fix)
- `TestUpdateNode_ReEmbedsReplacesPreviousFieldEmbeddings` passes (was failing before fix)
- `backfill --force` clears all three embedding tables before rebuilding
- All existing tests pass: `go test ./...`
