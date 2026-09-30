package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// ollamaEmbedRequest is the JSON body for the Ollama /api/embed endpoint.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse is the JSON response from the Ollama /api/embed endpoint.
type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

const defaultEmbeddingModel = "snowflake-arctic-embed"
const embeddingDim = 1024
const ollamaEndpoint = "http://localhost:11434/api/embed"

// ollamaHTTPClient is a shared HTTP client with a timeout for Ollama requests.
// Prevents a hung Ollama process from blocking the calling goroutine forever.
var ollamaHTTPClient = &http.Client{Timeout: 30 * time.Second}

// maxEmbeddingBodySize is the maximum response body size we'll read from Ollama.
// A legitimate embedding response is well under 1 MB; this guards against a
// misconfigured or malicious endpoint returning unbounded data.
const maxEmbeddingBodySize = 1 << 20 // 1 MB

// embeddingModel returns the Ollama model to use for embeddings.
// Defaults to snowflake-arctic-embed. Override with MEMORYWEB_EMBED_MODEL.
func embeddingModel() string {
	if model := os.Getenv("MEMORYWEB_EMBED_MODEL"); model != "" {
		return model
	}
	return defaultEmbeddingModel
}

// EmbeddingModel is the exported form of embeddingModel, used by main.go subcommands.
func EmbeddingModel() string { return embeddingModel() }

// embed calls the local Ollama instance to generate an embedding for the
// given text using the model returned by embeddingModel() (default:
// snowflake-arctic-embed; override: MEMORYWEB_EMBED_MODEL env var). Returns
// nil if Ollama is not running or the model is unavailable — callers must
// treat nil as a signal to fall back to literal LIKE search.
//
// The endpoint may be overridden by MEMORYWEB_OLLAMA_ENDPOINT. Set it to
// "disabled" to make embed always fail, which is useful in tests that
// exercise LIKE search behaviour in isolation from Ollama.
func embed(text string) ([]float32, error) {
	endpoint := ollamaEndpoint
	if envEndpoint := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT"); envEndpoint != "" {
		if envEndpoint == "disabled" {
			return nil, fmt.Errorf("embedding disabled by MEMORYWEB_OLLAMA_ENDPOINT")
		}
		endpoint = envEndpoint
	}
	body, err := json.Marshal(ollamaEmbedRequest{Model: embeddingModel(), Input: text})
	if err != nil {
		return nil, err
	}
	resp, err := ollamaHTTPClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxEmbeddingBodySize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, raw)
	}

	var result ollamaEmbedResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding returned")
	}
	return result.Embeddings[0], nil
}

// Embed is the exported form of embed, used by external tools such as
// the embeddings backfill command.
func Embed(text string) ([]float32, error) {
	return embed(text)
}

// embedTextForNode produces the canonical embed input for a node —
// label, description, and why_matters joined by spaces.
// Single source of truth used by AddNode, AddNodesBatch, BackfillEmbeddings, and UpdateNode.
func embedTextForNode(label, description, whyMatters string) string {
	return label + " " + description + " " + whyMatters
}

// EmbedTextForNode is the exported form of embedTextForNode, for use in tests.
func EmbedTextForNode(label, description, whyMatters string) string {
	return embedTextForNode(label, description, whyMatters)
}

// queryPrefix returns the inference-time prefix for the current embedding model.
// snowflake-arctic-embed and mxbai-embed-large are asymmetric: documents are
// indexed bare, but queries must be prepended with this string so the model
// maps them to the same vector space as stored embeddings. bge-m3 and all
// other models require no prefix.
func queryPrefix() string {
	switch embeddingModel() {
	case "snowflake-arctic-embed", "snowflake-arctic-embed:latest",
		"mxbai-embed-large", "mxbai-embed-large:latest":
		return "Represent this sentence for searching relevant passages: "
	default:
		return ""
	}
}

// QueryPrefix is the exported form of queryPrefix, for use in tests.
func QueryPrefix() string { return queryPrefix() }

