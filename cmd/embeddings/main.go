// Package main implements the memoryweb embeddings backfill command.
// It generates and stores vector embeddings for all live nodes that do not yet
// have one, enabling semantic search for existing databases.
//
// Usage:
//
//	memoryweb-embeddings --db /path/to/memoryweb.db
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corbym/memoryweb/db"
)

func main() {
	dbFlag := flag.String("db", "", "path to memoryweb database (default: ~/.memoryweb.db)")
	flag.Parse()

	dbPath := *dbFlag
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fatalf("cannot determine home directory: %v", err)
		}
		dbPath = filepath.Join(home, ".memoryweb.db")
	}

	store, err := db.New(dbPath)
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer store.Close()

	if !store.VecAvailable() {
		fatalf("sqlite-vec extension is not available; cannot generate embeddings.\n" +
			"  Ensure memoryweb was built with CGO and sqlite-vec support.")
	}

	fmt.Println("Backfilling embeddings for nodes without one...")
	fmt.Println("  This requires Ollama to be running with the snowflake-arctic-embed model.")
	fmt.Println("  Run: ollama pull snowflake-arctic-embed")

	n, err := store.BackfillEmbeddings()
	if err != nil {
		fatalf("backfill: %v", err)
	}

	if n == 0 {
		fmt.Println("No nodes needed backfilling (all nodes already have embeddings, or Ollama is unavailable).")
	} else {
		fmt.Printf("Backfilled %d embedding(s).\n", n)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
