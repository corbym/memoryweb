# memoryweb: fix four backfill correctness bugs (RC1+RC3 code review)

**Status:** DONE — fixed in commit `a519fe3`

Four correctness bugs in `BackfillEmbeddings` / `embedAndStoreFields` /
`ClearFieldEmbeddings` found during the code review of the RC1+RC3
semantic-search work (commits `f972854` / `fda462e`).

---

## Bugs

### CR-1 — `embedAndStoreFields` reports partial success as complete

`db/embeddings.go:363`

When `embed()` fails for one field (e.g. an intermittent Ollama error on the
`why_matters` call), the failing field is silently omitted from `fieldEmbs`.
`storeFieldEmbeddings` is then called with only the successful field, stores
it, and returns `true` — "all *supplied* fields stored". `embedAndStoreFields`
propagates that `true`, but the node still has no `why_matters` embedding.

The caller (`backfillFieldEmbeddings`) increments `count` and the node is
reported as done. Within the same `BackfillEmbeddings` run the supplementary
pass will immediately re-find the node (its `we.node_id IS NULL`), potentially
double-counting it. In subsequent runs it will be retried from the correct
state, so data is never permanently lost — but the single-run count is wrong
and the node silently misses one field for the duration of that run.

**Fix:** return false (or a partial-success error) when fewer fields were
stored than were supplied.

---

### CR-2 — Single node counted twice in `BackfillEmbeddings`

`db/embeddings.go:344`

Consequence of CR-1: main loop embeds node A's legacy vector (`count++`),
partially succeeds for per-field (label stored, `why_matters` not).
Supplementary pass later finds A (still missing `whymatters_embeddings` row),
successfully embeds both fields, increments count again.
`BackfillEmbeddings` returns `2` for `1` physical node. `runBackfill` prints
`"Backfilled 2 node(s)"` when only 1 node was processed.

`TestBackfillEmbeddings_NoDuplicateCountWhenFieldFails` only covers the case
where per-field fails in *both* passes; the partial-success path is untested.

**Fix:** track processed node IDs and deduplicate the final count; add a test
for the partial-success case.

---

### CR-3 — `"All embeddings are up to date"` when supplementary pass fails

`main.go:464`

`progressFired` is set only when the main-loop progress callback fires (i.e.
only when there are `candidates` — nodes with no legacy embedding). When all
nodes already have legacy embeddings but lack per-field ones (pre-v16
install), `len(candidates) == 0`, the progress callback never fires, and
`progressFired` stays `false`.

The supplementary pass runs, finds nodes, calls Ollama, fails (Ollama is
down), and returns `0`. `n == 0` and `progressFired == false` → the `default`
branch prints `"All embeddings are up to date."` rather than the
Ollama-unavailable warning. A pre-v16 user running `backfill` with Ollama
stopped gets a false assurance.

**Fix:** track whether the supplementary pass had candidates separately from
the main-loop progress callback; emit the correct warning when it had
candidates but wrote zero embeddings.

---

### CR-4 — `ClearFieldEmbeddings` not wrapped in a transaction

`db/embeddings.go:380`

If `DELETE FROM node_label_embeddings` succeeds but
`DELETE FROM node_whymatters_embeddings` fails (disk-full, I/O error), the
method returns an error and `backfillCmd` exits with `os.Exit(1)`. The
database is now in an inconsistent state: label embeddings cleared,
whymatters embeddings intact. Any semantic search in that window will get
asymmetric results. Re-running `--force` recovers the state.

**Fix:** wrap both DELETEs in a single transaction.

---

## Acceptance criteria

- `embedAndStoreFields` returns `false` when fewer fields were stored than supplied (partial embed failure)
- `BackfillEmbeddings` counts at most one increment per physical node, even when the main loop and supplementary pass both touch the same node
- When the supplementary-only pass has candidates but writes zero embeddings (Ollama unavailable), `runBackfill` prints the Ollama-unavailable warning, not `"All embeddings are up to date."`
- `ClearFieldEmbeddings` wraps both DELETEs in a transaction; a failure leaves both tables untouched
- `TestBackfillEmbeddings_NoDuplicateCountWhenFieldFails` covers the partial-success double-count path
- `go test ./...` green
