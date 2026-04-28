package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
		case "setup":
			setupCmd()
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
	quiet := flags.Bool("q", false, "suppress progress output")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := runBackfill(store, os.Stdout, *quiet); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// drawProgressBar writes a progress bar to out using a carriage return to
// update in place. format: "  [=========>          ] 45/100 (45%)"
func drawProgressBar(out io.Writer, done, total int) {
	const width = 30
	pct := float64(done) / float64(total)
	filled := int(pct * float64(width))
	var bar string
	if filled >= width {
		bar = strings.Repeat("=", width)
	} else {
		bar = strings.Repeat("=", filled) + ">" + strings.Repeat(" ", width-filled-1)
	}
	fmt.Fprintf(out, "\r  [%s] %d/%d (%d%%)", bar, done, total, int(pct*100))
}

// runBackfill generates embeddings for all live nodes that do not yet have one.
// Requires Ollama to be running with the snowflake-arctic-embed model.
func runBackfill(store *db.Store, out io.Writer, quiet bool) error {
	if !store.VecAvailable() {
		return fmt.Errorf("sqlite-vec extension is not available; cannot generate embeddings\n" +
			"  Ensure memoryweb was built with CGO and sqlite-vec support")
	}

	if !quiet {
		fmt.Fprintln(out, "Backfilling embeddings for nodes without one...")
		fmt.Fprintln(out, "  This requires Ollama to be running with the snowflake-arctic-embed model.")
		fmt.Fprintln(out, "  Run: ollama pull snowflake-arctic-embed")
	}

	var progressFired bool
	var progress func(done, total int)
	if !quiet {
		progress = func(done, total int) {
			progressFired = true
			drawProgressBar(out, done, total)
		}
	}

	n, err := store.BackfillEmbeddings(progress)
	if err != nil {
		return fmt.Errorf("backfill: %w", err)
	}

	// End the progress line before printing the summary.
	if !quiet && progressFired {
		fmt.Fprintln(out)
	}

	if !quiet {
		switch {
		case n > 0:
			fmt.Fprintf(out, "Backfilled %d embedding(s).\n", n)
		case progressFired:
			// Candidates existed but all embeds failed — Ollama is likely down.
			fmt.Fprintln(out, "No embeddings stored — is Ollama running? Run: ollama serve")
		default:
			fmt.Fprintln(out, "No nodes needed backfilling (all nodes already have embeddings).")
		}
	}
	return nil
}


// ── setup subcommand ──────────────────────────────────────────────────────────

// setupCmd implements the "memoryweb setup" subcommand.
func setupCmd() {
	flags := flag.NewFlagSet("setup", flag.ExitOnError)
	dbFlag := flags.String("db", "", "memoryweb database path (default ~/.memoryweb.db)")
	dryRun := flags.Bool("dry-run", false, "print resulting config without writing; skip Ollama prompts")
	hooksDirFlag := flags.String("hooks-dir", "", "directory containing hook scripts (default: hooks/ next to binary)")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	if err := runSetup(os.Stdout, os.Stdin, *dryRun, *dbFlag, *hooksDirFlag); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runSetup installs Claude Code hooks into ~/.claude/settings.local.json and
// optionally sets up Ollama for semantic search. Separated from setupCmd so
// tests can inject writers and readers.
func runSetup(out io.Writer, in io.Reader, dryRun bool, dbPath, hooksDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	// Locate hooks directory.
	if hooksDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine binary path: %w", err)
		}
		hooksDir = filepath.Join(filepath.Dir(exe), "hooks")
	}

	saveHook := filepath.Join(hooksDir, "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir, "memoryweb_precompact_hook.sh")

	for _, script := range []string{saveHook, precompactHook} {
		info, err := os.Stat(script)
		if err != nil {
			return fmt.Errorf("hook script not found: %s (%w)", script, err)
		}
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("hook script is not executable: %s", script)
		}
	}

	if dbPath == "" {
		dbPath = filepath.Join(home, ".memoryweb.db")
	}

	// Read or start with an empty settings object.
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("cannot parse %s: %w", settingsPath, err)
		}
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	makeEntry := func(command string) map[string]interface{} {
		return map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": command,
					"env": map[string]interface{}{
						"MEMORYWEB_DB": dbPath,
					},
				},
			},
		}
	}

	stop := setupToSlice(hooks["Stop"])
	if !setupContainsCommand(stop, saveHook) {
		stop = append(stop, makeEntry(saveHook))
	}
	hooks["Stop"] = stop

	precompact := setupToSlice(hooks["PreCompact"])
	if !setupContainsCommand(precompact, precompactHook) {
		precompact = append(precompact, makeEntry(precompactHook))
	}
	hooks["PreCompact"] = precompact

	settings["hooks"] = hooks

	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if dryRun {
		fmt.Fprintln(out, string(output))
		setupOllama(out, in, true)
		return nil
	}

	stateDir := filepath.Join(home, ".memoryweb", "hook_state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Fprintln(out, "memoryweb hooks installed. Restart Claude Code to activate.")
	setupOllama(out, in, false)
	return nil
}

