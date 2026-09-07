package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	home := t.TempDir() // no .claude/settings.local.json

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

	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
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
