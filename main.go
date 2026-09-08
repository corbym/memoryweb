package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/stats"
	"github.com/corbym/memoryweb/tools"
)

// Version is the current build version. It is injected at build time via
// -ldflags="-X main.Version=vX.Y.Z" by the release workflow; the default
// value "dev" is used for local / untagged builds.
var Version = "dev"

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
		case "--help", "-h", "help":
			fmt.Fprintf(os.Stdout, "memoryweb %s\n\n", Version)
			fmt.Fprintln(os.Stdout, "Usage: memoryweb [subcommand] [flags]")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "When run without a subcommand, memoryweb starts as an MCP server (stdin/stdout).")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Subcommands:")
			fmt.Fprintln(os.Stdout, "  setup          Install Claude Code hooks and configure desktop MCP clients")
			fmt.Fprintln(os.Stdout, "  options        View or set hook behaviour options")
			fmt.Fprintln(os.Stdout, "  doctor         Run diagnostic checks on the installation")
			fmt.Fprintln(os.Stdout, "  dream          Print a digest of recent nodes and drift candidates")
			fmt.Fprintln(os.Stdout, "  search         Search nodes and print lean results (for scripting / hooks)")
			fmt.Fprintln(os.Stdout, "  backfill       Generate embeddings for nodes that are missing one")
			fmt.Fprintln(os.Stdout, "  merge-domains  Merge all nodes from one domain into another")
			fmt.Fprintln(os.Stdout, "  backup         Write a consistent standalone snapshot of the database")
			fmt.Fprintln(os.Stdout, "  purge          Hard-delete archived nodes (requires --confirm or --dry-run)")
			fmt.Fprintln(os.Stdout, "  version        Print the version and exit")
			fmt.Fprintln(os.Stdout, "")
			fmt.Fprintln(os.Stdout, "Run 'memoryweb <subcommand> --help' for subcommand-specific flags.")
			return
		case "--version", "-v", "version":
			fmt.Println(Version)
			return
		case "dream":
			dreamCmd()
			return
		case "search":
			searchCmd()
			return
		case "backfill":
			backfillCmd()
			return
		case "setup":
			setupCmd()
			return
		case "options":
			optionsCmd()
			return
		case "doctor":
			doctorCmd()
			return
		case "merge-domains":
			mergeDomainsCmd()
			return
		case "backup":
			backupCmd()
			return
		case "purge":
			purgeCmd()
			return
		default:
			fmt.Fprintf(os.Stderr, "memoryweb: unknown subcommand %q\n\n", os.Args[1])
			fmt.Fprintln(os.Stderr, "Subcommands: setup, options, doctor, dream, search, backfill, merge-domains, backup, purge, version")
			fmt.Fprintln(os.Stderr, "Run 'memoryweb --help' for usage.")
			os.Exit(1)
		}
	}

	dbPath := resolveDBPath()

	store, err := db.New(dbPath)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer store.Close()

	handler := tools.New(store, Version, checkLatestRelease)

	// Stats recording — enabled when MEMORYWEB_STATS_FILE and/or
	// MEMORYWEB_STATS_JSON_FILE are set.
	var rec *stats.Recorder
	humanPath := os.Getenv("MEMORYWEB_STATS_FILE")
	jsonPath := os.Getenv("MEMORYWEB_STATS_JSON_FILE")
	if humanPath != "" || jsonPath != "" {
		rec = stats.New(humanPath, jsonPath)
		flushStats := func() {
			if _, err := rec.Flush(); err != nil {
				log.Printf("[memoryweb] stats flush: %v", err)
			}
		}
		defer flushStats()

		// Also flush on SIGTERM / SIGINT so stats are written when the MCP
		// host terminates the process rather than closing stdin cleanly.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigCh
			flushStats()
			os.Exit(0)
		}()
	}

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
			// Signal the client to refresh its tool list. This ensures that if
			// the server binary was updated between sessions, the agent picks up
			// the current schema rather than relying on a stale cached copy.
			encoder.Encode(map[string]interface{}{ //nolint:errcheck
				"jsonrpc": "2.0",
				"method":  "notifications/tools/list_changed",
			})
			continue
		}

		result, rpcErr := dispatch(req, handler, rec)
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
	if dbPath := os.Getenv("MEMORYWEB_DB"); dbPath != "" {
		return dbPath
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
	recent, err := store.RecentChanges("", 10, nil)
	if err != nil {
		return fmt.Errorf("recent changes: %w", err)
	}

	fmt.Fprintf(out, "Recent nodes (%d):\n", len(recent))
	for _, node := range recent {
		fmt.Fprintf(out, "  [%s] %s\n", node.Domain, node.Label)
	}
	if len(recent) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintln(out)

	// ── drift candidates ──────────────────────────────────────────────────────
	drift, err := store.FindDrift("", 5, nil, nil, "", 2)
	if err != nil {
		return fmt.Errorf("find drift: %w", err)
	}

	fmt.Fprintf(out, "Drift candidates (%d):\n", len(drift))
	for _, drift := range drift {
		fmt.Fprintf(out, "  %s: %s\n", drift.Node.Label, drift.Reason)
	}
	if len(drift) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintln(out)

	// ── disconnected nodes ────────────────────────────────────────────────────
	disconnected, err := store.FindDisconnected("", nil, nil, 50)
	if err != nil {
		return fmt.Errorf("find disconnected: %w", err)
	}

	fmt.Fprintf(out, "Disconnected nodes (%d):\n", len(disconnected))
	for _, node := range disconnected {
		fmt.Fprintf(out, "  [%s] %s\n", node.Domain, node.Label)
	}
	if len(disconnected) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "== end ==")
	return nil
}

