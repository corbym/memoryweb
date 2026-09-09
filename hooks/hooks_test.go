package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/tools"
)

// dreamBin is the path to the compiled dream binary, built once by TestMain.
var dreamBin string

// TestMain builds the dream binary before running all hook tests.
func TestMain(m *testing.M) {
	// Locate repo root by walking up from the working directory.
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	root := ""
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			root = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fmt.Fprintln(os.Stderr, "FAIL: could not find repo root (go.mod not found)")
			os.Exit(1)
		}
		dir = parent
	}

	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-hooks-%d%s", os.Getpid(), exeSuffix))
	buildCmd := exec.Command("go", "build", "-o", bin, ".")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot build memoryweb: %v\n%s\n", err, out)
		os.Exit(1)
	}
	dreamBin = bin

	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

func hooksDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "hooks")
}

// makeTranscript creates a JSONL transcript with numUser real user-prompt messages
// under projectsDir/<project>/<sessionID>.jsonl, using the Claude Code JSONL shape:
// real prompts have "type":"user" and no "toolUseResult" key.
func makeTranscript(t *testing.T, projectsDir, sessionID string, numUser int) {
	t.Helper()
	makeTranscriptWithToolResults(t, projectsDir, sessionID, numUser, 0)
}

// makeTranscriptWithToolResults creates a JSONL transcript with numUser real user-prompt
// messages followed by numToolResults tool-result messages. Tool-result lines carry
// "toolUseResult" and must not be counted toward the save interval.
func makeTranscriptWithToolResults(t *testing.T, projectsDir, sessionID string, numUser, numToolResults int) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("makeTranscriptWithToolResults: mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(projectDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("makeTranscriptWithToolResults: create: %v", err)
	}
	defer f.Close()
	for i := 0; i < numUser; i++ {
		// Real user prompt: "type":"user", content is a string, no toolUseResult key.
		fmt.Fprintf(f, `{"type":"user","message":{"role":"user","content":"message %d"},"origin":"user","promptSource":"user"}`+"\n", i+1)
		fmt.Fprintf(f, `{"type":"assistant","message":{"role":"assistant","content":"reply %d"}}`+"\n", i+1)
	}
	for i := 0; i < numToolResults; i++ {
		// Tool result: "type":"user" but carries "toolUseResult" — must not be counted.
		fmt.Fprintf(f, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_%d","content":"result"}]},"toolUseResult":{"invocationId":"toolu_%d","name":"some_tool"}}`+"\n", i+1, i+1)
	}
}

// shellCmd returns a command that executes a shell script. On Windows, .sh
// files are not directly executable so the script is passed to bash.
// Git Bash is preferred (it uses MSYS2 /c/... paths); WSL bash is used as
// a fallback (it uses /mnt/c/... paths).
func shellCmd(script string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.Command(script)
	}
	// Prefer Git Bash — it uses MSYS2 paths (/c/...).
	gitBashCandidates := []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	}
	for _, candidate := range gitBashCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return exec.Command(candidate, winToMSYS2(script))
		}
	}
	// Fall back to WSL bash — it uses /mnt/c/... paths.
	bash, err := exec.LookPath("bash")
	if err != nil {
		bash = "bash" // will fail with a clear error if bash is not installed
	}
	return exec.Command(bash, winToWSL(script))
}

