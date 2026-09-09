package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

func newTestStore(t *testing.T) (*db.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := db.New(path)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}

func TestPrintLiveRemaining_SilentWithoutDomain(t *testing.T) {
	out := captureStdout(t, func() {
		printLiveRemaining(db.PurgeResult{LiveRemaining: 3}, "", false)
	})
	if out != "" {
		t.Errorf("expected no output when domain is unscoped, got %q", out)
	}
}

func TestPrintLiveRemaining_SilentWhenIncludeLiveUsed(t *testing.T) {
	out := captureStdout(t, func() {
		printLiveRemaining(db.PurgeResult{LiveRemaining: 0}, "recordari-commercial", true)
	})
	if out != "" {
		t.Errorf("expected no output when includeLive already swept up live nodes, got %q", out)
	}
}

func TestPrintLiveRemaining_SilentWhenNoneRemain(t *testing.T) {
	out := captureStdout(t, func() {
		printLiveRemaining(db.PurgeResult{LiveRemaining: 0}, "recordari-commercial", false)
	})
	if out != "" {
		t.Errorf("expected no output when LiveRemaining is 0, got %q", out)
	}
}

func TestPrintLiveRemaining_WarnsWhenLiveNodesRemain(t *testing.T) {
	out := captureStdout(t, func() {
		printLiveRemaining(db.PurgeResult{LiveRemaining: 2}, "recordari-commercial", false)
	})
	if !strings.Contains(out, "2 live node(s)") || !strings.Contains(out, "recordari-commercial") {
		t.Errorf("expected a note naming the domain and live count, got %q", out)
	}
	if !strings.Contains(out, "--include-live") {
		t.Errorf("expected the note to mention --include-live as the escape hatch, got %q", out)
	}
}

func TestDrawProgressBar_Format(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 5, 10)
	got := buf.String()

	if !strings.HasPrefix(got, "\r") {
		t.Errorf("progress bar should start with \\r; got %q", got)
	}
	if !strings.Contains(got, "5/10") {
		t.Errorf("progress bar should contain '5/10'; got %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("progress bar should contain '50%%'; got %q", got)
	}
	if !strings.Contains(got, "[") || !strings.Contains(got, "]") {
		t.Errorf("progress bar should contain '[' and ']'; got %q", got)
	}
}

func TestDrawProgressBar_Complete(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 10, 10)
	got := buf.String()

	if !strings.Contains(got, "10/10") {
		t.Errorf("complete bar should show '10/10'; got %q", got)
	}
	if !strings.Contains(got, "100%") {
		t.Errorf("complete bar should show '100%%'; got %q", got)
	}
	// At 100% the bar should be all '=' with no '>'
	if strings.Contains(got, ">") {
		t.Errorf("complete bar should not contain '>'; got %q", got)
	}
}

func TestDrawProgressBar_First(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 1, 100)
	got := buf.String()

	if !strings.Contains(got, "1/100") {
		t.Errorf("first step should show '1/100'; got %q", got)
	}
	// Should contain the '>' cursor marker
	if !strings.Contains(got, ">") {
		t.Errorf("in-progress bar should contain '>'; got %q", got)
	}
}

// CR-14: drawProgressBar with total=0 must not divide by zero (NaN %).
func TestDrawProgressBar_ZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	drawProgressBar(&buf, 0, 0)
	got := buf.String()

	if strings.Contains(got, "NaN") {
		t.Errorf("zero-total bar must not emit NaN; got %q", got)
	}
	if !strings.Contains(got, "0/0") {
		t.Errorf("zero-total bar should show '0/0'; got %q", got)
	}
	if !strings.Contains(got, "0%") {
		t.Errorf("zero-total bar should show 0%%; got %q", got)
	}
}

// CR-13: waitForOllamaReady must report ready when the endpoint answers.
func TestWaitForOllamaReady_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	}))
	defer srv.Close()

	if !waitForOllamaReady(srv.Client(), srv.URL, time.Now().Add(5*time.Second)) {
		t.Error("expected ready=true for a reachable endpoint")
	}
}

