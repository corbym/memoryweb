// Package main implements a backfill command that generates embeddings for all
// existing nodes that do not already have one. Run this after first installing
// the snowflake-arctic-embed model to seed an existing database.
//
// Usage:
//
//	memoryweb-embeddings --db /path/to/memoryweb.db
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func init() {
	vec.Auto()
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.memoryweb.db"
}

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func embed(text string) ([]float32, error) {
	body, _ := json.Marshal(ollamaEmbedRequest{
		Model: "snowflake-arctic-embed",
		Input: text,
	})
	resp, err := http.Post("http://localhost:11434/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama returned no embeddings")
	}
	return result.Embeddings[0], nil
}

func main() {
	dbFlag := flag.String("db", defaultDBPath(), "path to the memoryweb SQLite database")
	flag.Parse()

	conn, err := sql.Open("sqlite3", *dbFlag+"?_journal_mode=WAL")
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer conn.Close()

	// Verify sqlite-vec is loaded.
	if _, err := conn.Exec(`SELECT vec_version()`); err != nil {
		fatalf("sqlite-vec not available: %v\nEnsure the database was opened with the sqlite-vec extension loaded.", err)
	}

	// Ensure the embeddings table exists.
	if _, err := conn.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS node_embeddings USING vec0(
			node_id   TEXT PRIMARY KEY,
			embedding FLOAT[384]
		)
	`); err != nil {
		fatalf("create vec0 table: %v", err)
	}

	// Find nodes without embeddings.
	rows, err := conn.Query(`
		SELECT id, label, description, why_matters
		FROM nodes
		WHERE archived_at IS NULL
		  AND id NOT IN (SELECT node_id FROM node_embeddings)
		ORDER BY created_at ASC
	`)
	if err != nil {
		fatalf("query nodes: %v", err)
	}

	type pending struct {
		id, label, description, whyMatters string
	}
	var nodes []pending
	for rows.Next() {
		var n pending
		if err := rows.Scan(&n.id, &n.label, &n.description, &n.whyMatters); err != nil {
			rows.Close()
			fatalf("scan node: %v", err)
		}
		nodes = append(nodes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fatalf("iterate nodes: %v", err)
	}

	if len(nodes) == 0 {
		fmt.Println("All nodes already have embeddings. Nothing to do.")
		return
	}

	fmt.Printf("Generating embeddings for %d node(s)...\n", len(nodes))

	ok, skipped := 0, 0
	for _, n := range nodes {
		text := n.label + " " + n.description + " " + n.whyMatters
		embedding, err := embed(text)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s (%s): %v\n", n.id, n.label, err)
			skipped++
			continue
		}

		blob, err := vec.SerializeFloat32(embedding)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s (%s): serialize: %v\n", n.id, n.label, err)
			skipped++
			continue
		}

		if _, err := conn.Exec(
			`INSERT OR REPLACE INTO node_embeddings(node_id, embedding) VALUES (?, ?)`,
			n.id, blob,
		); err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s (%s): insert: %v\n", n.id, n.label, err)
			skipped++
			continue
		}

		fmt.Printf("  [%s] %s\n", time.Now().Format("15:04:05"), n.label)
		ok++
	}

	fmt.Printf("Done. %d embedded, %d skipped.\n", ok, skipped)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