// winToMSYS2 converts a Windows path (C:\foo\bar) to MSYS2/Git Bash POSIX
// path (/c/foo/bar) so scripts can be located by the Git Bash runtime.
func winToMSYS2(p string) string {
	p = strings.ReplaceAll(p, `\`, `/`)
	// C:/foo → /c/foo
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		p = "/" + strings.ToLower(string(p[0])) + p[2:]
	}
	return p
}

// winToWSL converts a Windows path to WSL path (/mnt/c/...).
func winToWSL(p string) string {
	p = strings.ReplaceAll(p, `\`, `/`)
	// C:/foo → /mnt/c/foo
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		p = "/mnt/" + strings.ToLower(string(p[0])) + p[2:]
	}
	return p
}

// runHook executes a hook script with JSON payload on stdin and custom env.
func runHook(t *testing.T, script, sessionID, stateDir, projectsDir string) (string, int) {
	t.Helper()
	return runHookExtra(t, script, sessionID, stateDir, projectsDir)
}

// runHookExtra is like runHook but accepts additional environment variables
// (e.g. MEMORYWEB_DB, MEMORYWEB_DREAM_BIN) to pass to the hook process.
func runHookExtra(t *testing.T, script, sessionID, stateDir, projectsDir string, extraEnv ...string) (string, int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(string(payload))
	env := append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
		"MEMORYWEB_SAVE_INTERVAL=15",
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec hook %s: %v", script, err)
		}
	}
	return string(out), code
}

// runSubagentStartHook runs the SubagentStart hook with a payload carrying the
// subagent's own session_id plus a transcript_path pointing at the parent
// (main) session's transcript — the real-session shape Claude Code sends.
func runSubagentStartHook(t *testing.T, script, subagentSession, parentTranscriptPath, stateDir, projectsDir string, extraEnv ...string) (string, int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"session_id":      subagentSession,
		"transcript_path": parentTranscriptPath,
	})
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(string(payload))
	env := append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec SubagentStart hook %s: %v", script, err)
		}
	}
	return string(out), code
}

// ── save hook tests ───────────────────────────────────────────────────────────

func TestSaveHookAllowsBelowThreshold(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-below"

	makeTranscript(t, projectsDir, sessionID, 5) // 5 < 15

	out, code := runHook(t, saveHook, sessionID, stateDir, projectsDir)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true for 5 messages below threshold; got:\n%s", out)
	}
}

func TestSaveHookDoesNotCountToolResults(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-no-tool-count"

	// 5 real user messages + 20 tool-result messages = 5 real, still below threshold (15).
	makeTranscriptWithToolResults(t, projectsDir, sessionID, 5, 20)

	out, code := runHookExtra(t, saveHook, sessionID, stateDir, projectsDir,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when only 5 real user messages (20 tool-result lines excluded); got:\n%s", out)
	}
}

func TestSaveHookBlocksAtThreshold(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-block"

	makeTranscript(t, projectsDir, sessionID, 15) // 15 >= 15

	// Inhibit the dream binary so the test doesn't touch any real DB and
	// doesn't rely on memoryweb being on PATH.
	out, _ := runHookExtra(t, saveHook, sessionID, stateDir, projectsDir,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("expected decision:block for 15 messages at threshold; got:\n%s", out)
	}
	if strings.Contains(out, `"continue":false`) {
		t.Errorf("should not contain continue:false (halts session instead of blocking); got:\n%s", out)
	}
	if strings.Contains(out, `"stopReason"`) {
		t.Errorf("should not contain stopReason (shown to user, not model); got:\n%s", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON: %v\ngot:\n%s", err, out)
	}
	if reason, _ := envelope["reason"].(string); reason == "" {
		t.Errorf("reason field is empty or missing in:\n%s", out)
	}
	savingFlag := filepath.Join(stateDir, sessionID+".saving")
	if _, err := os.Stat(savingFlag); err != nil {
		t.Errorf(".saving flag not created: %v", err)
	}
}

func TestSaveHookAllowsOnReentry(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-reentry"

	makeTranscript(t, projectsDir, sessionID, 15)

	// Pre-create the saving flag (simulates re-entry after AI filed).
	savingFlag := filepath.Join(stateDir, sessionID+".saving")
	if err := os.WriteFile(savingFlag, []byte{}, 0644); err != nil {
		t.Fatalf("create saving flag: %v", err)
	}

	out, code := runHook(t, saveHook, sessionID, stateDir, projectsDir)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true on re-entry; got:\n%s", out)
	}
	if _, err := os.Stat(savingFlag); err == nil {
		t.Error(".saving flag should have been deleted on re-entry")
	}
}

// ── precompact hook tests ─────────────────────────────────────────────────────

func runPrecompactHook(t *testing.T, stateDir, sessionID string) (string, int) {
	t.Helper()
	// Enable the hook so tests exercise its core logic, not the option guard.
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"pre_compact_enabled": true})
	script := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"HOME="+home,
		// Inhibit dream so basic tests don't depend on a real DB or binary.
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec precompact hook: %v", err)
		}
	}
	return string(out), code
}

func TestPrecompactHookBlocks(t *testing.T) {
	stateDir := t.TempDir()
	sessionID := "test-precompact-block"

	out, _ := runPrecompactHook(t, stateDir, sessionID)
	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false on first run; got:\n%s", out)
	}
	compactingFlag := filepath.Join(stateDir, sessionID+".compacting")
	if _, err := os.Stat(compactingFlag); err != nil {
		t.Errorf(".compacting flag not created: %v", err)
	}
}

func TestPrecompactHookAllowsOnReentry(t *testing.T) {
	stateDir := t.TempDir()
	sessionID := "test-precompact-reentry"

	// Pre-create the compacting flag.
	compactingFlag := filepath.Join(stateDir, sessionID+".compacting")
	if err := os.WriteFile(compactingFlag, []byte{}, 0644); err != nil {
		t.Fatalf("create compacting flag: %v", err)
	}

	out, code := runPrecompactHook(t, stateDir, sessionID)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true on re-entry; got:\n%s", out)
	}
	if _, err := os.Stat(compactingFlag); err == nil {
		t.Error(".compacting flag should have been deleted on re-entry")
	}
}

// ── save hook + dream integration tests ───────────────────────────────────────

// seedRealisticDB populates a Store with a realistic dataset that exercises all
// code paths relevant to the dream digest: recent nodes across multiple domains,
// a superseded-label node (drift rule 2), and a contradicting pair (drift rule 1).
//
// It uses tools.Handler.CallTool — the same interface an MCP agent would use —
// so the test exercises the real remember / connect code paths.
func seedRealisticDB(t *testing.T, dbPath string) {
	t.Helper()

	store, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("seedRealisticDB: db.New: %v", err)
	}
	defer store.Close()

	h := tools.New(store, "dev", nil)

	call := func(name string, args map[string]any) string {
		t.Helper()
		argBytes, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("seedRealisticDB: marshal args for %q: %v", name, err)
		}
		params, err := json.Marshal(map[string]any{
			"name":      name,
			"arguments": json.RawMessage(argBytes),
		})
		if err != nil {
			t.Fatalf("seedRealisticDB: marshal params for %q: %v", name, err)
		}
		raw, err := h.CallTool(params)
		if err != nil {
			t.Fatalf("seedRealisticDB: CallTool(%q): %v", name, err)
		}
		type result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		b, err := json.Marshal(raw)
		if err != nil {
			t.Fatalf("seedRealisticDB: marshal result for %q: %v", name, err)
		}
		var r result
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatalf("seedRealisticDB: unmarshal result for %q: %v", name, err)
		}
		if r.IsError {
			t.Fatalf("seedRealisticDB: tool %q returned error: %v", name, r.Content)
		}
		// Try new shape first ({node: {id: ...}}), then fall back to root {id: ...}.
		var nodeResp struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			ID string `json:"id"`
		}
		if len(r.Content) > 0 {
			if err := json.Unmarshal([]byte(r.Content[0].Text), &nodeResp); err != nil {
				t.Fatalf("seedRealisticDB: unmarshal node ID from %q response: %v", name, err)
			}
		}
		if nodeResp.Node.ID != "" {
			return nodeResp.Node.ID
		}
		return nodeResp.ID
	}

	// Domain: deep-game — a game development project with decisions, findings,
	// and a superseded renderer approach.
	webglID := call("remember", map[string]any{
		"label":       "WebGL Renderer Architecture Decision",
		"domain":      "deep-game",
		"description": "Chose WebGL for card rendering to support 3D flip animations at 60fps.",
		"why_matters": "Sets the rendering budget for all card animations and constrains which browsers are supported.",
		"tags":        "rendering architecture graphics performance",
	})
	cssID := call("remember", map[string]any{
		"label":       "CSS Transform Animation Approach",
		"domain":      "deep-game",
		"description": "Alternative proposal: use CSS perspective transforms instead of WebGL for card flips.",
		"why_matters": "Simpler to implement but cannot reach 60fps on low-end Android; rejected in favour of WebGL.",
		"tags":        "animation css frontend alternative",
	})
	call("remember", map[string]any{
		"label":       "Card Flip Animation Design",
		"domain":      "deep-game",
		"description": "Cards flip with a 180-degree Y-axis rotation over 300ms using a WebGL shader.",
		"why_matters": "Core UX interaction; animation smoothness directly affects perceived game quality.",
		"tags":        "animation ux card shader",
	})
	call("remember", map[string]any{
		"label":       "Multiplayer Sync Protocol Choice",
		"domain":      "deep-game",
		"description": "Decided on CRDT-based state sync over WebSockets to handle offline play.",
		"why_matters": "Enables peer-to-peer play without a central game server; reduces infrastructure cost.",
		"tags":        "multiplayer sync crdt websocket protocol",
	})
	// Superseded approach — label contains "Old" → drift rule 2.
	call("remember", map[string]any{
		"label":       "Old Canvas-Based Renderer",
		"domain":      "deep-game",
		"description": "Original renderer using 2D canvas; replaced by WebGL pipeline in sprint 4.",
		"why_matters": "Retained for reference; documents why canvas was insufficient for 60fps requirements.",
		"tags":        "canvas renderer superseded legacy",
	})

	// Contradicting pair — drift rule 1.
	call("connect", map[string]any{
		"from_memory":  cssID,
		"to_memory":    webglID,
		"relationship": "contradicts",
		"narrative":    "CSS transform approach directly contradicts the WebGL decision: they cannot both be the canonical animation strategy.",
	})

	// Domain: memoryweb-meta — decisions about this tool itself.
	call("remember", map[string]any{
		"label":       "Hook Save Interval Configuration",
		"domain":      "memoryweb-meta",
		"description": "Save interval defaulted to 15 human messages to balance overhead vs. filing frequency.",
		"why_matters": "Too frequent interrupts workflow; too infrequent risks losing session context at compaction.",
		"tags":        "hook configuration interval tuning",
	})
	call("remember", map[string]any{
		"label":       "Dream Tool for Session Orientation",
		"domain":      "memoryweb-meta",
		"description": "Introduced a dream subcommand that surfaces recent nodes and drift candidates at hook trigger time.",
		"why_matters": "Gives Claude context before filing so it can connect new nodes to existing knowledge rather than creating duplicates.",
		"tags":        "dream hook orientation drift context",
	})
}

// TestSaveHookEmbedsDreamDigest verifies that when the save hook fires at
// threshold, it runs the dream binary and embeds its output — including recent
// node labels and drift candidates — in the reason so Claude can act on it.
func TestSaveHookEmbedsDreamDigest(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	sessionID := "test-dream-integration"

	// Seed the DB with a realistic dataset.
	seedRealisticDB(t, dbPath)

	// Create a transcript that exceeds the save interval (15 messages).
	makeTranscript(t, projectsDir, sessionID, 15)

	out, code := runHookExtra(t, saveHook, sessionID, stateDir, projectsDir,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}

	// ── structural assertions ─────────────────────────────────────────────────

	// Hook must block at threshold.
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("expected decision:block at threshold; got:\n%s", out)
	}
	if strings.Contains(out, `"continue":false`) {
		t.Errorf("should not contain continue:false; got:\n%s", out)
	}

	// Output must be a single-line JSON object (no literal newlines inside the
	// JSON value that would make it invalid).
	line := strings.TrimSpace(out)
	if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
		t.Errorf("hook output does not look like a JSON object; got:\n%s", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON: %v\ngot:\n%s", err, out)
	}

	reason, _ := envelope["reason"].(string)
	if reason == "" {
		t.Fatalf("reason is empty or missing in:\n%s", out)
	}

	// ── dream header ──────────────────────────────────────────────────────────

	if !strings.Contains(reason, "memoryweb dream") {
		t.Errorf("reason should contain the dream header 'memoryweb dream'; got:\n%s", reason)
	}

	// ── recent nodes ──────────────────────────────────────────────────────────
	// At least two recently filed node labels must appear so Claude knows what
	// context already exists.

	if !strings.Contains(reason, "WebGL Renderer Architecture Decision") {
		t.Errorf("reason should contain recent node 'WebGL Renderer Architecture Decision'; got:\n%s", reason)
	}
	if !strings.Contains(reason, "Dream Tool for Session Orientation") {
		t.Errorf("reason should contain recent node 'Dream Tool for Session Orientation'; got:\n%s", reason)
	}

	// ── drift candidates ──────────────────────────────────────────────────────
	// The superseded-label node and the contradicting pair must surface so Claude
	// knows which nodes need organising.

	if !strings.Contains(reason, "Old Canvas-Based Renderer") {
		t.Errorf("reason should surface drift candidate 'Old Canvas-Based Renderer'; got:\n%s", reason)
	}
	if !strings.Contains(reason, "CSS Transform Animation Approach") {
		t.Errorf("reason should surface the contradicting node 'CSS Transform Animation Approach'; got:\n%s", reason)
	}

	// ── actionable guidance ───────────────────────────────────────────────────
	// The filing instructions must follow the digest so Claude knows what to do.

	if !strings.Contains(reason, "remember with an items array") {
		t.Errorf("reason should contain 'remember with an items array' filing instruction; got:\n%s", reason)
	}
	if !strings.Contains(reason, "why_matters") {
		t.Errorf("reason should contain 'why_matters' guidance; got:\n%s", reason)
	}
}

// TestSaveHookBlocksGracefullyWithoutDreamBin verifies that the hook still
// blocks correctly and produces a valid reason when the dream binary is
// not present — ensuring the digest is genuinely optional.
func TestSaveHookBlocksGracefullyWithoutDreamBin(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-no-dream-bin"

	makeTranscript(t, projectsDir, sessionID, 15)

	// Point MEMORYWEB_BIN at a non-existent path.
	out, code := runHookExtra(t, saveHook, sessionID, stateDir, projectsDir,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-dream",
	)
	if code != 0 {
		t.Fatalf("hook exited %d without dream binary; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("expected decision:block even without dream binary; got:\n%s", out)
	}
	if strings.Contains(out, `"continue":false`) {
		t.Errorf("should not contain continue:false; got:\n%s", out)
	}
	line := strings.TrimSpace(out)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON without dream binary: %v\ngot:\n%s", err, out)
	}
	reason, _ := envelope["reason"].(string)
	if !strings.Contains(reason, "remember with an items array") {
		t.Errorf("reason should still contain filing instructions; got:\n%s", reason)
	}
}

// ── precompact hook + dream integration tests ─────────────────────────────────

// TestPrecompactHookEmbedsDreamDigest verifies that when the precompact hook
// fires, it runs the dream binary and embeds its output in the stopReason.
func TestPrecompactHookEmbedsDreamDigest(t *testing.T) {
	precompactHook := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	stateDir := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")
	sessionID := "test-precompact-dream"

	seedRealisticDB(t, dbPath)
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"pre_compact_enabled": true})

	out, code := runHookExtra(t, precompactHook, sessionID, stateDir, t.TempDir(),
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
		"HOME="+home,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}

	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false; got:\n%s", out)
	}

	line := strings.TrimSpace(out)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON: %v\ngot:\n%s", err, out)
	}

	stopReason, _ := envelope["stopReason"].(string)
	if stopReason == "" {
		t.Fatalf("stopReason is empty or missing in:\n%s", out)
	}

	if !strings.Contains(stopReason, "memoryweb dream") {
		t.Errorf("stopReason should contain the dream header; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "WebGL Renderer Architecture Decision") {
		t.Errorf("stopReason should contain a recent node label; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "Old Canvas-Based Renderer") {
		t.Errorf("stopReason should surface drift candidate; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "remember with an items array") {
		t.Errorf("stopReason should contain 'remember with an items array' filing instruction; got:\n%s", stopReason)
	}
}

// TestPrecompactHookBlocksGracefullyWithoutDreamBin verifies that the precompact
// hook still fires correctly when the dream binary is absent.
func TestPrecompactHookBlocksGracefullyWithoutDreamBin(t *testing.T) {
	precompactHook := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	stateDir := t.TempDir()
	sessionID := "test-precompact-no-dream"

	home2 := t.TempDir()
	writeConfig(t, home2, map[string]interface{}{"pre_compact_enabled": true})

	out, code := runHookExtra(t, precompactHook, sessionID, stateDir, t.TempDir(),
		"MEMORYWEB_BIN=/nonexistent/memoryweb-dream",
		"HOME="+home2,
	)
	if code != 0 {
		t.Fatalf("hook exited %d without dream binary; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false even without dream binary; got:\n%s", out)
	}
	line := strings.TrimSpace(out)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON without dream binary: %v\ngot:\n%s", err, out)
	}
	stopReason, _ := envelope["stopReason"].(string)
	if !strings.Contains(stopReason, "remember with an items array") {
		t.Errorf("stopReason should still contain filing instructions; got:\n%s", stopReason)
	}
}

// ── option helper tests (hooks-options-cli story) ─────────────────────────────

// writeConfig writes a flat JSON config to $home/.memoryweb/config.json.
func writeConfig(t *testing.T, home string, data map[string]interface{}) {
	t.Helper()
	dir := filepath.Join(home, ".memoryweb")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("writeConfig mkdir: %v", err)
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0600); err != nil {
		t.Fatalf("writeConfig write: %v", err)
	}
}

// runLibTest executes a small bash snippet that sources memoryweb_lib.sh.
func runLibTest(t *testing.T, home, snippet string) string {
	t.Helper()
	lib := filepath.Join(hooksDir(t), "memoryweb_lib.sh")
	script := filepath.Join(t.TempDir(), "lib_test.sh")
	libArg := lib
	if runtime.GOOS == "windows" {
		libArg = winToMSYS2(lib)
	}
	content := fmt.Sprintf("#!/usr/bin/env bash\nsource %q\n%s\n", libArg, snippet)
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("runLibTest write: %v", err)
	}
	homeArg := home
	if runtime.GOOS == "windows" {
		homeArg = winToMSYS2(home)
	}
	cmd := shellCmd(script)
	cmd.Env = append(os.Environ(), "HOME="+homeArg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("runLibTest exec: %v", err)
		}
	}
	return string(out)
}

func TestLib_ReadOptionMissingKeyWithExistingConfig(t *testing.T) {
	home := t.TempDir()
	// Config exists but lacks the queried key. Previously the trailing
	// `[ -n "$_raw" ] && _opt=...` returned non-zero as the function's last
	// statement, aborting hooks that call it bare under set -e.
	writeConfig(t, home, map[string]interface{}{"unrelated_key": true})

	out := runLibTest(t, home,
		`set -euo pipefail; memoryweb_read_option "sweep_interval_turns" "15"; printf 'opt=%s rc=%s\n' "$_opt" "$?"`)
	if !strings.Contains(out, "opt=15 rc=0") {
		t.Fatalf("expected default preserved and rc 0 under set -e; got: %s", out)
	}
}

func TestLib_ReadOptionPresentKey(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	out := runLibTest(t, home,
		`memoryweb_read_option "session_orient_enabled" "false"; printf 'opt=%s\n' "$_opt"`)
	if !strings.Contains(out, "opt=true") {
		t.Fatalf("expected present key to read through; got: %s", out)
	}
}

func TestReadOption_FileAbsent(t *testing.T) {
	home := t.TempDir()
	out := runLibTest(t, home,
		`memoryweb_read_option "sweep_interval_turns" "15"; printf '%s\n' "${_opt}"`)
	if strings.TrimSpace(out) != "15" {
		t.Errorf("expected default '15' when file absent; got %q", out)
	}
}

func TestReadOption_FilePresent(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": false})
	out := runLibTest(t, home,
		`memoryweb_read_option "session_orient_enabled" "true"; printf '%s\n' "${_opt}"`)
	if strings.TrimSpace(out) != "false" {
		t.Errorf("expected 'false' from config; got %q", out)
	}
}

func TestSaveHook_SweepZeroDisabled(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"sweep_interval_turns": 0})
	out, code := runHookExtra(t, saveHook, "sweep-zero-session", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_SAVE_INTERVAL=",
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when sweep_interval_turns=0; got:\n%s", out)
	}
	if strings.Contains(out, "stopReason") {
		t.Errorf("expected no stopReason when disabled; got:\n%s", out)
	}
}

func TestPreCompactHook_OptionDisabled(t *testing.T) {
	precompactHook := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir() // no config → pre_compact_enabled defaults to false
	out, code := runHookExtra(t, precompactHook, "precompact-disabled-session", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when pre_compact_enabled=false; got:\n%s", out)
	}
	if strings.Contains(out, "stopReason") {
		t.Errorf("expected no stopReason when disabled; got:\n%s", out)
	}
}

// ── UserPromptSubmit hook helpers ─────────────────────────────────────────────

// makeOrientTranscript writes a JSONL transcript containing an orient tool call
// under projectsDir/test-project/<sessionID>.jsonl.
func makeOrientTranscript(t *testing.T, projectsDir, sessionID, domain string) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("makeOrientTranscript: mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(projectDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("makeOrientTranscript: create: %v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, `{"type":"assistant","content":{"name":"orient","arguments":{"domain":%q}}}`+"\n", domain)
}

// makeMCPOrientTranscript writes a JSONL transcript containing an orient tool
// call serialised the way Claude Code actually records MCP tools: the tool name
// on the assistant tool_use block carries an mcp__<server>__ prefix (e.g.
// mcp__memoryweb__orient). Fixtures must use this real shape, not the bare name.
func makeMCPOrientTranscript(t *testing.T, projectsDir, sessionID, domain string) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("makeMCPOrientTranscript: mkdir: %v", err)
	}
	line := fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_mcp01","name":"mcp__memoryweb__orient","input":{"domain":%q}}]}}`+"\n", domain)
	if err := os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), []byte(line), 0644); err != nil {
		t.Fatalf("makeMCPOrientTranscript: write: %v", err)
	}
}

