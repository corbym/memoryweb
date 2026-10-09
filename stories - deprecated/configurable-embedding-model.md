# Configurable embedding model via MEMORYWEB_EMBED_MODEL

**Status:** DONE

**Recordari node:** `story-configurable-embedding-model-via-memoryweb-embed-model-env-var-8cff3259`

**GitHub issue:** #37 (XuanYue)

---

## Why

`snowflake-arctic-embed` is hardcoded across `db/embeddings.go`, the `setup`
subcommand, the `backfill` subcommand, and the `doctor` command. It is an
English-optimised model. Users working in Chinese, Japanese, Korean, or other
non-Latin-script languages get no benefit from semantic search — short LIKE
matches still fire, but longer semantic queries return nothing.

The fix is a single env var: `MEMORYWEB_EMBED_MODEL`. The default remains
`snowflake-arctic-embed` so existing databases are unaffected. Users who need
multilingual support set the var and point it at `bge-m3` or any other
Ollama-hosted 1024-dimensional model.

---

## The 1024-dimension constraint

sqlite-vec virtual tables are defined with a **fixed vector dimension** at
`CREATE TABLE` time. Migration 9 created `node_embeddings` at 1024 dimensions.
Inserting a vector of a different length fails at the sqlite-vec layer with a
constraint error.

Changing the dimension requires dropping and recreating the virtual table —
a schema migration that also destroys all existing embeddings. Supporting
arbitrary dimensions is explicitly out of scope. Only models that output
**exactly 1024-dimensional vectors** are compatible.

### Compatible models (1024-dim)

| Model | Notes |
|-------|-------|
| `snowflake-arctic-embed` | Default. English-optimised. |
| `bge-m3` | Multilingual — 100+ languages including Chinese, Japanese, Korean. Recommended for non-English use. |
| `mxbai-embed-large` | English-focused; strong general retrieval quality. |

Always verify a model's output dimension with `ollama show <model>` before
switching. Models that look plausible but output a different dimension (e.g.
`nomic-embed-text` at 768-dim, `all-minilm` at 384-dim) will be rejected by
the dimension guard with a clear error.

---

## Changes

### 1. `db/embeddings.go` — `embeddingModel()` helper

Remove the existing `const ollamaModel`. Replace with a function and a
companion constant for the dimension:

```go
const defaultEmbeddingModel = "snowflake-arctic-embed"
const embeddingDim = 1024

// embeddingModel returns the Ollama model to use for embeddings.
// Defaults to snowflake-arctic-embed. Override with MEMORYWEB_EMBED_MODEL.
func embeddingModel() string {
    if v := os.Getenv("MEMORYWEB_EMBED_MODEL"); v != "" {
        return v
    }
    return defaultEmbeddingModel
}

// EmbeddingModel is the exported form, used by main.go subcommands.
func EmbeddingModel() string { return embeddingModel() }
```

Update the `embed()` function to call `embeddingModel()` instead of the old
constant:

```go
// before
body, err := json.Marshal(ollamaEmbedRequest{Model: ollamaModel, Input: text})

// after
body, err := json.Marshal(ollamaEmbedRequest{Model: embeddingModel(), Input: text})
```

Update the existing comment on `embed()` to remove the hardcoded model name —
reference `embeddingModel()` and the env var instead.

### 2. `db/embeddings.go` — dimension guard in `storeEmbedding`

Add a length check before serialising, so a misconfigured model fails loudly
instead of corrupting the vector table silently:

```go
func (s *Store) storeEmbedding(id string, embedding []float32) bool {
    if !s.vecAvailable || len(embedding) == 0 {
        return false
    }
    if len(embedding) != embeddingDim {
        log.Printf(
            "[memoryweb] embedding dimension mismatch for %s: got %d, want %d — "+
                "check MEMORYWEB_EMBED_MODEL; only %d-dim models are compatible with this database",
            id, len(embedding), embeddingDim, embeddingDim,
        )
        return false
    }
    // ... existing serialise + INSERT path unchanged ...
}
```

### 3. `db/embeddings.go` — early-exit in `BackfillEmbeddings` on dimension mismatch

After generating the first embedding, check its dimension before entering the
full loop. This gives backfill a clear top-level error instead of logging
per-node failures silently:

```go
// After the candidates list is built, before the embed loop:
if len(candidates) > 0 {
    probe, err := embed(candidates[0].label)
    if err == nil && len(probe) != embeddingDim {
        return 0, fmt.Errorf(
            "embedding model %q returned %d-dim vectors; this database requires %d — "+
                "set MEMORYWEB_EMBED_MODEL to a %d-dim model (e.g. bge-m3, mxbai-embed-large)",
            embeddingModel(), len(probe), embeddingDim, embeddingDim,
        )
    }
}
```

