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

const ollamaModel = "snowflake-arctic-embed"
const ollamaEndpoint = "http://localhost:11434/api/embed"

// embed calls the local Ollama instance to generate an embedding for the
// given text using the snowflake-arctic-embed model. Returns nil if Ollama is
// not running or the model is unavailable — callers must treat nil as a signal
// to fall back to literal LIKE search.
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
	body, err := json.Marshal(ollamaEmbedRequest{Model: ollamaModel, Input: text})
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

// storeEmbedding inserts or replaces the embedding for a node in the
// node_embeddings virtual table. Returns true if the embedding was stored
// successfully. A failure only degrades search quality, not correctness.
func (s *Store) storeEmbedding(id string, embedding []float32) bool {
	if !s.vecAvailable || len(embedding) == 0 {
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
// Requires Ollama to be running with the snowflake-arctic-embed model.
// progress is called after each successful embedding with (done, total);
// pass nil to disable progress reporting.
func (s *Store) BackfillEmbeddings(progress func(done, total int)) (int, error) {
	if !s.vecAvailable {
		return 0, fmt.Errorf("sqlite-vec not available; cannot backfill embeddings")
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
	rows.Close()

	n := 0
	for i, c := range candidates {
		text := c.label + " " + c.description + " " + c.whyMatters
		embedding, err := embed(text)
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
	return n, nil
}