// CR-13: waitForOllamaReady must report not-ready without hanging when the
// endpoint never answers and the deadline has already passed.
func TestWaitForOllamaReady_UnreachableExpiredDeadline(t *testing.T) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(-time.Second)

	start := time.Now()
	ready := waitForOllamaReady(client, "http://127.0.0.1:1/api/tags", deadline)
	if ready {
		t.Error("expected ready=false for an unreachable endpoint")
	}
	if since := time.Since(start); since > 3*time.Second {
		t.Errorf("expired deadline must return immediately; took %v", since)
	}
}

// ── runDoctor tests ───────────────────────────────────────────────────────────

func TestRunDoctor_TextOutput_ContainsSections(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir() // no hooks configured

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	for _, want := range []string{
		"Database:", "sqlite-vec:", "Ollama binary:", "Ollama server:", "Ollama model:",
		"Claude hooks:", "Graph:", "Drift:", "Last activity:", "Update:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected section %q in output; got:\n%s", want, out)
		}
	}
}

func TestRunDoctor_DatabaseCheck_PassesOnFreshDB(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	// The fresh DB should report a passing database check.
	if !strings.Contains(out, "[✓] Database:") {
		t.Errorf("fresh DB should produce [✓] Database; got:\n%s", out)
	}
	// The DB path should appear in the output.
	if !strings.Contains(out, dbPath) {
		t.Errorf("DB path should appear in output; got:\n%s", out)
	}
}

func TestRunDoctor_HooksCheck_FailsWhenNoSettings(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir() // no .claude/settings.json

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "[✗] Claude hooks:") {
		t.Errorf("missing hooks should produce [✗] Claude hooks; got:\n%s", out)
	}
}

func TestRunDoctor_ReturnsFalse_WhenHooksMissing(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir() // no hooks → "fail"

	var buf bytes.Buffer
	passed := runDoctor(store, &buf, dbPath, home, false)

	// Hooks fail means overall result is false.
	if passed {
		t.Errorf("runDoctor should return false when hooks are missing; output:\n%s", buf.String())
	}
}

func TestRunDoctor_JSONOutput_ValidSchema(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, true)

	var report DoctorReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("JSON output is not valid: %v\nraw: %s", err, buf.String())
	}
	if len(report.Checks) == 0 {
		t.Error("JSON report should contain at least one check")
	}
	for _, c := range report.Checks {
		if c.Name == "" {
			t.Error("every check should have a non-empty Name")
		}
		switch c.Status {
		case "ok", "fail", "warn", "info":
			// valid
		default:
			t.Errorf("unexpected status %q in check %q", c.Status, c.Name)
		}
	}
}

func TestRunDoctor_GraphStats_ReflectAddedNode(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	// Add a node so the graph stats show something meaningful.
	if _, err := store.AddNode("test node", "desc", "why", "test-domain", nil, "", ""); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "1 live") {
		t.Errorf("expected '1 live' in graph stats; got:\n%s", out)
	}
	if !strings.Contains(out, "test-domain") {
		t.Errorf("expected domain 'test-domain' in graph stats; got:\n%s", out)
	}
}

func TestRunDoctor_DriftSnapshot_ShowsCandidate(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	// Add a node whose label contains "deprecated" so drift detects it.
	if _, err := store.AddNode("deprecated old feature", "d", "w", "test-domain", nil, "", ""); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "1 candidate") {
		t.Errorf("expected '1 candidate' in drift line; got:\n%s", out)
	}
}

func TestRunDoctor_DriftSnapshot_CategorisesResolvedPlaceholder(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	placeholder, err := store.AddNode("Story needed: api/openapi/admin.yaml", "", "", "test-domain", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode placeholder: %v", err)
	}
	done, err := store.AddNode("STORY-139 complete", "shipped 2026-06-28", "", "test-domain", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode done: %v", err)
	}
	if _, err := store.AddEdge(placeholder.ID, done.ID, "connects_to", "closes this placeholder"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "resolved placeholders") {
		t.Errorf("expected the resolved-placeholder drift candidate to be categorised as 'resolved placeholders', not folded into 'transient'; got:\n%s", out)
	}
}

