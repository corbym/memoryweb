package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

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

// embeddingModel returns the Ollama model to use for embeddings.
// Defaults to snowflake-arctic-embed. Override with MEMORYWEB_EMBED_MODEL.
func embeddingModel() string {
	if v := os.Getenv("MEMORYWEB_EMBED_MODEL"); v != "" {
		return v
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
	if v := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT"); v != "" {
		if v == "disabled" {
			return nil, fmt.Errorf("embedding disabled by MEMORYWEB_OLLAMA_ENDPOINT")
		}
		endpoint = v
	}
	body, err := json.Marshal(ollamaEmbedRequest{Model: embeddingModel(), Input: text})
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
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

// storedEmbeddingModel reads the embedding model recorded in the config table.
// Returns "" if no model has been recorded yet.
func (s *Store) storedEmbeddingModel() string {
	var v string
	s.db.QueryRow(`SELECT value FROM config WHERE key = 'embedding_model'`).Scan(&v)
	return v
}

// setStoredEmbeddingModel writes or updates the embedding model in the config table.
func (s *Store) setStoredEmbeddingModel(model string) {
	s.db.Exec( //nolint:errcheck
		`INSERT INTO config(key, value) VALUES('embedding_model', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		model,
	)
}

// clearEmbeddings deletes all rows from node_embeddings.
func (s *Store) clearEmbeddings() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM node_embeddings`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// storeEmbedding inserts or replaces the embedding for a node in the
// node_embeddings virtual table. Returns true if the embedding was stored
// successfully. A failure only degrades search quality, not correctness.
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
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		log.Printf("[memoryweb] serialize embedding for %s: %v", id, err)
		return false
	}
	if _, err := s.db.Exec(
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

	rows, err := s.db.Query(`
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
		var c candidate
		if err := rows.Scan(&c.id, &c.label, &c.description, &c.whyMatters); err != nil {
			return 0, err
		}
		candidates = append(candidates, c)
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

	n := 0
	for i, c := range candidates {
		embedding, err := embed(embedTextForNode(c.label, c.description, c.whyMatters))
		if progress != nil {
			progress(i+1, len(candidates))
		}
		if err != nil {
			// Only log when there is no progress callback — if one is present,
			// the caller is rendering a progress bar and individual error lines
			// would corrupt it. The summary already conveys how many succeeded.
			if progress == nil {
				log.Printf("[memoryweb] backfill embed %s: %v", c.id, err)
			}
			continue
		}
		if s.storeEmbedding(c.id, embedding) {
			n++
		}
	}

	// Record the current model only when the run produced results or there was
	// nothing to do — not when Ollama was unavailable and candidates were skipped.
	if n > 0 || len(candidates) == 0 {
		s.setStoredEmbeddingModel(current)
	}
	return n, nil
}