// searchCmd implements the "memoryweb search" subcommand.
func searchCmd() {
	flags := flag.NewFlagSet("search", flag.ExitOnError)
	dbFlag := flags.String("db", "", "database path (default ~/.memoryweb.db)")
	query := flags.String("query", "", "search terms")
	domain := flags.String("domain", "", "restrict to domain")
	limit := flags.Int("limit", 10, "max results")
	lean := flags.Bool("lean", false, "compact one-line output")
	exact := flags.Bool("exact", false, "exact sub-string label match instead of semantic search (for hyphenated IDs)")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error
	if err := runSearchCmd(os.Stdout, *dbFlag, *query, *domain, *limit, *lean, *exact); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runSearchCmd opens the DB, runs SearchNodes, and writes results to out.
// With lean=false it prints one JSON object per result; with lean=true it
// prints a compact single-line summary per result.
func runSearchCmd(out io.Writer, dbPath, query, domain string, limit int, lean, exact bool) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("--query is required")
	}
	if dbPath == "" {
		dbPath = resolveDBPath()
	}
	store, err := db.New(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	var result *db.SearchResult
	if exact {
		result, err = store.SearchNodesExact(query, domain, limit, "", nil)
	} else {
		result, err = store.SearchNodes(query, domain, limit, "", nil)
	}
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if len(result.Nodes) == 0 {
		return nil
	}

	for _, nr := range result.Nodes {
		if lean {
			why := searchTruncateWhy(nr.Node.WhyMatters)
			meta := nr.Node.Domain
			if nr.Node.NodeKind != "" {
				meta = nr.Node.Domain + ", " + nr.Node.NodeKind
			}
			dist := ""
			if nr.SemanticDistance != nil && *nr.SemanticDistance != 0 {
				dist = fmt.Sprintf("  %.4f", *nr.SemanticDistance)
			}
			fmt.Fprintf(out, "[%s] %s -- %s (%s)%s\n", nr.Node.ID, nr.Node.Label, why, meta, dist)
		} else {
			b, _ := json.Marshal(nr)
			fmt.Fprintln(out, string(b))
		}
	}
	return nil
}

// searchTruncateWhy truncates a why_matters string to ≤150 chars at a sentence
// boundary where possible, or appends "..." otherwise.
func searchTruncateWhy(s string) string {
	const limit = 150
	if len(s) <= limit {
		return s
	}
	sub := s[:limit]
	lastBoundary := -1
	for i := 0; i < len(sub); i++ {
		if sub[i] == '.' || sub[i] == '!' || sub[i] == '?' {
			next := i + 1
			if next >= len(sub) || sub[next] == ' ' || sub[next] == '\n' || sub[next] == '\t' {
				lastBoundary = i + 1
			}
		}
	}
	if lastBoundary > 0 {
		return strings.TrimRight(s[:lastBoundary], " \t\n")
	}
	return s[:limit] + "..."
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
// Requires Ollama to be running with the model named by db.EmbeddingModel().
func runBackfill(store *db.Store, out io.Writer, quiet bool) error {
	if !store.VecAvailable() {
		return fmt.Errorf("sqlite-vec extension is not available; cannot generate embeddings\n" +
			"  Ensure memoryweb was built with CGO and sqlite-vec support")
	}

	if !quiet {
		model := db.EmbeddingModel()
		fmt.Fprintln(out, "Backfilling embeddings for nodes without one...")
		fmt.Fprintf(out, "  This requires Ollama to be running with the %s model.\n", model)
		fmt.Fprintf(out, "  Run: ollama pull %s\n", model)
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

// detectedAgent represents a desktop MCP client found on the system.
type detectedAgent struct {
	Name       string // human-readable name, e.g. "Claude Desktop"
	ConfigPath string // path to the MCP server config JSON file
}

// agentSupportDir returns the OS-specific application-support directory rooted
// at home. macOS: ~/Library/Application Support; Windows: %APPDATA%;
// Linux/other: ~/.config.
func agentSupportDir(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return appdata
		}
		return filepath.Join(home, "AppData", "Roaming")
	default:
		return filepath.Join(home, ".config")
	}
}

// detectDesktopAgents returns desktop MCP clients that appear to be installed,
// based on whether the application's data directory already exists.
func detectDesktopAgents(home string) []detectedAgent {
	support := agentSupportDir(home)
	var agents []detectedAgent

	// Claude Desktop — config file is claude_desktop_config.json inside the
	// Claude/ subdirectory of the support dir.
	claudeDir := filepath.Join(support, "Claude")
	if info, err := os.Stat(claudeDir); err == nil && info.IsDir() {
		agents = append(agents, detectedAgent{
			Name:       "Claude Desktop",
			ConfigPath: filepath.Join(claudeDir, "claude_desktop_config.json"),
		})
	}

	// ChatGPT Desktop — not available on Linux.
	if runtime.GOOS != "linux" {
		chatgptDir := filepath.Join(support, "ChatGPT")
		if info, err := os.Stat(chatgptDir); err == nil && info.IsDir() {
			agents = append(agents, detectedAgent{
				Name:       "ChatGPT Desktop",
				ConfigPath: filepath.Join(chatgptDir, "mcp.json"),
			})
		}
	}

	return agents
}

// setupWriteMCPServerConfig reads the MCP server config at configPath (or
// starts with an empty object if the file does not exist), ensures the
// memoryweb entry is present under "mcpServers", and writes it back.
// The operation is idempotent.
func setupWriteMCPServerConfig(configPath, exePath, dbPath string) error {
	var cfg map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("cannot parse %s: %w", configPath, err)
		}
	}
	if cfg == nil {
		cfg = make(map[string]interface{})
	}

	servers, _ := cfg["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = make(map[string]interface{})
	}
	servers["memoryweb"] = map[string]interface{}{
		"command": exePath,
		"env": map[string]interface{}{
			"MEMORYWEB_DB": dbPath,
		},
	}
	cfg["mcpServers"] = servers

	output, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(configPath, output, 0600)
}

