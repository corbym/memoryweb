package dream_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
)

// ── test setup ────────────────────────────────────────────────────────────────

// dreamBin is the path to the compiled memoryweb binary, set by TestMain.
var dreamBin string

// TestMain compiles the memoryweb binary once before all tests run.
func TestMain(m *testing.M) {
	root := findRepoRoot()

	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-%d%s", os.Getpid(), exeSuffix))
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

// runDream executes "memoryweb dream" with the given flags.
// Returns combined stdout+stderr and the exit code.
func runDream(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	allArgs := append([]string{"dream"}, args...)
	cmd := exec.Command(dreamBin, allArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec dream: %v", err)
		}
	}
	return string(out), exitCode
}

// mustAddNode adds a node and returns its ID, fataling on error.
func mustAddNode(t *testing.T, store *db.Store, label, domain string) string {
	t.Helper()
	n, err := store.AddNode(label, "desc", "why it matters", domain, nil, "", false)
	if err != nil {
		t.Fatalf("AddNode(%q): %v", label, err)
	}
	return n.ID
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDreamExitsZeroWithEmptyDB: an empty database produces a clean report and
// exits 0.
func TestDreamExitsZeroWithEmptyDB(t *testing.T) {
	dbPath, store := newTestDB(t)
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d with empty DB; output:\n%s", code, out)
	}
	if len(strings.TrimSpace(out)) == 0 {
		t.Error("dream should produce some output even for an empty DB")
	}
}

// TestDreamOutputsRecentNodes: labels of recently added nodes appear in the
// output.
func TestDreamOutputsRecentNodes(t *testing.T) {
	dbPath, store := newTestDB(t)
	mustAddNode(t, store, "Alpha Decision", "project-a")
	mustAddNode(t, store, "Beta Finding", "project-b")
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Alpha Decision") {
		t.Errorf("output should contain 'Alpha Decision'; got:\n%s", out)
	}
	if !strings.Contains(out, "Beta Finding") {
		t.Errorf("output should contain 'Beta Finding'; got:\n%s", out)
	}
}

// TestDreamOutputsDriftCandidates: nodes flagged as drift candidates appear in
// the output with their reason.
func TestDreamOutputsDriftCandidates(t *testing.T) {
	dbPath, store := newTestDB(t)
	mustAddNode(t, store, "Old Auth Strategy", "security")
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Old Auth Strategy") {
		t.Errorf("output should contain drift candidate 'Old Auth Strategy'; got:\n%s", out)
	}
}

// TestDreamExitsNonZeroOnInvalidDB: pointing dream at a directory (not a valid
// SQLite file) causes a non-zero exit.
func TestDreamExitsNonZeroOnInvalidDB(t *testing.T) {
	dir := t.TempDir() // a directory, not a file

	out, code := runDream(t, "--db", dir)
	if code == 0 {
		t.Errorf("expected non-zero exit for invalid DB path; got 0; output:\n%s", out)
	}
}

// TestDreamReportIncludesHeader: the output starts with a recognisable header
// so the Stop hook can identify the block.
func TestDreamReportIncludesHeader(t *testing.T) {
	dbPath, store := newTestDB(t)
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "memoryweb dream") {
		t.Errorf("output should contain header 'memoryweb dream'; got:\n%s", out)
	}
}

// TestDreamReportShowsNodeCount: the output includes the count of recent nodes
// in the expected "Recent nodes (N):" header.
func TestDreamReportShowsNodeCount(t *testing.T) {
	dbPath, store := newTestDB(t)
	mustAddNode(t, store, "Node One", "test")
	mustAddNode(t, store, "Node Two", "test")
	mustAddNode(t, store, "Node Three", "test")
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Recent nodes (3):") {
		t.Errorf("output should contain 'Recent nodes (3):'; got:\n%s", out)
	}
}

// TestDreamReportShowsDisconnectedSection: the output includes a disconnected
// nodes section; orphan nodes are listed, connected nodes are not.
func TestDreamReportShowsDisconnectedSection(t *testing.T) {
	dbPath, store := newTestDB(t)
	mustAddNode(t, store, "Orphan Node", "test")
	connectedID := mustAddNode(t, store, "Connected Node", "test")
	otherID := mustAddNode(t, store, "Other Node", "test")
	store.AddEdge(connectedID, otherID, "connects_to", "") //nolint:errcheck
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}

	// Extract the disconnected section only.
	idx := strings.Index(out, "Disconnected nodes (")
	if idx < 0 {
		t.Fatalf("output should contain 'Disconnected nodes ('; got:\n%s", out)
	}
	disconnectedSection := out[idx:]

	if !strings.Contains(disconnectedSection, "Orphan Node") {
		t.Errorf("orphan node label should appear in disconnected section; got:\n%s", disconnectedSection)
	}
	if strings.Contains(disconnectedSection, "Connected Node") {
		t.Errorf("connected node should not appear in disconnected section; got:\n%s", disconnectedSection)
	}
}

// TestDreamReportDisconnectedNone: empty disconnected section when all nodes
// are connected.
func TestDreamReportDisconnectedNone(t *testing.T) {
	dbPath, store := newTestDB(t)
	aID := mustAddNode(t, store, "Alpha", "test")
	bID := mustAddNode(t, store, "Beta", "test")
	store.AddEdge(aID, bID, "connects_to", "") //nolint:errcheck
	store.Close()

	out, code := runDream(t, "--db", dbPath)
	if code != 0 {
		t.Fatalf("dream exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Disconnected nodes (0):") {
		t.Errorf("output should contain 'Disconnected nodes (0):'; got:\n%s", out)
	}
}