// orientAssistantLine builds a Claude Code assistant tool_use transcript line
// for an orient MCP call. An empty domain yields an empty input — a
// cross-domain orient() call that carries no domain field at all.
func orientAssistantLine(id, domain string) string {
	if domain == "" {
		return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"%s","name":"mcp__memoryweb__orient","input":{}}]}}`, id)
	}
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"%s","name":"mcp__memoryweb__orient","input":{"domain":%q}}]}}`, id, domain)
}

// orientAssistantLineTopic builds an orient tool_use line carrying both a
// domain and a topic — the shape the UserPromptSubmit hook persists to the
// session context file for PostCompact and SubagentStart to reuse.
func orientAssistantLineTopic(id, domain, topic string) string {
	if topic == "" {
		return orientAssistantLine(id, domain)
	}
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"%s","name":"mcp__memoryweb__orient","input":{"domain":%q,"topic":%q}}]}}`, id, domain, topic)
}

// orientAttachmentLine builds an attachment record that mentions the orient
// tool name verbatim (so it matches the name regex) but carries no domain
// field — the real-session shape that made a naive tail -1 extraction land on
// a non-orient line.
func orientAttachmentLine() string {
	return `{"type":"attachment","attachment":{"kind":"text","content":[{"type":"text","text":"session excerpt"}],"included_calls":[{"name":"mcp__memoryweb__orient","status":"recorded"}]}}`
}

// appendTranscriptLines appends JSONL lines to the session transcript under
// projectsDir/test-project/<sessionID>.jsonl, creating it if absent.
func appendTranscriptLines(t *testing.T, projectsDir, sessionID string, lines ...string) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("appendTranscriptLines: mkdir: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(projectDir, sessionID+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("appendTranscriptLines: open: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("appendTranscriptLines: write: %v", err)
		}
	}
}

// runUPSHook runs the UserPromptSubmit hook with the given sessionID, message,
// stateDir, projectsDir, and extra env vars.
func runUPSHook(t *testing.T, sessionID, stateDir, projectsDir, message string, extraEnv ...string) (string, int) {
	t.Helper()
	script := filepath.Join(hooksDir(t), "memoryweb_userpromptsubmit_hook.sh")
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID, "message": message})
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(string(payload))
	env := append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
	)
	env = append(env, extraEnv...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec userpromptsubmit hook: %v", err)
		}
	}
	return string(out), code
}

// wantHookSpecificOutput asserts the hook output wraps additionalContext under
// hookSpecificOutput with the matching hookEventName — the shape Claude Code
// actually injects (bare top-level additionalContext is silently discarded).
func wantHookSpecificOutput(t *testing.T, out, eventName string) {
	t.Helper()
	if !strings.Contains(out, `"hookSpecificOutput"`) {
		t.Errorf("expected hookSpecificOutput wrapper; got:\n%s", out)
	}
	if !strings.Contains(out, `"hookEventName":"`+eventName+`"`) {
		t.Errorf("expected hookEventName %q; got:\n%s", eventName, out)
	}
}

// ── UserPromptSubmit hook tests ───────────────────────────────────────────────

func TestUserPromptSubmitHook_OrientNotCalled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir() // no transcript file → orient not found
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	out, code := runUPSHook(t, "ups-orient-not-called", stateDir, projectsDir, "what should we build next?",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext when orient not called; got:\n%s", out)
	}
	wantHookSpecificOutput(t, out, "UserPromptSubmit")
	if !strings.Contains(out, "orient") {
		t.Errorf("expected 'orient' in additionalContext; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_OrientAlreadyCalled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})
	makeOrientTranscript(t, projectsDir, "ups-orient-called", "deep-game")

	out, code := runUPSHook(t, "ups-orient-called", stateDir, projectsDir, "continue working",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when orient already called; got:\n%s", out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_WritesContextFile(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-ctx-file"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})
	makeOrientTranscript(t, projectsDir, sessionID, "deep-game")

	_, code := runUPSHook(t, sessionID, stateDir, projectsDir, "next task",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited non-zero: %d", code)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file to be written: %v", err)
	}
	if !strings.Contains(string(data), "deep-game") {
		t.Errorf("context file should contain domain 'deep-game'; got: %s", data)
	}
}

func TestUserPromptSubmitHook_OrientAlreadyCalled_MCPPrefixedName(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-orient-mcp"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})
	makeMCPOrientTranscript(t, projectsDir, sessionID, "deep-game")

	out, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue working",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no orient nudge when orient already called via mcp__memoryweb__orient; got:\n%s", out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true; got:\n%s", out)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file written from MCP-prefixed orient call: %v", err)
	}
	if !strings.Contains(string(data), "deep-game") {
		t.Errorf("context file should contain domain 'deep-game'; got: %s", data)
	}
}

func TestUserPromptSubmitHook_OrientDomain_IgnoresNonDomainTrailingRecords(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-orient-domain-tail"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	// Real-session order: cross-domain orient, orient(domain=deep-game), then an
	// attachment record that mentions the tool name but carries no domain. The
	// naive tail -1 over name-matching lines lands on the attachment and
	// produces an empty domain.
	appendTranscriptLines(t, projectsDir, sessionID,
		orientAssistantLine("toolu_cross", ""),
		orientAssistantLine("toolu_ctx", "deep-game"),
		orientAttachmentLine(),
	)

	out, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no orient nudge; got:\n%s", out)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file: %v", err)
	}
	if !strings.Contains(string(data), `"domain":"deep-game"`) {
		t.Errorf("ctx file should carry the last domain-carrying orient, not an empty domain; got: %s", data)
	}
	if strings.Contains(string(data), `"topic"`) {
		t.Errorf("ctx file should omit topic when the orient call carried none; got: %s", data)
	}
}

func TestUserPromptSubmitHook_OrientDomainTopic_WritesBoth(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-orient-topic"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	appendTranscriptLines(t, projectsDir, sessionID,
		orientAssistantLine("toolu_plain", "deep-game"),
		orientAssistantLineTopic("toolu_topic", "deep-game", "schema migration"),
	)

	out, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file: %v", err)
	}
	if !strings.Contains(string(data), `"domain":"deep-game"`) {
		t.Errorf("ctx file should carry the oriented domain; got: %s", data)
	}
	if !strings.Contains(string(data), `"topic":"schema migration"`) {
		t.Errorf("ctx file should carry the topic from the last domain-carrying orient; got: %s", data)
	}
}

func TestUserPromptSubmitHook_CapturesOrientScopeForConsumerHookOnly(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-quiet-capture"
	// session_orient_enabled off (no per-prompt nudge); a consumer hook that
	// relies on the ctx file (here PostCompact) is enabled instead.
	writeConfig(t, home, map[string]interface{}{"reinject_on_compact": true})

	appendTranscriptLines(t, projectsDir, sessionID,
		orientAssistantLineTopic("toolu_topic", "deep-game", "schema migration"),
	)

	out, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file even without session_orient_enabled: %v", err)
	}
	if !strings.Contains(string(data), `"domain":"deep-game"`) || !strings.Contains(string(data), `"topic":"schema migration"`) {
		t.Errorf("ctx file should carry domain+topic for the consumer hook; got: %s", data)
	}
}

func TestUserPromptSubmitHook_OrientDomain_TracksLatestDomain(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-orient-domain-update"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	appendTranscriptLines(t, projectsDir, sessionID, orientAssistantLine("toolu_a", "alpha"))
	if _, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	); code != 0 {
		t.Fatalf("hook exited %d after first orient", code)
	}

	// The session re-orients to a new domain; the ctx file must follow it so
	// PostCompact reinjects into the current domain, not the first one.
	appendTranscriptLines(t, projectsDir, sessionID, orientAssistantLine("toolu_b", "beta"))
	if _, code := runUPSHook(t, sessionID, stateDir, projectsDir, "continue",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	); code != 0 {
		t.Fatalf("hook exited %d after second orient", code)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file: %v", err)
	}
	if !strings.Contains(string(data), `"domain":"beta"`) {
		t.Errorf("ctx file should track the latest oriented domain; got: %s", data)
	}
}

func TestUserPromptSubmitHook_OrientOptionDisabled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir() // no config → session_orient_enabled defaults to false

	out, code := runUPSHook(t, "ups-orient-disabled", stateDir, projectsDir, "any message",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when option disabled; got:\n%s", out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when option disabled; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_NoSessionID(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	// Send a payload with no session_id field.
	script := filepath.Join(hooksDir(t), "memoryweb_userpromptsubmit_hook.sh")
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(`{"message":"hello"}`)
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	rawOut, _ := cmd.CombinedOutput()
	out := string(rawOut)

	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true with no session_id; got:\n%s", out)
	}
	// No ctx file should have been created.
	entries, _ := os.ReadDir(stateDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "mw_orient_ctx_") {
			t.Errorf("no ctx file should be created when session_id is missing; found: %s", e.Name())
		}
	}
}

func TestUserPromptSubmitHook_AutoRecallEnabled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "recall.db")

	// Seed the DB with a node that will match the search query.
	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{"auto_recall": true, "session_orient_enabled": false})

	out, code := runUPSHook(t, "ups-recall-enabled", stateDir, projectsDir,
		"WebGL renderer architecture decision",
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext with recall results; got:\n%s", out)
	}
	wantHookSpecificOutput(t, out, "UserPromptSubmit")
	if !strings.Contains(out, "memoryweb relevant memories") {
		t.Errorf("expected 'memoryweb relevant memories' in additionalContext; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_AutoRecallDisabled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir() // no config → auto_recall defaults to false

	out, code := runUPSHook(t, "ups-recall-disabled", stateDir, projectsDir, "WebGL renderer",
		"HOME="+home,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, "memoryweb relevant memories") {
		t.Errorf("expected no recall section when auto_recall=false; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_AutoRecallNoResults(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "empty.db")
	writeConfig(t, home, map[string]interface{}{"auto_recall": true, "session_orient_enabled": false})

	// Empty DB — search will return no results.
	out, code := runUPSHook(t, "ups-recall-no-results", stateDir, projectsDir, "some query that matches nothing",
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, "memoryweb relevant memories") {
		t.Errorf("expected no recall section when search returns nothing; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_BothNudgeAndRecall(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir() // no transcript → orient not found
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "both.db")

	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{
		"session_orient_enabled": true,
		"auto_recall":            true,
	})

	out, code := runUPSHook(t, "ups-both", stateDir, projectsDir,
		"WebGL renderer architecture decision",
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext; got:\n%s", out)
	}
	wantHookSpecificOutput(t, out, "UserPromptSubmit")
	if !strings.Contains(out, "orient") {
		t.Errorf("expected orient nudge in additionalContext; got:\n%s", out)
	}
	if !strings.Contains(out, "memoryweb relevant memories") {
		t.Errorf("expected recall section in additionalContext; got:\n%s", out)
	}
}

func TestUserPromptSubmitHook_ContextFileEscapesDomainQuote(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-ctx-domain-quote"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})
	// A quote in the domain is escaped in the transcript (\"). Naive grep
	// extraction truncates the value at the escape, and the unescaped ctx-file
	// write then produces invalid JSON; the ctx file must stay parseable.
	makeMCPOrientTranscript(t, projectsDir, sessionID, `frob"bar`)

	_, code := runUPSHook(t, sessionID, stateDir, projectsDir, "next task",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited non-zero: %d", code)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file written: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("ctx file must be valid JSON when the domain contains a quote; got %s: %v", data, err)
	}
	if m["domain"] == "" {
		t.Errorf("expected a domain value in ctx file; got: %s", data)
	}
}