// setupCmd implements the "memoryweb setup" subcommand.
func setupCmd() {
	flags := flag.NewFlagSet("setup", flag.ExitOnError)
	dbFlag := flags.String("db", "", "memoryweb database path (default ~/.memoryweb.db)")
	dryRun := flags.Bool("dry-run", false, "print resulting config without writing; skip Ollama prompts")
	hooksDirFlag := flags.String("hooks-dir", "", "directory containing hook scripts (default: hooks/ next to binary)")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	if err := runSetup(os.Stdout, os.Stdin, *dryRun, *dbFlag, *hooksDirFlag, ""); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runSetup installs Claude Code hooks into ~/.claude/settings.json,
// detects desktop MCP clients (Claude Desktop, ChatGPT Desktop) and offers to
// configure each one, then optionally sets up Ollama for semantic search.
// Separated from setupCmd so tests can inject writers and readers.
func runSetup(out io.Writer, in io.Reader, dryRun bool, dbPath, hooksDir, homeOverride string) error {
	var home string
	if homeOverride != "" {
		home = homeOverride
	} else {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
	}

	// Wrap in with a bufio.Reader once so that all y/N prompts share the same
	// buffered reader and successive calls do not lose unconsumed bytes.
	reader := bufio.NewReader(in)

	// Locate hooks directory.
	if hooksDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine binary path: %w", err)
		}
		binDir := filepath.Dir(exe)
		// Primary: hooks/ next to the binary (dev / tarball install).
		candidate := filepath.Join(binDir, "hooks")
		if _, err := os.Stat(candidate); err != nil {
			// Fallback: <prefix>/share/memoryweb/hooks (Homebrew / FHS install).
			// Binary lives at <prefix>/bin/memoryweb; hooks at <prefix>/share/memoryweb/hooks.
			candidate = filepath.Join(filepath.Dir(binDir), "share", "memoryweb", "hooks")
		}
		hooksDir = candidate
	}

	saveHook := filepath.Join(hooksDir, "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir, "memoryweb_precompact_hook.sh")
	userpromptsubmitHook := filepath.Join(hooksDir, "memoryweb_userpromptsubmit_hook.sh")
	subagentStartHook := filepath.Join(hooksDir, "memoryweb_subagent_start_hook.sh")
	subagentStopHook := filepath.Join(hooksDir, "memoryweb_subagent_stop_hook.sh")
	postcompactHook := filepath.Join(hooksDir, "memoryweb_postcompact_hook.sh")

	for _, script := range []string{saveHook, precompactHook, userpromptsubmitHook, subagentStartHook, subagentStopHook, postcompactHook} {
		info, err := os.Stat(script)
		if err != nil {
			return fmt.Errorf("hook script not found: %s (%w)", script, err)
		}
		if info.Mode()&0o111 == 0 && runtime.GOOS != "windows" {
			return fmt.Errorf("hook script is not executable: %s", script)
		}
	}

	if dbPath == "" {
		dbPath = filepath.Join(home, ".memoryweb.db")
	} else if abs, err := filepath.Abs(dbPath); err == nil {
		// Resolve relative paths (e.g. --db ./x.db) against the current
		// working directory so the value embedded in hook and MCP client
		// configs is stable regardless of the client's runtime CWD.
		dbPath = abs
	}

	// ── Claude Code hooks ─────────────────────────────────────────────────────

	// Read or start with an empty settings object.
	settingsPath := filepath.Join(home, ".claude", "settings.json")
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

	hooks["Stop"] = setupUpsertCommand(setupToSlice(hooks["Stop"]), saveHook, makeEntry(saveHook))
	hooks["PreCompact"] = setupUpsertCommand(setupToSlice(hooks["PreCompact"]), precompactHook, makeEntry(precompactHook))
	hooks["UserPromptSubmit"] = setupUpsertCommand(setupToSlice(hooks["UserPromptSubmit"]), userpromptsubmitHook, makeEntry(userpromptsubmitHook))
	hooks["SubagentStart"] = setupUpsertCommand(setupToSlice(hooks["SubagentStart"]), subagentStartHook, makeEntry(subagentStartHook))
	hooks["SubagentStop"] = setupUpsertCommand(setupToSlice(hooks["SubagentStop"]), subagentStopHook, makeEntry(subagentStopHook))
	hooks["PostCompact"] = setupUpsertCommand(setupToSlice(hooks["PostCompact"]), postcompactHook, makeEntry(postcompactHook))

	settings["hooks"] = hooks

	claudeOutput, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if dryRun {
		fmt.Fprintln(out, string(claudeOutput))
	} else {
		stateDir := filepath.Join(home, ".memoryweb", "hook_state")
		if err := os.MkdirAll(stateDir, 0700); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0700); err != nil {
			return fmt.Errorf("create .claude dir: %w", err)
		}
		if err := os.WriteFile(settingsPath, claudeOutput, 0600); err != nil {
			return fmt.Errorf("write settings: %w", err)
		}
		fmt.Fprintln(out, "memoryweb hooks installed. Restart Claude Code to activate.")
	}

	// ── Desktop agent detection ───────────────────────────────────────────────

	exePath, exeErr := os.Executable()
	if exeErr != nil {
		fmt.Fprintf(out, "Warning: cannot determine binary path — skipping desktop agent configuration: %v\n", exeErr)
	} else {
		desktopAgents := detectDesktopAgents(home)
		for _, agent := range desktopAgents {
			if dryRun {
				preview := map[string]interface{}{
					"mcpServers": map[string]interface{}{
						"memoryweb": map[string]interface{}{
							"command": exePath,
							"env":     map[string]interface{}{"MEMORYWEB_DB": dbPath},
						},
					},
				}
				previewJSON, _ := json.MarshalIndent(preview, "", "  ")
				fmt.Fprintf(out, "[dry-run] %s detected — would write to %s:\n%s\n",
					agent.Name, agent.ConfigPath, previewJSON)
				continue
			}

			fmt.Fprintf(out, "Detected %s. Configure it? [y/N] ", agent.Name)
			if setupReadYN(reader) {
				if err := setupWriteMCPServerConfig(agent.ConfigPath, exePath, dbPath); err != nil {
					fmt.Fprintf(out, "Warning: could not configure %s: %v\n", agent.Name, err)
				} else {
					fmt.Fprintf(out, "%s configured. Restart %s to activate memoryweb.\n",
						agent.Name, agent.Name)
				}
			}
		}
	}

	// ── Ollama ────────────────────────────────────────────────────────────────

	setupOllama(out, reader, dryRun)
	return nil
}