### 4. `db/embeddings.go` — update comments

Update the `BackfillEmbeddings` doc comment — it currently says "Requires
Ollama to be running with the snowflake-arctic-embed model". Replace:

```go
// BackfillEmbeddings generates and stores embeddings for all live nodes that
// do not yet have one. Returns the count of embeddings successfully written.
// Requires Ollama to be running with the model named by embeddingModel()
// (default: snowflake-arctic-embed; override: MEMORYWEB_EMBED_MODEL env var).
// The model must output exactly 1024-dimensional vectors.
```

### 5. `db/migrations.go` — migration 14: config table

Append to the migrations slice:

```go
{
    version: 14,
    desc:    "Add config key-value table",
    up: func(tx *sql.Tx) error {
        _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS config (
            key   TEXT PRIMARY KEY,
            value TEXT NOT NULL
        )`)
        return err
    },
},
```

### 6. `db/embeddings.go` — config helpers + auto-clear on model change

Add two unexported helpers for reading and writing the stored model name:

```go
func (s *Store) storedEmbeddingModel() string {
    var v string
    s.db.QueryRow(`SELECT value FROM config WHERE key = 'embedding_model'`).Scan(&v)
    return v // empty string if never set
}

func (s *Store) setStoredEmbeddingModel(model string) {
    s.db.Exec(
        `INSERT INTO config(key, value) VALUES('embedding_model', ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
        model,
    )
}
```

Add an unexported `clearEmbeddings()` helper:

```go
func (s *Store) clearEmbeddings() (int64, error) {
    res, err := s.db.Exec(`DELETE FROM node_embeddings`)
    if err != nil {
        return 0, err
    }
    return res.RowsAffected()
}
```

Update `BackfillEmbeddings` to auto-detect model changes at the top, before
building the candidates list:

```go
func (s *Store) BackfillEmbeddings(progress func(done, total int)) (int, error) {
    if !s.vecAvailable {
        return 0, fmt.Errorf("sqlite-vec not available; cannot backfill embeddings")
    }
    current := embeddingModel()
    if stored := s.storedEmbeddingModel(); stored != "" && stored != current {
        log.Printf("[memoryweb] embedding model changed from %q to %q — clearing existing embeddings", stored, current)
        if _, err := s.clearEmbeddings(); err != nil {
            return 0, fmt.Errorf("clear embeddings on model change: %w", err)
        }
    }
    // ... existing candidates query + embed loop unchanged ...
    // At the end, after the loop:
    s.setStoredEmbeddingModel(current)
    return n, nil
}
```

The config is written only after a completed backfill, not per `AddNode` call.
This means the stored model tracks "what model was used for the last full
backfill" — a deliberate choice.

> **Documented quirk:** if a user switches `MEMORYWEB_EMBED_MODEL` and then
> files a few nodes before running backfill, those new nodes get embeddings
> from the new model via `AddNode`. When backfill runs next, it sees the config
> still shows the old model, auto-clears (including those fresh embeddings),
> and regenerates everything. The fresh embeddings are immediately rewritten
> correctly — net result is a clean, consistent database. This is the intended
> behaviour; backfill is the point of consistency, not individual `AddNode`
> calls. The README's switching instructions should mention this: run
> `memoryweb backfill` promptly after changing the env var.

### 7. `main.go` — `runBackfill` banner

Replace hardcoded model references in the banner:

```go
model := db.EmbeddingModel()
fmt.Fprintf(out, "  This requires Ollama to be running with the %s model.\n", model)
fmt.Fprintf(out, "  Run: ollama pull %s\n", model)
```

### 8. `main.go` — `setupOllama`

Replace all four hardcoded `"snowflake-arctic-embed"` references with
`db.EmbeddingModel()`:

```go
model := db.EmbeddingModel()

// check presence
if err != nil || !strings.Contains(string(listOut), model) {
    if dryRun {
        fmt.Fprintf(out, "[dry-run] %s not found — would pull automatically\n", model)
        return
    }
    fmt.Fprintf(out, "Pulling %s model for semantic search...\n", model)
    cmd := exec.Command("ollama", "pull", model)
    // ... unchanged ...
    return
}

fmt.Fprintf(out, "Ollama: %s is ready.\n", model)
```

Update the function comment to remove the hardcoded name.

### 9. `main.go` — `doctor` Ollama model check

```go
model := db.EmbeddingModel()
if err != nil || !strings.Contains(string(listOut), model) {
    add("Ollama model", "fail", model+" not found — run: ollama pull "+model)
} else {
    add("Ollama model", "ok", model+" ready")
}
```

### 10. `tools/tools_test.go` — model availability check

Line 45 currently checks `strings.HasPrefix(m.Name, "snowflake-arctic-embed")`.
Update to use `db.EmbeddingModel()`:

```go
if strings.HasPrefix(m.Name, db.EmbeddingModel()) {
```

### 11. `tools/search_test.go` — skip messages

The three skip calls at lines 345, 371, 400 currently name
`snowflake-arctic-embed` literally. Update each to:

```go
t.Skip("Ollama with " + db.EmbeddingModel() + " not available")
```

---

## Documentation update — `README.md`

Add a new **Embedding model** subsection under the existing Semantic Search
section. It must cover:

1. **What the env var does** — `MEMORYWEB_EMBED_MODEL` sets the Ollama model
   used for all embedding operations (filing, backfill). Default is
   `snowflake-arctic-embed`.

2. **Why only 1024-dim models work** — the vector table dimension is fixed at
   schema creation time. Mismatched models are detected and rejected with a
   clear log message. Common incompatible models: `nomic-embed-text` (768-dim),
   `all-minilm` (384-dim).

3. **Compatible models table** — same as the table in this story.

4. **How to switch models** — step-by-step:

   ```
   # 1. Pull the new model
   ollama pull bge-m3

   # 2. Set the env var (add to your shell profile or MCP server config)
   export MEMORYWEB_EMBED_MODEL=bge-m3

   # 3. Regenerate — backfill detects the model change and clears automatically
   memoryweb backfill
   ```

   Explain the auto-clear behaviour: memoryweb tracks which model was used for
   the last backfill. When `MEMORYWEB_EMBED_MODEL` changes, the next backfill
   detects the difference, logs the change, clears all existing embeddings, and
   regenerates from scratch. No manual intervention needed. Embeddings from
   different models live in incompatible vector spaces; the auto-clear ensures
   the database is always consistent.

5. **Checking what model is active** — `memoryweb doctor` reports the
   configured model under "Ollama model".

---

## Acceptance criteria

- `TestEmbeddingModel_Default`: unset env, call `db.EmbeddingModel()`, assert
  returns `"snowflake-arctic-embed"`.
- `TestEmbeddingModel_EnvOverride`: set `MEMORYWEB_EMBED_MODEL=bge-m3`, call
  `db.EmbeddingModel()`, assert returns `"bge-m3"`. Restore env after.
- `TestStoreEmbedding_DimensionMismatch`: with a mock store where `vecAvailable
  = true`, call `storeEmbedding` with a 768-element slice; assert returns
  `false`. (Does not require Ollama.)
- `TestBackfillEmbeddings_DimensionMismatch`: with `MEMORYWEB_OLLAMA_ENDPOINT`
  pointing at a fake server that returns a 768-dim vector, call
  `BackfillEmbeddings`; assert it returns a non-nil error mentioning the
  dimension. (No real Ollama required.)
- `TestBackfillEmbeddings_AutoClearOnModelChange`: seed two fake embeddings in
  `node_embeddings`; set `config('embedding_model')` to `"old-model"`; set
  `MEMORYWEB_EMBED_MODEL=new-model` and `MEMORYWEB_OLLAMA_ENDPOINT=disabled`;
  call `BackfillEmbeddings`; assert `node_embeddings` is empty (cleared) and
  `config('embedding_model')` remains `"old-model"` (not written on failed run).
- `TestBackfillEmbeddings_WritesModelToConfig`: with Ollama disabled and an
  empty database (no nodes), call `BackfillEmbeddings`; assert
  `config('embedding_model')` is set to `embeddingModel()` after completion.
- `TestBackfillEmbeddings_NoAutoClearWhenModelUnchanged`: seed embeddings with
  stored model matching current; call `BackfillEmbeddings` with Ollama disabled;
  assert `node_embeddings` is NOT cleared.
- All existing embedding/semantic-search tests remain green.
- `go test ./...` green.

---

## Files

- `db/migrations.go` — migration 14: config table
- `db/embeddings.go` — `embeddingModel()`, `EmbeddingModel()`, `embeddingDim`
  const, `storedEmbeddingModel()`, `setStoredEmbeddingModel()`,
  `clearEmbeddings()`, dimension guard in `storeEmbedding`, auto-clear +
  config write in `BackfillEmbeddings`, updated comments
- `main.go` — `runBackfill` banner, `setupOllama`, doctor Ollama model check
- `tools/tools_test.go` — model availability check
- `tools/search_test.go` — skip messages
- `README.md` — Embedding model subsection
- `CLAUDE.md` — migration 14 in the migrations table

---

## Out of scope

- CLI flag (`--embed-model`) — env var is sufficient
- `--clear` CLI flag — auto-detection via the config table makes it unnecessary
- Automatic schema migration for dimension changes
- Per-domain model selection
- Supporting non-1024-dim models (requires a schema migration story of its own)