// embedFieldsForNode returns the per-field embedding inputs: label and why_matters
// are embedded separately so each fits within the model's context window.
// description is excluded — it is the longest field and adds no meaningful recall
// improvement beyond label and why_matters.
func embedFieldsForNode(label, _, whyMatters string) map[string]string {
	return map[string]string{
		"label":       label,
		"why_matters": whyMatters,
	}
}

// EmbedFieldsForNode is the exported form of embedFieldsForNode, for use in tests.
func EmbedFieldsForNode(label, description, whyMatters string) map[string]string {
	return embedFieldsForNode(label, description, whyMatters)
}

// storeFieldEmbeddings stores per-field embeddings for a node in the
// node_label_embeddings and node_whymatters_embeddings virtual tables.
// Each field is stored independently; a failure in one does not affect the other.
// Returns true only when all supplied embeddings are stored successfully.
func (st *Store) storeFieldEmbeddings(id string, fields map[string][]float32) bool {
	if !st.vecFieldsAvailable {
		return false
	}
	if len(fields) == 0 {
		return false
	}
	tableFor := map[string]string{
		"label":       "node_label_embeddings",
		"why_matters": "node_whymatters_embeddings",
	}
	ok := true
	for field, emb := range fields {
		table, known := tableFor[field]
		if !known || len(emb) == 0 {
			continue
		}
		if len(emb) != embeddingDim {
			log.Printf(
				"[memoryweb] field embedding dim mismatch for %s/%s: got %d, want %d",
				id, field, len(emb), embeddingDim,
			)
			ok = false
			continue
		}
		blob, err := vec.SerializeFloat32(emb)
		if err != nil {
			log.Printf("[memoryweb] serialize field embedding for %s/%s: %v", id, field, err)
			ok = false
			continue
		}
		if _, err := st.db.Exec(
			`INSERT OR REPLACE INTO `+table+`(node_id, embedding) VALUES (?, ?)`,
			id, blob,
		); err != nil {
			log.Printf("[memoryweb] store field embedding for %s/%s: %v", id, field, err)
			ok = false
		}
	}
	return ok
}

// storedEmbeddingModel reads the embedding model recorded in the config table.
// Returns "" if no model has been recorded yet.
func (st *Store) storedEmbeddingModel() string {
	var model string
	st.db.QueryRow(`SELECT value FROM config WHERE key = 'embedding_model'`).Scan(&model)
	return model
}