// setupOllama checks whether Ollama is installed and whether the configured
// embedding model is pulled, prompting the user to install/pull as needed.
// In dry-run mode it reports what would happen without prompting.
func setupOllama(out io.Writer, in *bufio.Reader, dryRun bool) {
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
	model := db.EmbeddingModel()
	listCmd := exec.Command("ollama", "list")
	listOut, err := listCmd.Output()
	if err != nil || !strings.Contains(string(listOut), model) {
		if dryRun {
			fmt.Fprintf(out, "[dry-run] %s not found — would pull automatically\n", model)
			return
		}
		fmt.Fprintf(out, "Pulling %s model for semantic search...\n", model)
		cmd := exec.Command("ollama", "pull", model)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(out, "Pull failed: %v\n", err)
		}
		return
	}

	fmt.Fprintf(out, "Ollama: %s is ready.\n", model)
}

// setupStartOllama ensures the Ollama server is running. It checks whether
// localhost:11434 is already accepting connections; if not, it starts
// "ollama serve" as a detached background process and polls until ready.
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

	// Poll until the HTTP API responds, up to 30 seconds.
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://localhost:11434/api/tags")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Fprintln(out, "started.")
}

// setupReadYN reads one line from in and returns true if the answer is "y".
// Returns false on EOF or any other input.
func setupReadYN(in *bufio.Reader) bool {
	line, err := in.ReadString('\n')
	if err != nil && len(line) == 0 {
		return false
	}
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

// setupToSlice safely converts an interface{} to []interface{}.
func setupToSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	s, _ := v.([]interface{})
	return s
}

// setupUpsertCommand ensures exactly one entry for cmd exists in entries. If
// an entry whose nested command matches by basename already exists, it is
// replaced (handles install-path changes across releases). Otherwise appended.
func setupUpsertCommand(entries []interface{}, cmd string, newEntry interface{}) []interface{} {
	base := filepath.Base(cmd)
	out := make([]interface{}, 0, len(entries)+1)
	replaced := false
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			out = append(out, entry)
			continue
		}
		hookEntries, _ := entryMap["hooks"].([]interface{})
		match := false
		for _, hookEntry := range hookEntries {
			hookMap, ok := hookEntry.(map[string]interface{})
			if !ok {
				continue
			}
			existing, _ := hookMap["command"].(string)
			if filepath.Base(existing) == base {
				match = true
				break
			}
		}
		if match {
			// Collapses every stale entry for this hook (old install paths or
			// a previous --db) into a single fresh entry carrying the current
			// env — so re-running setup never leaves old hooks firing with a
			// stale MEMORYWEB_DB. Only the fields setup owns (type, command,
			// env) are refreshed; any user-added keys on the kept entry survive.
			if !replaced {
				if fresh, ok := newEntry.(map[string]interface{}); ok {
					merged := make(map[string]interface{}, len(entryMap)+len(fresh))
					for k, v := range entryMap {
						merged[k] = v
					}
					for k, v := range fresh {
						merged[k] = v
					}
					out = append(out, merged)
				} else {
					out = append(out, newEntry)
				}
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, newEntry)
	}
	return out
}

