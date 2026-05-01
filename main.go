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
		case "--version", "-v", "version":
			fmt.Println(Version)
			return
		case "dream":
			dreamCmd()
			return
		case "backfill":
			backfillCmd()
			return
		case "setup":
			setupCmd()
			return
		case "doctor":
			doctorCmd()
			return
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
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(configPath, output, 0644)
}

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

// runSetup installs Claude Code hooks into ~/.claude/settings.local.json,
// detects desktop MCP clients (Claude Desktop, ChatGPT Desktop) and offers to
// configure each one, then optionally sets up Ollama for semantic search.
// Separated from setupCmd so tests can inject writers and readers.
func runSetup(out io.Writer, in io.Reader, dryRun bool, dbPath, hooksDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	// Wrap in with a bufio.Reader once so that all y/N prompts share the same
	// buffered reader and successive calls do not lose unconsumed bytes.
	br := bufio.NewReader(in)

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

	// ── Claude Code hooks ─────────────────────────────────────────────────────

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

	claudeOutput, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if dryRun {
		fmt.Fprintln(out, string(claudeOutput))
	} else {
		stateDir := filepath.Join(home, ".memoryweb", "hook_state")
		if err := os.MkdirAll(stateDir, 0755); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
			return fmt.Errorf("create .claude dir: %w", err)
		}
		if err := os.WriteFile(settingsPath, claudeOutput, 0644); err != nil {
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
			if setupReadYN(br) {
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

	setupOllama(out, br, dryRun)
	return nil
}

// setupOllama checks whether Ollama is installed and whether the
// snowflake-arctic-embed model is pulled, prompting the user to install/pull
// as needed. In dry-run mode it reports what would happen without prompting.
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
	listCmd := exec.Command("ollama", "list")
	listOut, err := listCmd.Output()
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
	Passed bool           `json:"passed"`
	Checks []DoctorCheck  `json:"checks"`
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
		if err != nil || !strings.Contains(string(listOut), "snowflake-arctic-embed") {
			add("Ollama model", "fail", "snowflake-arctic-embed not found — run: ollama pull snowflake-arctic-embed")
		} else {
			add("Ollama model", "ok", "snowflake-arctic-embed ready")
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
	drift, driftErr := store.FindDrift("", 100)
	if driftErr != nil {
		add("Drift", "info", fmt.Sprintf("error reading drift candidates: %v", driftErr))
	} else if len(drift) == 0 {
		add("Drift", "info", "no candidates")
	} else {
		cats := map[string]int{}
		for _, d := range drift {
			switch {
			case strings.HasPrefix(d.Reason, "explicitly marked"):
				cats["contradicts"]++
			case strings.HasPrefix(d.Reason, "label suggests"):
				cats["stale labels"]++
			case strings.HasPrefix(d.Reason, "open question"):
				cats["old open questions"]++
			case strings.HasPrefix(d.Reason, "possible duplicate"):
				cats["duplicates"]++
			default:
				cats["transient"]++
			}
		}
		var parts []string
		for _, key := range []string{"contradicts", "stale labels", "old open questions", "duplicates", "transient"} {
			if n := cats[key]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, key))
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

// doctorCheckHooks inspects ~/.claude/settings.local.json and returns a
// human-readable message and status about the memoryweb hook configuration.
func doctorCheckHooks(home string) (message, status string) {
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return "settings.local.json not found — run: memoryweb setup", "fail"
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Sprintf("settings.local.json is not valid JSON: %v", err), "fail"
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	saveCmd := doctorFindHookCommand(setupToSlice(hooks["Stop"]), "memoryweb_save_hook.sh")
	precompactCmd := doctorFindHookCommand(setupToSlice(hooks["PreCompact"]), "memoryweb_precompact_hook.sh")

	var issues []string
	if saveCmd == "" {
		issues = append(issues, "Stop/save hook not found")
	} else if info, err := os.Stat(saveCmd); err != nil {
		issues = append(issues, fmt.Sprintf("save hook script missing: %s", saveCmd))
	} else if info.Mode()&0o111 == 0 {
		issues = append(issues, fmt.Sprintf("save hook not executable: %s", saveCmd))
	}

	if precompactCmd == "" {
		issues = append(issues, "PreCompact hook not found")
	} else if info, err := os.Stat(precompactCmd); err != nil {
		issues = append(issues, fmt.Sprintf("precompact hook script missing: %s", precompactCmd))
	} else if info.Mode()&0o111 == 0 {
		issues = append(issues, fmt.Sprintf("precompact hook not executable: %s", precompactCmd))
	}

	if len(issues) == 0 {
		return "Stop and PreCompact hooks installed", "ok"
	}
	if saveCmd == "" && precompactCmd == "" {
		return strings.Join(issues, "; ") + " — run: memoryweb setup", "fail"
	}
	return strings.Join(issues, "; "), "warn"
}

// doctorFindHookCommand scans a hooks slice for the first command path that
// ends with the given suffix (e.g. "memoryweb_save_hook.sh").
func doctorFindHookCommand(entries []interface{}, suffix string) string {
	for _, e := range entries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		hs, _ := entry["hooks"].([]interface{})
		for _, h := range hs {
			hm, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
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
			if tr, ok := result.(*tools.ToolResult); ok {
				text := ""
				if len(tr.Content) > 0 {
					text = tr.Content[0].Text
				}
				var callReq struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				json.Unmarshal(req.Params, &callReq)
				rec.Record(callReq.Name, callReq.Arguments, text, tr.IsError)
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