func TestRunDoctor_AuditLog_ShowsLastActivity(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	n, err := store.AddNode("audit target", "d", "w", "proj", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if err := store.ArchiveNode(n.ID, "test"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if strings.Contains(out, "(no activity recorded)") {
		t.Errorf("expected last activity to be populated; got:\n%s", out)
	}
	if !strings.Contains(out, "archive") {
		t.Errorf("expected 'archive' in last activity; got:\n%s", out)
	}
}

func TestRunDoctor_UpdateCheck_DevBuild(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	// Version is "dev" in tests (the zero value of the package variable),
	// so the update check should report the dev-build info message.
	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "dev build") {
		t.Errorf("expected 'dev build' in update check line; got:\n%s", out)
	}
}

// ── setupUpsertCommand unit tests (setup-idempotency story) ──────────────────

func makeTestEntry(cmd string) map[string]interface{} {
	return map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": cmd,
			},
		},
	}
}

func TestSetupUpsertCommand_AppendsWhenEmpty(t *testing.T) {
	cmd := "/hooks/memoryweb_save_hook.sh"
	result := setupUpsertCommand(nil, cmd, makeTestEntry(cmd))
	if len(result) != 1 {
		t.Fatalf("want 1 entry, got %d", len(result))
	}
}

func TestSetupUpsertCommand_IdempotentSamePath(t *testing.T) {
	cmd := "/hooks/memoryweb_save_hook.sh"
	entry := makeTestEntry(cmd)
	first := setupUpsertCommand(nil, cmd, entry)
	second := setupUpsertCommand(first, cmd, makeTestEntry(cmd))
	if len(second) != 1 {
		t.Fatalf("want 1 entry after second call, got %d", len(second))
	}
}

func TestSetupUpsertCommand_ReplacesOnPathChange(t *testing.T) {
	oldCmd := "/old/path/memoryweb_save_hook.sh"
	newCmd := "/new/path/memoryweb_save_hook.sh"
	entries := setupUpsertCommand(nil, oldCmd, makeTestEntry(oldCmd))
	result := setupUpsertCommand(entries, newCmd, makeTestEntry(newCmd))
	if len(result) != 1 {
		t.Fatalf("want 1 entry after path change, got %d", len(result))
	}
	entry, _ := result[0].(map[string]interface{})
	hs, _ := entry["hooks"].([]interface{})
	h, _ := hs[0].(map[string]interface{})
	if h["command"] != newCmd {
		t.Errorf("want command %q, got %q", newCmd, h["command"])
	}
}

func TestSetupUpsertCommand_DoesNotTouchOtherEntries(t *testing.T) {
	cmd1 := "/hooks/memoryweb_save_hook.sh"
	cmd2 := "/hooks/memoryweb_precompact_hook.sh"
	entries := setupUpsertCommand(nil, cmd1, makeTestEntry(cmd1))
	entries = append(entries, makeTestEntry(cmd2))
	result := setupUpsertCommand(entries, cmd1, makeTestEntry(cmd1))
	if len(result) != 2 {
		t.Fatalf("want 2 entries, got %d", len(result))
	}
}

// makeTestEntryWithEnv returns a hook entry carrying the given MEMORYWEB_DB env.
func makeTestEntryWithEnv(cmd, db string) map[string]interface{} {
	return map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": cmd,
				"env":     map[string]interface{}{"MEMORYWEB_DB": db},
			},
		},
	}
}

// testEntryEnv extracts the MEMORYWEB_DB env value from a hook entry.
func testEntryEnv(t *testing.T, e interface{}) string {
	t.Helper()
	entry, ok := e.(map[string]interface{})
	if !ok {
		t.Fatalf("entry is %T, want map", e)
	}
	hs, ok := entry["hooks"].([]interface{})
	if !ok || len(hs) == 0 {
		t.Fatalf("hooks array missing: %v", entry)
	}
	h, ok := hs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("hook is %T, want map", hs[0])
	}
	env, _ := h["env"].(map[string]interface{})
	db, _ := env["MEMORYWEB_DB"].(string)
	return db
}

// TestSetupUpsertCommand_RefreshesEnvOnSamePath: re-running setup with the same
// hook command path but a new DB path must update the existing entry's env —
// otherwise a stale relative --db survives forever in settings.json.
func TestSetupUpsertCommand_RefreshesEnvOnSamePath(t *testing.T) {
	cmd := "/hooks/memoryweb_save_hook.sh"
	staleDB := "./.memoryweb.db"
	absDB := "/Users/x/.memoryweb/.memoryweb.db"

	first := setupUpsertCommand(nil, cmd, makeTestEntryWithEnv(cmd, staleDB))
	result := setupUpsertCommand(first, cmd, makeTestEntryWithEnv(cmd, absDB))
	if len(result) != 1 {
		t.Fatalf("want 1 entry, got %d", len(result))
	}
	if got := testEntryEnv(t, result[0]); got != absDB {
		t.Errorf("env MEMORYWEB_DB = %q, want refreshed %q", got, absDB)
	}
}