// setupContainsCommand reports whether any entry in the slice contains the
// given command path in its nested "hooks" array.
func setupContainsCommand(entries []interface{}, cmd string) bool {
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hookEntries, _ := entryMap["hooks"].([]interface{})
		for _, hookEntry := range hookEntries {
			hookMap, ok := hookEntry.(map[string]interface{})
			if ok && hookMap["command"] == cmd {
				return true
			}
		}
	}
	return false
}

// ── options subcommand ────────────────────────────────────────────────────────

type optionSpec struct {
	key    string
	defVal interface{} // bool or int
	desc   string
}

var optionSpecs = []optionSpec{
	{"session_orient_enabled", false, "orient() nudge when orient not yet called (UserPromptSubmit hook)"},
	{"auto_recall", false, "inject relevant memories on each prompt (UserPromptSubmit hook)"},
	{"pre_compact_enabled", false, "file before compaction (PreCompact hook)"},
	{"reinject_on_compact", false, "reinject orient (domain+topic) after compaction (PostCompact hook)"},
	{"sweep_interval_turns", 15, "turns between filing prompts; 0 disables (Stop hook)"},
	{"subagent_orient_enabled", false, "orient sub-agents into parent scope + inject digest (SubagentStart hook)"},
	{"subagent_audit_enabled", false, "orphan audit on sub-agent stop (SubagentStop hook)"},
}

func optionsCmd() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	cfgPath := filepath.Join(home, ".memoryweb", "config.json")
	if err := runOptionsCmd(os.Stdout, cfgPath, os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runOptionsCmd(out io.Writer, cfgPath string, args []string) error {
	flags := flag.NewFlagSet("options", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.String("db", "", "database path (ignored; options are stored in config.json, not the database)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("usage: memoryweb options [set <key> <value>]")
	}
	args = flags.Args()

	if len(args) == 0 {
		return optionsPrint(cfgPath, out)
	}
	if args[0] == "set" {
		if len(args) < 3 {
			return fmt.Errorf("'options set' requires a key and value\nUsage: memoryweb options set <key> <value>")
		}
		return optionsSet(cfgPath, args[1], args[2], out)
	}
	return fmt.Errorf("unknown options subcommand %q\nUsage: memoryweb options [set <key> <value>]", args[0])
}

func optionsPrint(cfgPath string, out io.Writer) error {
	cfg := readConfig(cfgPath)
	for _, spec := range optionSpecs {
		fmt.Fprintf(out, "%-30s %-5v  %s\n", spec.key, cfg[spec.key], spec.desc)
	}
	return nil
}

// readConfig reads ~/.memoryweb/config.json, applying defaults for missing keys.
func readConfig(cfgPath string) map[string]interface{} {
	result := make(map[string]interface{})
	for _, spec := range optionSpecs {
		result[spec.key] = spec.defVal
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return result
	}
	var raw map[string]interface{}
	if json.Unmarshal(data, &raw) == nil {
		for k, v := range raw {
			result[k] = v
		}
	}
	return result
}

func optionsSet(cfgPath, key, value string, out io.Writer) error {
	var spec *optionSpec
	for i := range optionSpecs {
		if optionSpecs[i].key == key {
			spec = &optionSpecs[i]
			break
		}
	}
	if spec == nil {
		keys := make([]string, len(optionSpecs))
		for i, spec := range optionSpecs {
			keys[i] = spec.key
		}
		sort.Strings(keys)
		return fmt.Errorf("unknown option %q; valid keys: %s", key, strings.Join(keys, ", "))
	}

	var parsed interface{}
	switch spec.defVal.(type) {
	case bool:
		switch strings.ToLower(value) {
		case "true", "1", "on":
			parsed = true
		case "false", "0", "off":
			parsed = false
		default:
			return fmt.Errorf("invalid value %q for %s: expected true/false/on/off/1/0", value, key)
		}
	case int:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid value %q for %s: expected a non-negative integer", value, key)
		}
		parsed = n
	default:
		return fmt.Errorf("unsupported option type for %s", key)
	}

	cfg := readConfig(cfgPath)
	cfg[key] = parsed

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Fprintf(out, "set %s = %v\n", key, parsed)
	return nil
}

// ── doctor subcommand ─────────────────────────────────────────────────────────

// DoctorCheck is a single diagnostic result produced by runDoctor.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "fail", "warn", "info"
	Message string `json:"message"`
}

// DoctorReport is the full structured output of the doctor command when
// the --json flag is used.
type DoctorReport struct {
	Passed bool          `json:"passed"`
	Checks []DoctorCheck `json:"checks"`
}

