package main

import (
	"bytes"
	"encoding/json"
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
		"Claude hooks:", "Graph:", "Drift:", "Last activity:",
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
	if _, err := store.AddNode("test node", "desc", "why", "test-domain", nil, "", false); err != nil {
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
	if _, err := store.AddNode("deprecated old feature", "d", "w", "test-domain", nil, "", false); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(store, &buf, dbPath, home, false)
	out := buf.String()

	if !strings.Contains(out, "1 candidate") {
		t.Errorf("expected '1 candidate' in drift line; got:\n%s", out)
	}
}

func TestRunDoctor_AuditLog_ShowsLastActivity(t *testing.T) {
	store, dbPath := newTestStore(t)
	home := t.TempDir()

	n, err := store.AddNode("audit target", "d", "w", "proj", nil, "", false)
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

