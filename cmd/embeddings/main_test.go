package embeddings_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
)

// ── test setup ────────────────────────────────────────────────────────────────

// backfillBin is the path to the compiled memoryweb binary, set by TestMain.
var backfillBin string

// TestMain compiles the memoryweb binary once before all tests run.
func TestMain(m *testing.M) {
	root := findRepoRoot()

	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-%d", os.Getpid()))
	buildCmd := exec.Command("go", "build", "-o", bin, ".")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot build memoryweb: %v\n%s\n", err, out)
		os.Exit(1)
	}
	backfillBin = bin

	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

// findRepoRoot walks up from the working directory to locate go.mod.
func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// newTestDB creates an isolated Store backed by a temp-file SQLite DB.
func newTestDB(t *testing.T) (dbPath string, store *db.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "test.db")
	var err error
	store, err = db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return dbPath, store
}

// runBackfill executes "memoryweb backfill" with the given flags.
// Returns combined stdout+stderr and the exit code.
func runBackfill(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	allArgs := append([]string{"backfill"}, args...)
	cmd := exec.Command(backfillBin, allArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec backfill: %v", err)
		}
	}
	return string(out), exitCode
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestBackfillExitsZeroWithEmptyDB: no nodes to backfill → exit 0 with output.
func TestBackfillExitsZeroWithEmptyDB(t *testing.T) {
	dbPath, store := newTestDB(t)
	store.Close()

	out, code := runBackfill(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("backfill exited %d with empty DB; output:\n%s", code, out)
	}
	if len(strings.TrimSpace(out)) == 0 {
		t.Error("backfill should produce some output even for an empty DB")
	}
}

// TestBackfillReportsZeroWhenOllamaUnavailable: when Ollama is not running,
// backfill exits 0 and reports that no nodes were backfilled.
func TestBackfillReportsZeroWhenOllamaUnavailable(t *testing.T) {
	dbPath, store := newTestDB(t)
	if _, err := store.AddNode("test node", "desc", "why", "test", nil, "", false); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	store.Close()

	out, code := runBackfill(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("backfill exited %d; output:\n%s", code, out)
	}
	// Without Ollama, embed() returns nil → 0 nodes backfilled.
	// The message distinguishes "nothing to do" from "Ollama unavailable".
	if !strings.Contains(out, "No embeddings stored") && !strings.Contains(out, "No nodes needed") {
		t.Errorf("expected output to report zero backfilled; got:\n%s", out)
	}
}

// TestBackfillExitsNonZeroOnInvalidDB: pointing backfill at a directory (not a
// valid SQLite file) causes a non-zero exit.
func TestBackfillExitsNonZeroOnInvalidDB(t *testing.T) {
	dir := t.TempDir() // a directory, not a file

	out, code := runBackfill(t, "--db", dir)
	if code == 0 {
		t.Errorf("expected non-zero exit for invalid DB path; got 0; output:\n%s", out)
	}
}

// TestBackfillShowsProgressBarWithNodes: when nodes exist, the progress bar
// must appear in stdout even when Ollama is unavailable (progress fires on
// each attempt, not only on successful embed).
func TestBackfillShowsProgressBarWithNodes(t *testing.T) {
	dbPath, store := newTestDB(t)
	// Disable Ollama during AddNode so the node has no pre-stored embedding
	// and backfill has a candidate to process (and fire the progress bar for).
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	if _, err := store.AddNode("progress test node", "desc", "why", "test", nil, "", false); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	store.Close()

	out, code := runBackfill(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("backfill exited %d; output:\n%s", code, out)
	}
	// Progress bar must contain the bracket markers and N/Total counter.
	if !strings.Contains(out, "[") || !strings.Contains(out, "]") {
		t.Errorf("expected progress bar '[...]' in output; got:\n%q", out)
	}
	if !strings.Contains(out, "1/1") {
		t.Errorf("expected '1/1' counter in progress output; got:\n%q", out)
	}
}

// TestBackfillQuietSuppressesAllOutput: -q must produce no stdout at all.
// (stderr may still carry log lines from the Go logger; we only check stdout.)
func TestBackfillQuietSuppressesAllOutput(t *testing.T) {
	dbPath, store := newTestDB(t)
	if _, err := store.AddNode("quiet test node", "desc", "why", "test", nil, "", false); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	store.Close()

	cmd := exec.Command(backfillBin, "backfill", "--db", dbPath, "-q")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("backfill -q exited %d", exitErr.ExitCode())
		}
		t.Fatalf("exec: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Errorf("expected no stdout with -q; got:\n%q", got)
	}
}