func TestUserPromptSubmitHook_ContextFileEscapesTopicQuote(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "ups-ctx-topic-quote"
	writeConfig(t, home, map[string]interface{}{"session_orient_enabled": true})

	appendTranscriptLines(t, projectsDir, sessionID,
		orientAssistantLineTopic("toolu_topic_quote", "deep-game", `a"b`),
	)

	_, code := runUPSHook(t, sessionID, stateDir, projectsDir, "next task",
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited non-zero: %d", code)
	}

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	data, err := os.ReadFile(ctxFile)
	if err != nil {
		t.Fatalf("expected context file written: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("ctx file must be valid JSON when the topic contains a quote; got %s: %v", data, err)
	}
	if m["domain"] == "" {
		t.Errorf("expected a domain value in ctx file; got: %s", data)
	}
}

func TestUserPromptSubmitHook_AutoRecallMessageLeadingQuote(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "recall_quote.db")

	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{
		"session_orient_enabled": true,
		"auto_recall":            true,
	})

	// The message begins with a quote; the payload keeps it escaped as \" in
	// the JSON. The naive regex truncates the message at that escape, so the
	// auto-recall query degrades and no memories are injected.
	out, code := runUPSHook(t, "ups-recall-quote", stateDir, projectsDir,
		`"WebGL renderer architecture decision"`,
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "memoryweb relevant memories") {
		t.Errorf("expected recall section despite leading quote in message; got:\n%s", out)
	}
}