// doctorCmd implements the "memoryweb doctor" subcommand.
func doctorCmd() {
	flags := flag.NewFlagSet("doctor", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	jsonFlag := flags.Bool("json", false, "output results as machine-readable JSON")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	home, _ := os.UserHomeDir()
	if !runDoctor(store, os.Stdout, *dbFlag, home, *jsonFlag) {
		os.Exit(1)
	}
}

// runDoctor runs all diagnostic checks, writes output to out, and returns true
// if all checks pass (i.e. no "fail" results). Informational ("info") and warning
// ("warn") results do not affect the return value.
func runDoctor(store *db.Store, out io.Writer, dbPath, home string, jsonMode bool) bool {
	var checks []DoctorCheck

	add := func(name, status, message string) {
		checks = append(checks, DoctorCheck{Name: name, Status: status, Message: message})
	}

	// ── 1. Database ───────────────────────────────────────────────────────────
	applied, expected, schemaErr := store.SchemaVersion()
	switch {
	case schemaErr != nil:
		add("Database", "fail", fmt.Sprintf("%s — could not read schema version: %v", dbPath, schemaErr))
	case applied < expected:
		add("Database", "fail", fmt.Sprintf("%s (WAL, schema v%d — expected v%d; run the binary to migrate)", dbPath, applied, expected))
	default:
		add("Database", "ok", fmt.Sprintf("%s (WAL, schema v%d)", dbPath, applied))
	}

	// ── 2. sqlite-vec / semantic search ───────────────────────────────────────
	vecVer := store.VecVersion()
	live, covered, embErr := store.EmbeddingCoverage()
	switch {
	case embErr != nil:
		add("sqlite-vec", "warn", fmt.Sprintf("error checking coverage: %v", embErr))
	case vecVer == "":
		add("sqlite-vec", "warn", "not available (text search fallback active)")
	case live == 0:
		add("sqlite-vec", "ok", fmt.Sprintf("%s — no nodes to embed", vecVer))
	case covered < live:
		pct := int(float64(covered) * 100 / float64(live))
		add("sqlite-vec", "warn", fmt.Sprintf("%s — %d/%d nodes embedded (%d%%) — run: memoryweb backfill", vecVer, covered, live, pct))
	default:
		add("sqlite-vec", "ok", fmt.Sprintf("%s — %d/%d nodes embedded (100%%)", vecVer, covered, live))
	}

	// ── 3–5. Ollama ───────────────────────────────────────────────────────────
	ollamaBinOK := false
	if _, err := exec.LookPath("ollama"); err != nil {
		add("Ollama binary", "fail", "not found in PATH — install from https://ollama.com/download")
	} else {
		add("Ollama binary", "ok", "found")
		ollamaBinOK = true
	}

	ollamaServerOK := false
	if ollamaBinOK {
		conn, err := net.DialTimeout("tcp", "localhost:11434", time.Second)
		if err != nil {
			add("Ollama server", "fail", "not reachable on localhost:11434 — run: ollama serve")
		} else {
			conn.Close()
			add("Ollama server", "ok", "reachable on localhost:11434")
			ollamaServerOK = true
		}
	} else {
		add("Ollama server", "warn", "skipped (Ollama binary not found)")
	}

	if ollamaServerOK {
		listOut, err := exec.Command("ollama", "list").Output()
		model := db.EmbeddingModel()
		if err != nil || !strings.Contains(string(listOut), model) {
			add("Ollama model", "fail", model+" not found — run: ollama pull "+model)
		} else {
			add("Ollama model", "ok", model+" ready")
		}
	} else {
		add("Ollama model", "warn", "skipped (Ollama server not available)")
	}

	// ── 6. Claude Code hooks ──────────────────────────────────────────────────
	hooksMsg, hooksStatus := doctorCheckHooks(home)
	add("Claude hooks", hooksStatus, hooksMsg)

	// ── 7. Graph stats (informational) ────────────────────────────────────────
	liveNodes, archivedNodes, nodeErr := store.NodeCounts()
	edges, edgeErr := store.EdgeCount()
	domains, domErr := store.ListDomains()
	aliases, aliasErr := store.ListAliases()

	if nodeErr != nil || edgeErr != nil || domErr != nil || aliasErr != nil {
		add("Graph", "info", "error reading graph stats")
	} else {
		domainStr := fmt.Sprintf("%d domain(s)", len(domains))
		if len(domains) > 0 {
			domainStr = fmt.Sprintf("%d domain(s) (%s)", len(domains), strings.Join(domains, ", "))
		}
		add("Graph", "info", fmt.Sprintf("%d live nodes, %d archived, %d edges, %s, %d alias(es)",
			liveNodes, archivedNodes, edges, domainStr, len(aliases)))
	}

	// ── 8. Drift snapshot (informational) ─────────────────────────────────────
	drift, driftErr := store.FindDrift("", 100, nil, nil, "", 2)
	if driftErr != nil {
		add("Drift", "info", fmt.Sprintf("error reading drift candidates: %v", driftErr))
	} else if len(drift) == 0 {
		add("Drift", "info", "no candidates")
	} else {
		cats := map[string]int{}
		for _, drift := range drift {
			switch {
			case strings.HasPrefix(drift.Reason, "explicitly marked"):
				cats["contradicts"]++
			case strings.HasPrefix(drift.Reason, "label suggests"):
				cats["stale labels"]++
			case strings.HasPrefix(drift.Reason, "open question"):
				cats["old open questions"]++
			case strings.HasPrefix(drift.Reason, "possible duplicate"):
				cats["duplicates"]++
			case strings.HasPrefix(drift.Reason, "standing rule"):
				cats["low-connection standing rules"]++
			case strings.HasPrefix(drift.Reason, "connected placeholder"):
				cats["resolved placeholders"]++
			default:
				cats["transient"]++
			}
		}
		var parts []string
		for _, key := range []string{"contradicts", "stale labels", "old open questions", "duplicates", "low-connection standing rules", "resolved placeholders", "transient"} {
			if count := cats[key]; count > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", count, key))
			}
		}
		add("Drift", "info", fmt.Sprintf("%d candidate(s): %s", len(drift), strings.Join(parts, ", ")))
	}

	// ── 9. Audit log recency (informational) ──────────────────────────────────
	entry, ok, auditErr := store.LastAuditEntry()
	if auditErr != nil {
		add("Last activity", "info", fmt.Sprintf("error reading audit log: %v", auditErr))
	} else if !ok {
		add("Last activity", "info", "(no activity recorded)")
	} else {
		add("Last activity", "info", fmt.Sprintf("%s %s (node %q)",
			entry.ActionedAt.Format("2006-01-02"), entry.Action, entry.NodeLabel))
	}

	// ── 10. Update check ──────────────────────────────────────────────────────
	if Version == "dev" {
		add("Update", "info", "running dev build — skipping update check")
	} else {
		latest, updateErr := checkLatestRelease()
		switch {
		case updateErr != nil:
			add("Update", "info", "could not check (offline or rate-limited)")
		case latest == Version:
			add("Update", "ok", fmt.Sprintf("up to date (%s)", Version))
		default:
			add("Update", "warn", fmt.Sprintf("%s available — download from https://github.com/corbym/memoryweb/releases/latest", latest))
		}
	}

	// ── determine overall pass/fail ───────────────────────────────────────────
	passed := true
	for _, c := range checks {
		if c.Status == "fail" {
			passed = false
			break
		}
	}

	// ── emit output ───────────────────────────────────────────────────────────
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(DoctorReport{Passed: passed, Checks: checks}); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode JSON: %v\n", err)
		}
		return passed
	}

	for _, c := range checks {
		sym := map[string]string{
			"ok":   "✓",
			"fail": "✗",
			"warn": "!",
			"info": "i",
		}[c.Status]
		fmt.Fprintf(out, "[%s] %-16s %s\n", sym, c.Name+":", c.Message)
	}
	return passed
}