// setupOllama checks whether Ollama is installed and whether the
// snowflake-arctic-embed model is pulled, prompting the user to install/pull
// as needed. In dry-run mode it reports what would happen without prompting.
func setupOllama(out io.Writer, in io.Reader, dryRun bool) {
	_, err := exec.LookPath("ollama")
	if err != nil {
		// Ollama not installed.
		if dryRun {
			fmt.Fprintln(out, "[dry-run] Ollama not found — would prompt to install via https://ollama.com/install.sh")
			return
		}
		fmt.Fprint(out, "Semantic search requires Ollama. Install it? [y/N] ")
		if setupReadYN(in) {
			cmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
			cmd.Stdout = out
			cmd.Stderr = out
			if err := cmd.Run(); err != nil {
				fmt.Fprintf(out, "Ollama install failed: %v\n", err)
			}
		} else {
			fmt.Fprintln(out, "Advisory: Install Ollama from https://ollama.com/download to enable semantic search.")
		}
		return
	}

	// Ollama is installed; ensure the server is running.
	setupStartOllama(out, dryRun)

	// Check if the model is pulled.
	listOut, err := exec.Command("ollama", "list").Output()
	if err != nil || !strings.Contains(string(listOut), "snowflake-arctic-embed") {
		if dryRun {
			fmt.Fprintln(out, "[dry-run] snowflake-arctic-embed not found — would pull automatically")
			return
		}
		fmt.Fprintln(out, "Pulling snowflake-arctic-embed model for semantic search...")
		cmd := exec.Command("ollama", "pull", "snowflake-arctic-embed")
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(out, "Pull failed: %v\n", err)
		}
		return
	}

	fmt.Fprintln(out, "Ollama: snowflake-arctic-embed is ready.")
}

// setupStartOllama ensures the Ollama server is running. It checks whether
// localhost:11434 is already accepting connections; if not, it starts
// "ollama serve" as a detached background process.
func setupStartOllama(out io.Writer, dryRun bool) {
	conn, err := net.DialTimeout("tcp", "localhost:11434", time.Second)
	if err == nil {
		conn.Close()
		fmt.Fprintln(out, "Ollama: server already running.")
		return
	}

	if dryRun {
		fmt.Fprintln(out, "[dry-run] Ollama server not running — would start via 'ollama serve'")
		return
	}

	fmt.Fprint(out, "Starting Ollama server... ")
	cmd := exec.Command("ollama", "serve")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(out, "failed: %v\n", err)
		return
	}
	// Give the server a moment to bind its port before the model check.
	time.Sleep(2 * time.Second)
	fmt.Fprintln(out, "started.")
}

// setupReadYN reads one line from in and returns true if the answer is "y".
// Returns false on EOF or any other input.
func setupReadYN(in io.Reader) bool {
	scanner := bufio.NewScanner(in)
	if scanner.Scan() {
		return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
	}
	return false
}

// setupToSlice safely converts an interface{} to []interface{}.
func setupToSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	s, _ := v.([]interface{})
	return s
}

// setupContainsCommand reports whether any entry in the slice contains the
// given command path in its nested "hooks" array.
func setupContainsCommand(entries []interface{}, cmd string) bool {
	for _, e := range entries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		hs, _ := entry["hooks"].([]interface{})
		for _, h := range hs {
			hm, ok := h.(map[string]interface{})
			if ok && hm["command"] == cmd {
				return true
			}
		}
	}
	return false
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