// ── SubagentStart hook tests ──────────────────────────────────────────────────

func TestSubagentStartHook_NoMemoryweb(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	// Enable option so we exercise the binary check, not the early return.
	writeConfig(t, home, map[string]interface{}{"subagent_orient_enabled": true})

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"),
		"subagent-no-bin", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true; got:\n%s", out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when binary missing; got:\n%s", out)
	}
}

func TestSubagentStartHook_WithDreamDigest(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "subagent.db")

	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{"subagent_orient_enabled": true})

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"),
		"subagent-with-dream", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext with dream digest; got:\n%s", out)
	}
	wantHookSpecificOutput(t, out, "SubagentStart")
	if !strings.Contains(out, "memoryweb") {
		t.Errorf("expected 'memoryweb' in additionalContext; got:\n%s", out)
	}
}

func TestSubagentStartHook_OrientHintFromParentSession(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "subagent.db")

	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{"subagent_orient_enabled": true})

	// The parent session oriented in the transcript; its UPS hook left a ctx
	// file keyed by the parent session id. The SubagentStart payload carries
	// the parent's transcript_path, whose basename is that id.
	parentSessionID := "parent-orient-ctx"
	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+parentSessionID+".json")
	if err := os.WriteFile(ctxFile, []byte(`{"domain":"deep-game","topic":"schema migration"}`), 0644); err != nil {
		t.Fatalf("write ctx file: %v", err)
	}
	parentTranscript := filepath.Join(projectsDir, "test-project", parentSessionID+".jsonl")

	out, code := runSubagentStartHook(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"),
		"subagent-session-1", parentTranscript, stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	wantHookSpecificOutput(t, out, "SubagentStart")
	// The subagent must be told to orient into the parent's domain+topic.
	if !strings.Contains(out, `orient(domain=\"deep-game\", topic=\"schema migration\")`) {
		t.Errorf("expected parent domain+topic orient hint; got:\n%s", out)
	}
	// The dream digest is retained alongside the hint.
	if !strings.Contains(out, "memoryweb context for this sub-agent session") {
		t.Errorf("expected dream digest alongside the orient hint; got:\n%s", out)
	}
}