// doctorCheckHooks inspects ~/.claude/settings.json and returns a
// human-readable message and status about the memoryweb hook configuration.
// It validates every hook `setup` installs (Stop/save, PreCompact,
// UserPromptSubmit, SubagentStart, SubagentStop, PostCompact).
func doctorCheckHooks(home string) (message, status string) {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return "settings.json not found — run: memoryweb setup", "fail"
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Sprintf("settings.json is not valid JSON: %v", err), "fail"
	}

	hooks, _ := settings["hooks"].(map[string]interface{})

	type hookCheck struct {
		event  string // Claude Code hook event key
		name   string // human-readable name for reports
		script string // hook script filename
	}
	checks := []hookCheck{
		{"Stop", "Stop", "memoryweb_save_hook.sh"},
		{"PreCompact", "PreCompact", "memoryweb_precompact_hook.sh"},
		{"UserPromptSubmit", "UserPromptSubmit", "memoryweb_userpromptsubmit_hook.sh"},
		{"SubagentStart", "SubagentStart", "memoryweb_subagent_start_hook.sh"},
		{"SubagentStop", "SubagentStop", "memoryweb_subagent_stop_hook.sh"},
		{"PostCompact", "PostCompact", "memoryweb_postcompact_hook.sh"},
	}

	var issues []string
	installed := 0
	for _, c := range checks {
		cmd := doctorFindHookCommand(setupToSlice(hooks[c.event]), c.script)
		if cmd == "" {
			issues = append(issues, c.name+" hook missing")
			continue
		}
		installed++
		info, err := os.Stat(cmd)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s hook script missing: %s", c.name, cmd))
		} else if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			issues = append(issues, fmt.Sprintf("%s hook not executable: %s", c.name, cmd))
		}
	}

	if len(issues) == 0 {
		return "All hooks installed", "ok"
	}
	if installed == 0 {
		return strings.Join(issues, "; ") + " — run: memoryweb setup", "fail"
	}
	return strings.Join(issues, "; "), "warn"
}

// doctorFindHookCommand scans a hooks slice for the first command path that
// ends with the given suffix (e.g. "memoryweb_save_hook.sh").
func doctorFindHookCommand(entries []interface{}, suffix string) string {
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hookEntries, _ := entryMap["hooks"].([]interface{})
		for _, hookEntry := range hookEntries {
			hookMap, ok := hookEntry.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hookMap["command"].(string)
			if strings.HasSuffix(cmd, suffix) {
				return cmd
			}
		}
	}
	return ""
}

// checkLatestRelease fetches the latest release tag from GitHub and returns it.
// It returns an error on network failure, non-200 response, or malformed JSON.
func checkLatestRelease() (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/corbym/memoryweb/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "memoryweb")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("empty tag_name in response")
	}
	return body.TagName, nil
}

