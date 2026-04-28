package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/tools"
)

// JSON-RPC 2.0 types
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Notification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dream":
			dreamCmd()
			return
		case "backfill":
			backfillCmd()
			return
		}
	}

	dbPath := resolveDBPath()

	store, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer store.Close()

	handler := tools.New(store)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeError(encoder, nil, -32700, "parse error")
			continue
		}

		// Notifications have no ID - fire and forget
		if req.ID == nil && req.Method == "notifications/initialized" {
			continue
		}

		result, rpcErr := dispatch(req, handler)
		resp := Response{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		encoder.Encode(resp)
	}
}

func resolveDBPath() string {
	if p := os.Getenv("MEMORYWEB_DB"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return home + "/.memoryweb.db"
}

// dreamCmd implements the "memoryweb dream" subcommand.
func dreamCmd() {
	flags := flag.NewFlagSet("dream", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := runDream(store, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runDream prints a digest of recent nodes and drift candidates to out.
func runDream(store *db.Store, out io.Writer) error {
	fmt.Fprintln(out, "== memoryweb dream ==")
	fmt.Fprintln(out)

	// ── recent nodes ──────────────────────────────────────────────────────────
	recent, err := store.RecentChanges("", 10)
	if err != nil {
		return fmt.Errorf("recent changes: %w", err)
	}

	fmt.Fprintf(out, "Recent nodes (%d):\n", len(recent))
	for _, n := range recent {
		fmt.Fprintf(out, "  [%s] %s\n", n.Domain, n.Label)
	}
	if len(recent) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintln(out)

	// ── drift candidates ──────────────────────────────────────────────────────
	drift, err := store.FindDrift("", 5)
	if err != nil {
		return fmt.Errorf("find drift: %w", err)
	}

	fmt.Fprintf(out, "Drift candidates (%d):\n", len(drift))
	for _, d := range drift {
		fmt.Fprintf(out, "  %s: %s\n", d.Node.Label, d.Reason)
	}
	if len(drift) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "== end ==")
	return nil
}

// backfillCmd implements the "memoryweb backfill" subcommand.
func backfillCmd() {
	flags := flag.NewFlagSet("backfill", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := runBackfill(store, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runBackfill generates embeddings for all live nodes that do not yet have one.
// Requires Ollama to be running with the snowflake-arctic-embed model.
func runBackfill(store *db.Store, out io.Writer) error {
	if !store.VecAvailable() {
		return fmt.Errorf("sqlite-vec extension is not available; cannot generate embeddings\n" +
			"  Ensure memoryweb was built with CGO and sqlite-vec support")
	}

	fmt.Fprintln(out, "Backfilling embeddings for nodes without one...")
	fmt.Fprintln(out, "  This requires Ollama to be running with the snowflake-arctic-embed model.")
	fmt.Fprintln(out, "  Run: ollama pull snowflake-arctic-embed")

	n, err := store.BackfillEmbeddings()
	if err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	if n == 0 {
		fmt.Fprintln(out, "No nodes needed backfilling (all nodes already have embeddings, or Ollama is unavailable).")
	} else {
		fmt.Fprintf(out, "Backfilled %d embedding(s).\n", n)
	}
	return nil
}


func dispatch(req Request, h *tools.Handler) (interface{}, *RPCError) {
	switch req.Method {
	case "initialize":
		return handleInitialize(req.Params)
	case "tools/list":
		result, err := h.ListTools()
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "tools/call":
		result, err := h.CallTool(req.Params)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return result, nil
	default:
		return nil, &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func handleInitialize(params json.RawMessage) (interface{}, *RPCError) {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    "memoryweb",
			"version": "0.1.0",
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"instructions": tools.Instructions,
	}, nil
}

func writeError(enc *json.Encoder, id interface{}, code int, msg string) {
	enc.Encode(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	})
}