func TestSubagentStartHook_HonoursParentSessionIdField(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"subagent_orient_enabled": true})

	// Future-proofing: if Claude Code starts sending parent_session_id, prefer
	// it over transcript_path basename.
	parentSessionID := "parent-session-field"
	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+parentSessionID+".json")
	if err := os.WriteFile(ctxFile, []byte(`{"domain":"deep-game"}`), 0644); err != nil {
		t.Fatalf("write ctx file: %v", err)
	}
	// transcript_path points at an unrelated session id on purpose.
	unrelatedTranscript := filepath.Join(projectsDir, "test-project", "some-other-session"+".jsonl")

	payload := fmt.Sprintf(`{"session_id":"subagent-session-2","parent_session_id":%q,"transcript_path":%q}`, parentSessionID, unrelatedTranscript)
	cmd := shellCmd(filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"))
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exec SubagentStart hook: %v", err)
	}
	if !strings.Contains(string(out), `orient(domain=\"deep-game\")`) {
		t.Errorf("expected orient hint from parent_session_id; got:\n%s", out)
	}
}

func TestSubagentStartHook_NoParentCtx_DigestOnly(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "subagent.db")

	seedRealisticDB(t, dbPath)
	writeConfig(t, home, map[string]interface{}{"subagent_orient_enabled": true})

	// transcript_path basename points at a parent id with no ctx file (e.g.
	// the parent never oriented). Behaviour must fall back to digest-only.
	parentTranscript := filepath.Join(projectsDir, "test-project", "parent-no-orient"+".jsonl")

	out, code := runSubagentStartHook(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"),
		"subagent-session-3", parentTranscript, stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_DB="+dbPath,
		"MEMORYWEB_BIN="+dreamBin,
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	wantHookSpecificOutput(t, out, "SubagentStart")
	if !strings.Contains(out, "memoryweb context for this sub-agent session") {
		t.Errorf("expected dream digest fallback; got:\n%s", out)
	}
	if strings.Contains(out, `orient(domain=`) {
		t.Errorf("expected no orient hint when the parent never oriented; got:\n%s", out)
	}
}