// TestSetupUpsertCommand_PreservesExtraFieldsOnRefresh: refreshing the env on
// a same-path entry must not strip user-added keys (e.g. timeout, matcher).
func TestSetupUpsertCommand_PreservesExtraFieldsOnRefresh(t *testing.T) {
	cmd := "/hooks/memoryweb_save_hook.sh"
	customized := makeTestEntryWithEnv(cmd, "./.memoryweb.db")
	customized["timeout"] = float64(40)
	customized["stops"] = []interface{}{"idle"}

	first := setupUpsertCommand(nil, cmd, customized)
	result := setupUpsertCommand(first, cmd, makeTestEntryWithEnv(cmd, "/abs/x.db"))
	if len(result) != 1 {
		t.Fatalf("want 1 entry, got %d", len(result))
	}
	entry := result[0].(map[string]interface{})
	if _, ok := entry["timeout"]; !ok {
		t.Errorf("user-added timeout key was stripped: %v", entry)
	}
	if _, ok := entry["stops"]; !ok {
		t.Errorf("user-added stops key was stripped: %v", entry)
	}
	if got := testEntryEnv(t, result[0]); got != "/abs/x.db" {
		t.Errorf("env MEMORYWEB_DB = %q, want refreshed %q", got, "/abs/x.db")
	}
}

// TestSetupUpsertCommand_DedupesOldSameNameEntries: entries from previous
// installs at different paths but the same hook basename must be replaced, not
// left alongside the new entry — otherwise old Stop/PreCompact hooks keep firing.
func TestSetupUpsertCommand_DedupesOldSameNameEntries(t *testing.T) {
	oldA := "/old/install/hooks/memoryweb_save_hook.sh"
	oldB := "/older/install/hooks/memoryweb_save_hook.sh"
	newCmd := "/hooks/memoryweb_save_hook.sh"
	precompact := "/hooks/memoryweb_precompact_hook.sh"

	entries := []interface{}{
		makeTestEntry(oldA),
		makeTestEntry(oldB),
		makeTestEntry(precompact),
	}
	result := setupUpsertCommand(entries, newCmd, makeTestEntry(newCmd))
	if len(result) != 2 {
		t.Fatalf("want 2 entries (fresh save + untouched precompact), got %d", len(result))
	}
	count := 0
	var got string
	for _, e := range result {
		if _, ok := e.(map[string]interface{}); !ok {
			continue
		}
		hs := e.(map[string]interface{})["hooks"].([]interface{})
		if len(hs) == 0 {
			continue
		}
		h := hs[0].(map[string]interface{})
		existing, _ := h["command"].(string)
		if filepath.Base(existing) == "memoryweb_save_hook.sh" {
			count++
			got = existing
		}
	}
	if count != 1 || got != newCmd {
		t.Errorf("want exactly one new save entry %q, got count=%d command=%q", newCmd, count, got)
	}
}