// mergeDomainsCmd implements the "memoryweb merge-domains" subcommand.
func mergeDomainsCmd() {
	flags := flag.NewFlagSet("merge-domains", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	source := flags.String("source", "", "source domain to merge from (required)")
	target := flags.String("target", "", "target domain to merge into (required)")
	dryRun := flags.Bool("dry-run", false, "print what would happen without making changes")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	if *source == "" || *target == "" {
		fmt.Fprintln(os.Stderr, "error: --source and --target are required")
		flags.Usage()
		os.Exit(1)
	}

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := runMergeDomains(store, os.Stdout, *source, *target, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func backupCmd() {
	flags := flag.NewFlagSet("backup", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: memoryweb backup [--db <path>] <destination>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Writes a consistent standalone snapshot (single file, no -wal/-shm sidecars)")
		fmt.Fprintln(os.Stderr, "that is safe to copy or sync even while memoryweb is running. Never copy the")
		fmt.Fprintln(os.Stderr, "live .db folder directly — the .db and -wal can desync and corrupt.")
		fmt.Fprintln(os.Stderr, "")
		flags.PrintDefaults()
	}
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	rest := flags.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "error: exactly one destination path is required")
		flags.Usage()
		os.Exit(1)
	}

	if err := db.Backup(*dbFlag, rest[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("backup written to %s\n", rest[0])
}

// runMergeDomains performs or previews the domain merge. Separated from
// mergeDomainsCmd so tests can inject a writer.
func runMergeDomains(store *db.Store, out io.Writer, source, target string, dryRun bool) error {
	result, err := store.MergeDomains(source, target, dryRun)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Fprintf(out, "Would move %d node(s) from %q to %q.", result.NodesMoved, source, target)
		if len(result.LabelCollisions) == 0 {
			fmt.Fprintln(out, " No label collisions detected.")
		} else {
			fmt.Fprintf(out, " %d label collision(s) detected: %s\n",
				len(result.LabelCollisions), strings.Join(result.LabelCollisions, ", "))
		}
		return nil
	}

	fmt.Fprintf(out, "Moved %d node(s) from %q to %q. Alias %q → %q created.",
		result.NodesMoved, source, target, source, target)
	if len(result.LabelCollisions) == 0 {
		fmt.Fprintln(out, "")
	} else {
		fmt.Fprintf(out, " %d label collision(s): %s\n",
			len(result.LabelCollisions), strings.Join(result.LabelCollisions, ", "))
	}
	return nil
}

func purgeCmd() {
	flags := flag.NewFlagSet("purge", flag.ExitOnError)
	dbFlag := flags.String("db", resolveDBPath(), "path to the SQLite database file")
	domainFlag := flags.String("domain", "", "scope purge to this domain only")
	beforeFlag := flags.String("before", "", "purge only nodes archived before this ISO8601 date (e.g. 2026-01-01)")
	dryRun := flags.Bool("dry-run", false, "print what would be purged without deleting anything")
	confirm := flags.Bool("confirm", false, "required to actually execute; without it nothing is deleted")
	includeLive := flags.Bool("include-live", false, "also hard-delete live (non-archived) nodes matching --domain — irreversible, requires --domain")
	flags.Parse(os.Args[2:]) //nolint:errcheck // ExitOnError handles the error

	if !*dryRun && !*confirm {
		fmt.Fprintln(os.Stderr, "warning: no action taken. Use --confirm to purge archived nodes, or --dry-run to preview.")
		os.Exit(1)
	}

	if *includeLive && *domainFlag == "" {
		fmt.Fprintln(os.Stderr, "error: --include-live requires --domain (refusing to hard-delete every live node in the database)")
		os.Exit(1)
	}

	var beforeTime *time.Time
	if *beforeFlag != "" {
		t, err := time.Parse(time.RFC3339, *beforeFlag)
		if err != nil {
			t, err = time.Parse("2006-01-02", *beforeFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --before value (use ISO8601 date or datetime): %s\n", *beforeFlag)
				os.Exit(1)
			}
		}
		beforeTime = &t
	}

	store, err := db.New(*dbFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	result, err := store.Purge(*domainFlag, beforeTime, *dryRun, *includeLive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("DRY RUN — no changes will be made.\n")
		fmt.Printf("%d node(s) would be purged:\n", len(result.Nodes))
		for _, node := range result.Nodes {
			archived := "live"
			if node.ArchivedAt != nil {
				archived = node.ArchivedAt.UTC().Format(time.RFC3339)
			}
			fmt.Printf("  - %s (id: %s, archived: %s)\n", node.Label, node.ID, archived)
		}
		printLiveRemaining(result, *domainFlag, *includeLive)
		return
	}

	fmt.Printf("%d node(s) purged, %d edge(s) removed\n", len(result.Nodes), result.TotalEdges)
	printLiveRemaining(result, *domainFlag, *includeLive)
}

// printLiveRemaining tells the operator when a domain-scoped purge left live
// (non-archived) nodes untouched — otherwise "0 archived candidates" reads as
// "domain is empty" when it may still hold live nodes that were never
// archived. Silent when there's no domain filter, includeLive was used (there
// is nothing left to report), or the domain genuinely has no live nodes.
func printLiveRemaining(result db.PurgeResult, domain string, includeLive bool) {
	if domain == "" || includeLive || result.LiveRemaining == 0 {
		return
	}
	fmt.Printf("note: %d live node(s) still exist in domain %q — archive them first (forget), "+
		"then re-run purge, or use --include-live to remove them directly\n", result.LiveRemaining, domain)
}

func dispatch(req Request, h *tools.Handler, rec *stats.Recorder) (interface{}, *RPCError) {
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
		// Record the call for stats if enabled.
		if rec != nil {
			if toolResult, ok := result.(*tools.ToolResult); ok {
				text := ""
				if len(toolResult.Content) > 0 {
					text = toolResult.Content[0].Text
				}
				var callReq struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				json.Unmarshal(req.Params, &callReq)
				rec.Record(callReq.Name, callReq.Arguments, text, toolResult.IsError)
			}
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
			"version": Version,
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