func TestSubagentStartHook_OptionDisabledWithParentCtx(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir() // no config → subagent_orient_enabled defaults to false

	parentSessionID := "parent-disabled"
	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+parentSessionID+".json")
	if err := os.WriteFile(ctxFile, []byte(`{"domain":"deep-game"}`), 0644); err != nil {
		t.Fatalf("write ctx file: %v", err)
	}
	parentTranscript := filepath.Join(projectsDir, "test-project", parentSessionID+".jsonl")

	out, code := runSubagentStartHook(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_start_hook.sh"),
		"subagent-session-4", parentTranscript, stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when option disabled; got:\n%s", out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when option disabled; got:\n%s", out)
	}
}

// ── SubagentStop hook tests ───────────────────────────────────────────────────

func TestSubagentStopHook_FirstFire(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "subagent-stop-first"
	writeConfig(t, home, map[string]interface{}{"subagent_audit_enabled": true})

	out, _ := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_stop_hook.sh"),
		sessionID, stateDir, projectsDir,
		"HOME="+home,
	)
	if !strings.Contains(out, `"decision":"block"`) {
		t.Errorf("expected decision:block on first fire; got:\n%s", out)
	}
	if strings.Contains(out, `"continue":false`) {
		t.Errorf("should not contain continue:false; got:\n%s", out)
	}
	if strings.Contains(out, `"stopReason"`) {
		t.Errorf("should not contain stopReason; got:\n%s", out)
	}
	if !strings.Contains(out, "audit") {
		t.Errorf("expected 'audit' in reason; got:\n%s", out)
	}
	if !strings.Contains(out, "orphans") {
		t.Errorf("expected 'orphans' in reason; got:\n%s", out)
	}
	flagFile := filepath.Join(stateDir, sessionID+".subagent_stop")
	if _, err := os.Stat(flagFile); err != nil {
		t.Errorf("flag file should have been created: %v", err)
	}
}