func TestRunSetup_IdempotentHookEntries(t *testing.T) {
	home := t.TempDir()
	hooksDir := t.TempDir()
	// On Windows the executable-bit check is skipped, so empty files are fine.
	for _, name := range []string{
		"memoryweb_save_hook.sh",
		"memoryweb_precompact_hook.sh",
		"memoryweb_userpromptsubmit_hook.sh",
		"memoryweb_subagent_start_hook.sh",
		"memoryweb_subagent_stop_hook.sh",
		"memoryweb_postcompact_hook.sh",
	} {
		p := filepath.Join(hooksDir, name)
		if err := os.WriteFile(p, []byte("#!/usr/bin/env bash\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Redirect APPDATA so detectDesktopAgents finds nothing on Windows.
	t.Setenv("APPDATA", home)

	// Run setup twice; answer every interactive prompt with "n".
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		if err := runSetup(&buf, strings.NewReader("n\nn\nn\nn\n"), false, "", hooksDir, home); err != nil {
			t.Fatalf("runSetup run %d: %v", i+1, err)
		}
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	for _, hookName := range []string{"Stop", "PreCompact", "UserPromptSubmit", "SubagentStart", "SubagentStop", "PostCompact"} {
		entries := setupToSlice(hooks[hookName])
		if len(entries) != 1 {
			t.Errorf("%s: want 1 entry after two setup runs, got %d", hookName, len(entries))
		}
	}
}

// ── search subcommand tests (hooks-userpromptsubmit story) ───────────────────

func TestSearchCmd_LeanOutput(t *testing.T) {
	store, dbPath := newTestStore(t)
	if _, err := store.AddNode("WebGL Renderer Architecture", "desc", "Sets the rendering budget and browser support matrix.", "deep-game", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddNode("CSS Animation Approach", "desc2", "Alternative CSS-based card flip; rejected due to performance on low-end Android.", "deep-game", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	store.Close()

	var buf bytes.Buffer
	if err := runSearchCmd(&buf, dbPath, "WebGL Renderer", "", 10, true, false); err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 line; got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "[") {
		t.Errorf("lean line should start with '['; got: %q", lines[0])
	}
	if !strings.Contains(out, "WebGL Renderer Architecture") {
		t.Errorf("expected node label in output; got: %q", out)
	}
	if !strings.Contains(out, "deep-game") {
		t.Errorf("expected domain in output; got: %q", out)
	}
}

func TestSearchCmd_ExactMode(t *testing.T) {
	store, dbPath := newTestStore(t)
	if _, err := store.AddNode("STORY-123 done: exact label match", "desc", "An exact identifier lookup.", "deep-game", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddNode("CSS Animation Approach", "desc2", "Unrelated node that should not match.", "deep-game", nil, "", "decision"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	store.Close()

	var buf bytes.Buffer
	if err := runSearchCmd(&buf, dbPath, "STORY-123", "", 10, true, true); err != nil {
		t.Fatalf("runSearchCmd exact: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "STORY-123 done") {
		t.Errorf("exact mode should substring-match the query; got: %q", out)
	}
	if strings.Contains(out, "CSS Animation") {
		t.Errorf("exact mode must not return unrelated nodes; got: %q", out)
	}
}

func TestSearchCmd_NoResults(t *testing.T) {
	_, dbPath := newTestStore(t)
	var buf bytes.Buffer
	if err := runSearchCmd(&buf, dbPath, "nonexistent query xyz123", "", 10, true, false); err != nil {
		t.Fatalf("runSearchCmd should not error on empty results: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("expected empty output for no results; got: %q", buf.String())
	}
}

func TestSearchCmd_MissingQuery(t *testing.T) {
	_, dbPath := newTestStore(t)
	err := runSearchCmd(io.Discard, dbPath, "", "", 10, false, false)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !strings.Contains(err.Error(), "--query") {
		t.Errorf("expected '--query' in error message; got: %v", err)
	}
}

// ── options subcommand tests (hooks-options-cli story) ────────────────────────

func TestOptionsCmd_PrintDefaults(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, key := range []string{
		"session_orient_enabled", "auto_recall", "pre_compact_enabled",
		"reinject_on_compact", "sweep_interval_turns",
		"subagent_orient_enabled", "subagent_audit_enabled",
	} {
		if !strings.Contains(out, key) {
			t.Errorf("expected key %q in output; got:\n%s", key, out)
		}
	}
	if !strings.Contains(out, "false") {
		t.Errorf("expected 'false' for bool defaults in output; got:\n%s", out)
	}
	if !strings.Contains(out, "15") {
		t.Errorf("expected '15' for sweep_interval_turns default; got:\n%s", out)
	}
}

func TestOptionsCmd_DBFlagAccepted(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	if err := runOptionsCmd(io.Discard, cfg, []string{"set", "auto_recall", "true"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, []string{"--db", filepath.Join(t.TempDir(), "custom.db")}); err != nil {
		t.Fatalf("print with --db: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "auto_recall") || !strings.Contains(out, "true") {
		t.Errorf("expected auto_recall=true in output; got:\n%s", out)
	}
}

func TestOptionsCmd_DBFlagBeforeSet(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, []string{"--db", filepath.Join(t.TempDir(), "custom.db"), "set", "sweep_interval_turns", "20"}); err != nil {
		t.Fatalf("set after --db: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `"sweep_interval_turns": 20`) {
		t.Errorf("expected sweep_interval_turns 20 in file; got: %s", data)
	}
}

func TestOptionsCmd_SetBool(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, []string{"set", "session_orient_enabled", "true"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Print and verify
	buf.Reset()
	if err := runOptionsCmd(&buf, cfg, nil); err != nil {
		t.Fatalf("print: %v", err)
	}
	if !strings.Contains(buf.String(), "session_orient_enabled") {
		t.Error("key missing from output after set")
	}
	// Read the file directly to confirm the stored value
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `"session_orient_enabled": true`) {
		t.Errorf("config file should contain true; got: %s", data)
	}
}

func TestOptionsCmd_SetInt(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, []string{"set", "sweep_interval_turns", "30"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `"sweep_interval_turns": 30`) {
		t.Errorf("expected sweep_interval_turns 30 in file; got: %s", data)
	}
}

func TestOptionsCmd_SetInt_Zero(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	var buf bytes.Buffer
	if err := runOptionsCmd(&buf, cfg, []string{"set", "sweep_interval_turns", "0"}); err != nil {
		t.Fatalf("set zero: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if !strings.Contains(string(data), `"sweep_interval_turns": 0`) {
		t.Errorf("expected sweep_interval_turns 0 in file; got: %s", data)
	}
}

func TestOptionsCmd_UnknownKey(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	err := runOptionsCmd(io.Discard, cfg, []string{"set", "no_such_option", "true"})
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Errorf("expected 'unknown option' in error; got: %v", err)
	}
}

func TestOptionsCmd_BadBool(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	err := runOptionsCmd(io.Discard, cfg, []string{"set", "auto_recall", "maybe"})
	if err == nil {
		t.Fatal("expected error for bad bool value")
	}
}

func TestOptionsCmd_BadInt(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	err := runOptionsCmd(io.Discard, cfg, []string{"set", "sweep_interval_turns", "-5"})
	if err == nil {
		t.Fatal("expected error for negative integer")
	}
}

func TestOptionsCmd_Idempotent(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	// Set one key.
	if err := runOptionsCmd(io.Discard, cfg, []string{"set", "auto_recall", "true"}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	// Set a different key; auto_recall should be unchanged.
	if err := runOptionsCmd(io.Discard, cfg, []string{"set", "sweep_interval_turns", "20"}); err != nil {
		t.Fatalf("second set: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	s := string(data)
	if !strings.Contains(s, `"auto_recall": true`) {
		t.Errorf("auto_recall should still be true; got: %s", s)
	}
	if !strings.Contains(s, `"sweep_interval_turns": 20`) {
		t.Errorf("sweep_interval_turns should be 20; got: %s", s)
	}
}

// ── doctorCheckHooks tests ────────────────────────────────────────────────────

// writeHookSettings writes ~/.claude/settings.json containing the given
// event→script mapping, mirroring the shape `memoryweb setup` produces.
func writeHookSettings(t *testing.T, home string, hookScripts map[string]string) {
	t.Helper()
	hooks := make(map[string]interface{})
	for event, script := range hookScripts {
		hooks[event] = []interface{}{
			map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"type":    "command",
						"command": script,
					},
				},
			},
		}
	}
	settings := map[string]interface{}{"hooks": hooks}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// writeHookSettingsWith writes a settings.json given a raw event→entries
// map, mirroring whatever polluted state an install may have left behind.
func writeHookSettingsWith(t *testing.T, home string, eventEntries map[string]interface{}) {
	t.Helper()
	settings := map[string]interface{}{"hooks": eventEntries}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, data, 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

// writeHookScript creates the named hook script in dir with the given mode.
func writeHookScript(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/usr/bin/env bash\n"), mode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// allHookScripts returns the six hook event→script names supported by setup.
var allHookScripts = []struct {
	event  string
	name   string
	script string
}{
	{"Stop", "Stop", "memoryweb_save_hook.sh"},
	{"PreCompact", "PreCompact", "memoryweb_precompact_hook.sh"},
	{"UserPromptSubmit", "UserPromptSubmit", "memoryweb_userpromptsubmit_hook.sh"},
	{"SubagentStart", "SubagentStart", "memoryweb_subagent_start_hook.sh"},
	{"SubagentStop", "SubagentStop", "memoryweb_subagent_stop_hook.sh"},
	{"PostCompact", "PostCompact", "memoryweb_postcompact_hook.sh"},
}

func TestDoctorCheckHooks_AllInstalled(t *testing.T) {
	home := t.TempDir()
	scriptsDir := t.TempDir()
	hookScripts := make(map[string]string)
	for _, h := range allHookScripts {
		hookScripts[h.event] = writeHookScript(t, scriptsDir, h.script, 0755)
	}
	writeHookSettings(t, home, hookScripts)

	message, status := doctorCheckHooks(home)
	if status != "ok" {
		t.Fatalf("expected status ok, got %q (message: %s)", status, message)
	}
	if message != "All hooks installed" {
		t.Errorf("expected 'All hooks installed', got %q", message)
	}
}

func TestDoctorCheckHooks_ReportsEachMissingHook(t *testing.T) {
	home := t.TempDir()
	scriptsDir := t.TempDir()
	// Only Stop + PreCompact installed — the pre-v1.54.0 surface.
	writeHookSettings(t, home, map[string]string{
		"Stop":       writeHookScript(t, scriptsDir, "memoryweb_save_hook.sh", 0755),
		"PreCompact": writeHookScript(t, scriptsDir, "memoryweb_precompact_hook.sh", 0755),
	})

	message, status := doctorCheckHooks(home)
	if status != "warn" {
		t.Fatalf("expected status warn for partial install, got %q (message: %s)", status, message)
	}
	for _, h := range allHookScripts {
		switch h.event {
		case "Stop", "PreCompact":
			continue
		default:
			if !strings.Contains(message, h.name+" hook missing") {
				t.Errorf("expected message to report missing %q hook; got: %s", h.name, message)
			}
		}
	}
}

func TestDoctorCheckHooks_NoHooksConfigured(t *testing.T) {
	home := t.TempDir()
	// settings.json exists but has no memoryweb hooks.
	writeHookSettings(t, home, map[string]string{})

	message, status := doctorCheckHooks(home)
	if status != "fail" {
		t.Fatalf("expected status fail for no hooks, got %q (message: %s)", status, message)
	}
	if !strings.Contains(message, "run: memoryweb setup") {
		t.Errorf("expected setup hint in message; got: %s", message)
	}
	if !strings.Contains(message, "Stop hook missing") {
		t.Errorf("expected Stop hook to be reported missing; got: %s", message)
	}
}

func TestDoctorCheckHooks_NoSettingsFile(t *testing.T) {
	home := t.TempDir()
	message, status := doctorCheckHooks(home)
	if status != "fail" {
		t.Fatalf("expected status fail when settings file missing, got %q", status)
	}
	if !strings.Contains(message, "run: memoryweb setup") {
		t.Errorf("expected setup hint when settings file missing; got: %s", message)
	}
}

func TestDoctorCheckHooks_ScriptMissing(t *testing.T) {
	home := t.TempDir()
	// Config points at a script that does not exist.
	phantomDir := t.TempDir()
	writeHookSettings(t, home, map[string]string{
		"Stop": filepath.Join(phantomDir, "memoryweb_save_hook.sh"),
	})
	message, status := doctorCheckHooks(home)
	if status != "warn" {
		t.Fatalf("expected status warn for missing script file, got %q (message: %s)", status, message)
	}
	if !strings.Contains(message, "Stop hook script missing") {
		t.Errorf("expected script-missing report for Stop; got: %s", message)
	}
}

func TestDoctorCheckHooks_NotExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable bit is not tracked on Windows")
	}
	home := t.TempDir()
	scriptsDir := t.TempDir()
	writeHookSettings(t, home, map[string]string{
		"Stop": writeHookScript(t, scriptsDir, "memoryweb_save_hook.sh", 0644),
	})
	message, status := doctorCheckHooks(home)
	if status != "warn" {
		t.Fatalf("expected status warn for non-executable script, got %q (message: %s)", status, message)
	}
	if !strings.Contains(message, "Stop hook not executable") {
		t.Errorf("expected not-executable report for Stop; got: %s", message)
	}
}

// TestSetupResolvesRelativeDBPath: `memoryweb setup --db ./x.db` must store the
// resolved absolute path in the hook env (and desktop MCP configs), not the
// raw relative string — otherwise the DB resolves against the client's CWD.
func TestSetupResolvesRelativeDBPath(t *testing.T) {
	home := t.TempDir()
	hooks := t.TempDir()
	dbPath := filepath.Join("..", "relative", "x.db")
	want, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	for _, script := range [6]string{
		"memoryweb_save_hook.sh",
		"memoryweb_precompact_hook.sh",
		"memoryweb_userpromptsubmit_hook.sh",
		"memoryweb_subagent_start_hook.sh",
		"memoryweb_subagent_stop_hook.sh",
		"memoryweb_postcompact_hook.sh",
	} {
		writeHookScript(t, hooks, script, 0755)
	}

	var out bytes.Buffer
	if err := runSetup(&out, strings.NewReader("n\n"), false, dbPath, hooks, home); err != nil {
		t.Fatalf("runSetup: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var settings struct {
		Hooks map[string]interface{} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json invalid JSON: %v\n%s", err, data)
	}
	stopEntry, ok := settings.Hooks["Stop"].([]interface{})
	if !ok || len(stopEntry) == 0 {
		t.Fatalf("Stop hook entry missing: %s", data)
	}
	env, ok := stopEntry[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("Stop hook env missing: %s", data)
	}
	got := env["MEMORYWEB_DB"]
	if got != want {
		t.Errorf("MEMORYWEB_DB = %v, want resolved absolute path %v", got, want)
	}
}

// TestSetupRunRemovesStaleHookEntries: re-running setup over a settings file
// polluted by earlier installs (stale hook paths + stale relative env) must
// yield exactly one fresh entry per hook — old Stop/PreCompact entries are
// removed and the env carries the resolved absolute DB path.
func TestSetupRunRemovesStaleHookEntries(t *testing.T) {
	home := t.TempDir()
	hooks := t.TempDir()
	for _, script := range [6]string{
		"memoryweb_save_hook.sh",
		"memoryweb_precompact_hook.sh",
		"memoryweb_userpromptsubmit_hook.sh",
		"memoryweb_subagent_start_hook.sh",
		"memoryweb_subagent_stop_hook.sh",
		"memoryweb_postcompact_hook.sh",
	} {
		writeHookScript(t, hooks, script, 0755)
	}

	// Simulate the pre-fix state: two old Stop installs at different paths each
	// carrying a stale relative --db, plus an old PreCompact entry.
	stale := []interface{}{
		makeTestEntryWithEnv("/oldA/hooks/memoryweb_save_hook.sh", "./.memoryweb.db"),
		makeTestEntryWithEnv("/oldB/hooks/memoryweb_save_hook.sh", "~/stale/other.db"),
		makeTestEntryWithEnv("/oldA/hooks/memoryweb_precompact_hook.sh", "./.memoryweb.db"),
	}
	writeHookSettingsWith(t, home, map[string]interface{}{
		"Stop":       stale[:2],
		"PreCompact": stale[2:],
	})

	dbPath := "nested/custom.db"
	want, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	var out bytes.Buffer
	if err := runSetup(&out, strings.NewReader("n\n"), false, dbPath, hooks, home); err != nil {
		t.Fatalf("runSetup: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var settings struct {
		Hooks map[string]interface{} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json invalid JSON: %v\n%s", err, data)
	}

	assertHookEntries := func(event, scriptName string) {
		t.Helper()
		entries, _ := settings.Hooks[event].([]interface{})
		if len(entries) != 1 {
			t.Fatalf("%s: want 1 entry, got %d; settings:\n%s", event, len(entries), data)
		}
		h := entries[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
		cmd, _ := h["command"].(string)
		if wantCmd := filepath.Join(hooks, scriptName); cmd != wantCmd {
			t.Errorf("%s: command = %q, want %q", event, cmd, wantCmd)
		}
		envEnv, _ := h["env"].(map[string]interface{})
		if db, _ := envEnv["MEMORYWEB_DB"].(string); db != want {
			t.Errorf("%s: MEMORYWEB_DB = %q, want resolved %q", event, db, want)
		}
	}
	assertHookEntries("Stop", "memoryweb_save_hook.sh")
	assertHookEntries("PreCompact", "memoryweb_precompact_hook.sh")
}
