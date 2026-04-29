package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-hooks-%d", os.Getpid()))
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

// makeTranscript creates a JSONL transcript with numHuman human messages under
// projectsDir/<project>/<sessionID>.jsonl.
func makeTranscript(t *testing.T, projectsDir, sessionID string, numHuman int) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("makeTranscript: mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(projectDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("makeTranscript: create: %v", err)
	}
	defer f.Close()
	for i := 0; i < numHuman; i++ {
		fmt.Fprintf(f, `{"role":"human","content":"message %d"}`+"\n", i+1)
		fmt.Fprintf(f, `{"role":"assistant","content":"reply %d"}`+"\n", i+1)
	}
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
	cmd := exec.Command(script)
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
	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false for 15 messages at threshold; got:\n%s", out)
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
	script := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	cmd := exec.Command(script)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
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

	h := tools.New(store)

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
		"from_node":    cssID,
		"to_node":      webglID,
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
// node labels and drift candidates — in the stopReason so Claude can act on it.
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
	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false at threshold; got:\n%s", out)
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

	stopReason, _ := envelope["stopReason"].(string)
	if stopReason == "" {
		t.Fatalf("stopReason is empty or missing in:\n%s", out)
	}

	// ── dream header ──────────────────────────────────────────────────────────

	if !strings.Contains(stopReason, "memoryweb dream") {
		t.Errorf("stopReason should contain the dream header 'memoryweb dream'; got:\n%s", stopReason)
	}

	// ── recent nodes ──────────────────────────────────────────────────────────
	// At least two recently filed node labels must appear so Claude knows what
	// context already exists.

	if !strings.Contains(stopReason, "WebGL Renderer Architecture Decision") {
		t.Errorf("stopReason should contain recent node 'WebGL Renderer Architecture Decision'; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "Dream Tool for Session Orientation") {
		t.Errorf("stopReason should contain recent node 'Dream Tool for Session Orientation'; got:\n%s", stopReason)
	}

	// ── drift candidates ──────────────────────────────────────────────────────
	// The superseded-label node and the contradicting pair must surface so Claude
	// knows which nodes need organising.

	if !strings.Contains(stopReason, "Old Canvas-Based Renderer") {
		t.Errorf("stopReason should surface drift candidate 'Old Canvas-Based Renderer'; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "CSS Transform Animation Approach") {
		t.Errorf("stopReason should surface the contradicting node 'CSS Transform Animation Approach'; got:\n%s", stopReason)
	}

	// ── actionable guidance ───────────────────────────────────────────────────
	// The filing instructions must follow the digest so Claude knows what to do.

	if !strings.Contains(stopReason, "remember_all") {
		t.Errorf("stopReason should contain 'remember_all' filing instruction; got:\n%s", stopReason)
	}
	if !strings.Contains(stopReason, "why_matters") {
		t.Errorf("stopReason should contain 'why_matters' guidance; got:\n%s", stopReason)
	}
}

// TestSaveHookBlocksGracefullyWithoutDreamBin verifies that the hook still
// blocks correctly and produces a valid stopReason when the dream binary is
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
	if !strings.Contains(out, `"continue":false`) {
		t.Errorf("expected continue:false even without dream binary; got:\n%s", out)
	}
	line := strings.TrimSpace(out)
	var envelope map[string]any
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Errorf("hook output is not valid JSON without dream binary: %v\ngot:\n%s", err, out)
	}
	stopReason, _ := envelope["stopReason"].(string)
	if !strings.Contains(stopReason, "remember_all") {
		t.Errorf("stopReason should still contain filing instructions; got:\n%s", stopReason)
	}
}