func TestSubagentStopHook_SecondFire(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "subagent-stop-second"
	writeConfig(t, home, map[string]interface{}{"subagent_audit_enabled": true})

	// First fire to create the flag.
	runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_stop_hook.sh"),
		sessionID, stateDir, projectsDir,
		"HOME="+home,
	)

	// Second fire: flag exists → should clear it and return continue:true.
	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_subagent_stop_hook.sh"),
		sessionID, stateDir, projectsDir,
		"HOME="+home,
	)
	if code != 0 {
		t.Fatalf("hook exited %d on second fire; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true on second fire; got:\n%s", out)
	}
	flagFile := filepath.Join(stateDir, sessionID+".subagent_stop")
	if _, err := os.Stat(flagFile); err == nil {
		t.Error("flag file should have been deleted on second fire")
	}
}

func TestSubagentStopHook_NoSessionID(t *testing.T) {
	stateDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"subagent_audit_enabled": true})

	script := filepath.Join(hooksDir(t), "memoryweb_subagent_stop_hook.sh")
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(`{"no_session":"here"}`)
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"HOME="+home,
	)
	rawOut, _ := cmd.CombinedOutput()
	out := string(rawOut)

	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when session_id missing; got:\n%s", out)
	}
	// No flag file should be created.
	entries, _ := os.ReadDir(stateDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".subagent_stop") {
			t.Errorf("no flag file should be created without session_id; found: %s", e.Name())
		}
	}
}

// ── PostCompact hook tests ────────────────────────────────────────────────────

func TestPostCompactHook_WithContextFile(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "postcompact-with-ctx"
	writeConfig(t, home, map[string]interface{}{"reinject_on_compact": true})

	// Write a context file as the UserPromptSubmit hook would.
	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	if err := os.WriteFile(ctxFile, []byte(`{"domain":"deep-game"}`), 0644); err != nil {
		t.Fatalf("write ctx file: %v", err)
	}

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_postcompact_hook.sh"),
		sessionID, stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext; got:\n%s", out)
	}
	wantHookSpecificOutput(t, out, "PostCompact")
	if !strings.Contains(out, "deep-game") {
		t.Errorf("expected domain 'deep-game' in additionalContext; got:\n%s", out)
	}
	if !strings.Contains(out, "orient") {
		t.Errorf("expected 'orient' in additionalContext; got:\n%s", out)
	}
}

func TestPostCompactHook_NoContextFile(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"reinject_on_compact": true})

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_postcompact_hook.sh"),
		"postcompact-no-ctx", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected additionalContext even without ctx file; got:\n%s", out)
	}
	// Without a ctx file, orient hint should be generic orient().
	if !strings.Contains(out, "orient()") {
		t.Errorf("expected generic 'orient()' hint in additionalContext; got:\n%s", out)
	}
}

func TestPostCompactHook_TopicHint(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir()
	sessionID := "postcompact-topic"
	writeConfig(t, home, map[string]interface{}{"reinject_on_compact": true})

	ctxFile := filepath.Join(stateDir, "mw_orient_ctx_"+sessionID+".json")
	if err := os.WriteFile(ctxFile, []byte(`{"domain":"deep-game","topic":"schema migration"}`), 0644); err != nil {
		t.Fatalf("write ctx file: %v", err)
	}

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_postcompact_hook.sh"),
		sessionID, stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	wantHookSpecificOutput(t, out, "PostCompact")
	// The hint must carry the topic, not stop at domain.
	if !strings.Contains(out, `orient(domain=\"deep-game\", topic=\"schema migration\")`) {
		t.Errorf("expected orient(domain, topic) hint; got:\n%s", out)
	}
}

func TestPostCompactHook_OptionDisabled(t *testing.T) {
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	home := t.TempDir() // no config → reinject_on_compact defaults to false

	out, code := runHookExtra(t,
		filepath.Join(hooksDir(t), "memoryweb_postcompact_hook.sh"),
		"postcompact-disabled", stateDir, projectsDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when option disabled; got:\n%s", out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when option disabled; got:\n%s", out)
	}
}

func TestPostCompactHook_NoSessionID(t *testing.T) {
	stateDir := t.TempDir()
	home := t.TempDir()
	writeConfig(t, home, map[string]interface{}{"reinject_on_compact": true})

	script := filepath.Join(hooksDir(t), "memoryweb_postcompact_hook.sh")
	cmd := shellCmd(script)
	cmd.Stdin = strings.NewReader(`{"no_session":"here"}`)
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"HOME="+home,
		"MEMORYWEB_BIN=/nonexistent/memoryweb-test",
	)
	rawOut, _ := cmd.CombinedOutput()
	out := string(rawOut)

	if !strings.Contains(out, `"continue":true`) {
		t.Errorf("expected continue:true when session_id missing; got:\n%s", out)
	}
	if strings.Contains(out, `"additionalContext"`) {
		t.Errorf("expected no additionalContext when session_id missing; got:\n%s", out)
	}
}