// setStoredEmbeddingModel writes or updates the embedding model in the config table.
func (st *Store) setStoredEmbeddingModel(model string) {
	st.db.Exec( //nolint:errcheck
		`INSERT INTO config(key, value) VALUES('embedding_model', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		model,
	)
}

// clearEmbeddings deletes all rows from the embedding tables.
func (st *Store) clearEmbeddings() (int64, error) {
	res, err := st.db.Exec(`DELETE FROM node_embeddings`)
	if err != nil {
		return 0, err
	}
	if st.vecFieldsAvailable {
		if _, err := st.db.Exec(`DELETE FROM node_label_embeddings`); err != nil {
			return 0, fmt.Errorf("clear label embeddings: %w", err)
		}
		if _, err := st.db.Exec(`DELETE FROM node_whymatters_embeddings`); err != nil {
			return 0, fmt.Errorf("clear whymatters embeddings: %w", err)
		}
	}
	return res.RowsAffected()
}

// storeEmbedding inserts or replaces the embedding for a node in the
// node_embeddings virtual table. Returns true if the embedding was stored
// successfully. A failure only degrades search quality, not correctness.
func (st *Store) storeEmbedding(id string, embedding []float32) bool {
	if !st.vecAvailable || len(embedding) == 0 {
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
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		log.Printf("[memoryweb] serialize embedding for %s: %v", id, err)
		return false
	}
	if _, err := st.db.Exec(
		`INSERT OR REPLACE INTO node_embeddings(node_id, embedding) VALUES (?, ?)`,
		id, blob,
	); err != nil {
		log.Printf("[memoryweb] store embedding for %s: %v", id, err)
		return false
	}
	return true
}

// BackfillEmbeddings generates and stores embeddings for all live nodes that
// do not yet have one. Returns the count of embeddings successfully written.
// Requires Ollama to be running with the model named by embeddingModel()
// (default: snowflake-arctic-embed; override: MEMORYWEB_EMBED_MODEL env var).
// The model must output exactly 1024-dimensional vectors.
// progress is called after each successful embedding with (done, total);
// pass nil to disable progress reporting.
func (st *Store) BackfillEmbeddings(progress func(done, total int)) (int, error) {
	if !st.vecAvailable {
		return 0, fmt.Errorf("sqlite-vec not available; cannot backfill embeddings")
	}

	current := embeddingModel()
	if stored := st.storedEmbeddingModel(); stored != "" && stored != current {
		log.Printf("[memoryweb] embedding model changed from %q to %q — clearing existing embeddings", stored, current)
		if _, err := st.clearEmbeddings(); err != nil {
			return 0, fmt.Errorf("clear embeddings on model change: %w", err)
		}
	}

	rows, err := st.db.Query(`
		SELECT n.id, n.label, n.description, n.why_matters
		FROM nodes n
		LEFT JOIN node_embeddings e ON e.node_id = n.id
		WHERE n.archived_at IS NULL AND e.node_id IS NULL
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type candidate struct {
		id, label, description, whyMatters string
	}
	var candidates []candidate
	for rows.Next() {
		var cand candidate
		if err := rows.Scan(&cand.id, &cand.label, &cand.description, &cand.whyMatters); err != nil {
			return 0, err
		}
		candidates = append(candidates, cand)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	// Early-exit: probe the first candidate to detect a dimension mismatch
	// before running the full loop.
	if len(candidates) > 0 {
		probe, probeErr := embed(candidates[0].label)
		if probeErr == nil && len(probe) != embeddingDim {
			return 0, fmt.Errorf(
				"embedding model %q returned %d-dim vectors; this database requires %d — "+
					"set MEMORYWEB_EMBED_MODEL to a %d-dim model (e.g. bge-m3, mxbai-embed-large)",
				embeddingModel(), len(probe), embeddingDim, embeddingDim,
			)
		}
	}

	processedIDs := make(map[string]struct{})
	count := 0
	for i, cand := range candidates {
		embedding, err := embed(embedTextForNode(cand.label, cand.description, cand.whyMatters))
		if progress != nil {
			progress(i+1, len(candidates))
		}
		if err != nil {
			// Only log when there is no progress callback — if one is present,
			// the caller is rendering a progress bar and individual error lines
			// would corrupt it. The summary already conveys how many succeeded.
			if progress == nil {
				log.Printf("[memoryweb] backfill embed %s: %v", cand.id, err)
			}
			continue
		}
		if st.storeEmbedding(cand.id, embedding) {
			count++
			processedIDs[cand.id] = struct{}{} // track for supplementary-pass dedup
		}
		// Per-field embeddings (best-effort alongside the legacy embedding).
		// Supplementary pass will retry nodes whose field embeddings fail here.
		if st.vecFieldsAvailable {
			_ = st.embedAndStoreFields(cand.id, cand.label, cand.description, cand.whyMatters)
		}
	}

	// Supplementary pass: fill field embeddings for nodes that already have a
	// legacy node_embeddings entry but were created before migration v16.
	// Skipped when the main loop had candidates but Ollama was unavailable
	// (count == 0 with candidates > 0) to avoid doubling failed network calls.
	if st.vecFieldsAvailable && (count > 0 || len(candidates) == 0) {
		suppCount, suppCandidates := st.backfillFieldEmbeddings(processedIDs)
		count += suppCount
		// When the main loop had no candidates but the supplementary pass did,
		// fire the progress callback so runBackfill can detect Ollama was needed
		// and print the correct warning rather than "All embeddings are up to date."
		if len(candidates) == 0 && suppCandidates > 0 && suppCount == 0 && progress != nil {
			progress(0, suppCandidates)
		}
	}

	// Record the current model only when the run produced results or there was
	// nothing to do — not when Ollama was unavailable and candidates were skipped.
	if count > 0 || len(candidates) == 0 {
		st.setStoredEmbeddingModel(current)
	}
	return count, nil
}

// embedAndStoreFields generates and stores per-field embeddings for a node.
// Returns true only when every expected field was embedded and stored. When
// one or more embed calls fail (partial success), the successful fields are
// still stored so future passes only retry the missing ones — but false is
// returned so callers can distinguish a complete write from a partial one.
func (st *Store) embedAndStoreFields(id, label, description, whyMatters string) bool {
	fieldTexts := embedFieldsForNode(label, description, whyMatters)
	fieldEmbs := make(map[string][]float32, len(fieldTexts))
	for field, text := range fieldTexts {
		if emb, err := embed(text); err == nil {
			fieldEmbs[field] = emb
		}
	}
	if len(fieldEmbs) < len(fieldTexts) {
		// At least one embed call failed — store partial result so the
		// supplementary pass only retries the missing field(s) next run.
		st.storeFieldEmbeddings(id, fieldEmbs)
		return false
	}
	return st.storeFieldEmbeddings(id, fieldEmbs)
}

// ClearFieldEmbeddings deletes all rows from the per-field embedding tables
// (node_label_embeddings and node_whymatters_embeddings) without touching the
// legacy node_embeddings table. Safe to call before a forced re-backfill.
// Both deletes run in a single transaction so a failure leaves both tables
// untouched rather than leaving them in an asymmetric state.
func (st *Store) ClearFieldEmbeddings() error {
	if !st.vecFieldsAvailable {
		return nil
	}
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM node_label_embeddings`); err != nil {
		return fmt.Errorf("clear label embeddings: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM node_whymatters_embeddings`); err != nil {
		return fmt.Errorf("clear whymatters embeddings: %w", err)
	}
	return tx.Commit()
}

// backfillFieldEmbeddings fills node_label_embeddings and node_whymatters_embeddings
// for live nodes that lack either per-field embedding (created before migration v16
// or partially written due to a mid-run Ollama failure). alreadyCounted is the set
// of node IDs already incremented by the main loop — nodes in this set are processed
// but not counted again, preventing double-counting when the main loop wrote the
// legacy embedding but per-field embeddings were only partial. Returns (count,
// candidateCount): count is the number of newly-counted nodes, candidateCount is the
// total number of nodes that were candidates for the pass.
func (st *Store) backfillFieldEmbeddings(alreadyCounted map[string]struct{}) (count, candidateCount int) {
	rows, err := st.db.Query(`
		SELECT n.id, n.label, n.description, n.why_matters
		FROM nodes n
		LEFT JOIN node_label_embeddings le ON le.node_id = n.id
		LEFT JOIN node_whymatters_embeddings we ON we.node_id = n.id
		WHERE n.archived_at IS NULL AND (le.node_id IS NULL OR we.node_id IS NULL)
	`)
	if err != nil {
		log.Printf("[memoryweb] backfill field embeddings: query: %v", err)
		return 0, 0
	}

	type candidate struct{ id, label, description, whyMatters string }
	var pending []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.label, &c.description, &c.whyMatters); err != nil {
			log.Printf("[memoryweb] backfill field embeddings: scan: %v", err)
			continue
		}
		pending = append(pending, c)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[memoryweb] backfill field embeddings: rows: %v", err)
	}
	rows.Close()

	for _, cand := range pending {
		if st.embedAndStoreFields(cand.id, cand.label, cand.description, cand.whyMatters) {
			if _, dup := alreadyCounted[cand.id]; !dup {
				count++
			}
		}
	}
	return count, len(pending)
}
