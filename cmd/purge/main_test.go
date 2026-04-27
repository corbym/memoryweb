package main_test

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
)

// ── test setup ────────────────────────────────────────────────────────────────

// purgeBin is the path to the compiled purge binary, set by TestMain.
var purgeBin string

// TestMain compiles the purge binary once before all tests run.
// If the build fails, all tests fail immediately.
func TestMain(m *testing.M) {
	root := findRepoRoot()

	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-purge-%d", os.Getpid()))
	buildCmd := exec.Command("go", "build", "-o", bin, "./cmd/purge")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot build cmd/purge: %v\n%s\n", err, out)
		os.Exit(1)
	}
	purgeBin = bin

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
// The caller must call store.Close() before running the purge binary
// (WAL mode allows concurrent readers but not concurrent writers).
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

// runPurge executes the purge binary with the given flags.
// Returns combined stdout+stderr and the exit code.
func runPurge(t *testing.T, args ...string) (output string, exitCode int) {
	t.Helper()
	cmd := exec.Command(purgeBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec purge: %v", err)
		}
	}
	return string(out), exitCode
}

// nodeExists returns true if the node id is present in the nodes table.
func nodeExists(t *testing.T, dbPath, nodeID string) bool {
	t.Helper()
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer rawDB.Close()
	var count int
	rawDB.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, nodeID).Scan(&count)
	return count > 0
}

// edgeRefsNode returns true if any edge references nodeID as from_node or to_node.
func edgeRefsNode(t *testing.T, dbPath, nodeID string) bool {
	t.Helper()
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer rawDB.Close()
	var count int
	rawDB.QueryRow(
		`SELECT COUNT(*) FROM edges WHERE from_node = ? OR to_node = ?`, nodeID, nodeID,
	).Scan(&count)
	return count > 0
}

// auditLogHasPurge returns true if audit_log has a 'purge' entry for nodeID.
func auditLogHasPurge(t *testing.T, dbPath, nodeID string) bool {
	t.Helper()
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer rawDB.Close()
	var count int
	rawDB.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE node_id = ? AND action = 'purge'`, nodeID,
	).Scan(&count)
	return count > 0
}

// mustAddNode adds a node and returns its ID, fataling on error.
func mustAddNode(t *testing.T, store *db.Store, label, domain string) string {
	t.Helper()
	n, err := store.AddNode(label, "", "", domain, nil, "")
	if err != nil {
		t.Fatalf("AddNode(%q): %v", label, err)
	}
	return n.ID
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestPurgeDryRunDoesNotDelete: --dry-run prints what would be purged without
// removing any rows.
func TestPurgeDryRunDoesNotDelete(t *testing.T) {
	dbPath, store := newTestDB(t)
	id1 := mustAddNode(t, store, "Purge Test Alpha", "test")
	id2 := mustAddNode(t, store, "Purge Test Beta", "test")
	store.ArchiveNode(id1, "stale")
	store.ArchiveNode(id2, "stale")
	store.Close()

	out, code := runPurge(t, "--db", dbPath, "--dry-run")
	if code != 0 {
		t.Fatalf("--dry-run exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Purge Test Alpha") {
		t.Errorf("dry-run output should mention 'Purge Test Alpha'; got:\n%s", out)
	}
	if !strings.Contains(out, "Purge Test Beta") {
		t.Errorf("dry-run output should mention 'Purge Test Beta'; got:\n%s", out)
	}

	if !nodeExists(t, dbPath, id1) {
		t.Error("node 1 should still exist after --dry-run")
	}
	if !nodeExists(t, dbPath, id2) {
		t.Error("node 2 should still exist after --dry-run")
	}
}

// TestPurgeRequiresConfirmFlag: running without --confirm or --dry-run must
// exit non-zero and leave the db untouched.
func TestPurgeRequiresConfirmFlag(t *testing.T) {
	dbPath, store := newTestDB(t)
	id := mustAddNode(t, store, "Needs Confirm", "test")
	store.ArchiveNode(id, "stale")
	store.Close()

	out, code := runPurge(t, "--db", dbPath)
	if code == 0 {
		t.Errorf("expected non-zero exit without --confirm; got 0; output:\n%s", out)
	}
	if !nodeExists(t, dbPath, id) {
		t.Error("node should not have been deleted without --confirm")
	}
}

// TestPurgeDeletesArchivedNodes: --confirm hard-deletes archived nodes and
// cascades their edges.
func TestPurgeDeletesArchivedNodes(t *testing.T) {
	dbPath, store := newTestDB(t)
	idA := mustAddNode(t, store, "Delete Me A", "test")
	idB := mustAddNode(t, store, "Delete Me B", "test")

	edge, err := store.AddEdge(idA, idB, "connects_to", "test edge")
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	store.ArchiveNode(idA, "stale")
	store.ArchiveNode(idB, "stale")
	store.Close()

	out, code := runPurge(t, "--db", dbPath, "--confirm")
	if code != 0 {
		t.Fatalf("--confirm exited %d; output:\n%s", code, out)
	}
	if nodeExists(t, dbPath, idA) {
		t.Error("node A should have been purged")
	}
	if nodeExists(t, dbPath, idB) {
		t.Error("node B should have been purged")
	}
	if edgeRefsNode(t, dbPath, edge.ID) {
		t.Error("edge should have been deleted along with its nodes")
	}
}

// TestPurgeNeverTouchesLiveNodes: nodes without archived_at must survive a purge.
func TestPurgeNeverTouchesLiveNodes(t *testing.T) {
	dbPath, store := newTestDB(t)
	idArchived := mustAddNode(t, store, "I Am Archived", "test")
	idLive := mustAddNode(t, store, "I Am Live", "test")

	store.ArchiveNode(idArchived, "stale")
	store.Close()

	out, code := runPurge(t, "--db", dbPath, "--confirm")
	if code != 0 {
		t.Fatalf("--confirm exited %d; output:\n%s", code, out)
	}
	if !nodeExists(t, dbPath, idLive) {
		t.Error("live node should NOT have been purged")
	}
	if nodeExists(t, dbPath, idArchived) {
		t.Error("archived node should have been purged")
	}
}

// TestPurgeBeforeFlag: --before scopes the purge by archive timestamp.
func TestPurgeBeforeFlag(t *testing.T) {
	t.Run("future date purges all archived", func(t *testing.T) {
		dbPath, store := newTestDB(t)
		id1 := mustAddNode(t, store, "Before Future A", "test")
		id2 := mustAddNode(t, store, "Before Future B", "test")
		store.ArchiveNode(id1, "stale")
		store.ArchiveNode(id2, "stale")
		store.Close()

		out, code := runPurge(t, "--db", dbPath, "--before", "2099-01-01", "--confirm")
		if code != 0 {
			t.Fatalf("exited %d; output:\n%s", code, out)
		}
		if nodeExists(t, dbPath, id1) || nodeExists(t, dbPath, id2) {
			t.Error("nodes should have been purged (archived before 2099-01-01)")
		}
	})

	t.Run("past date purges nothing", func(t *testing.T) {
		dbPath, store := newTestDB(t)
		id := mustAddNode(t, store, "Before Past", "test")
		store.ArchiveNode(id, "stale")
		store.Close()

		// archived_at is ~now (2026-04-26), before 2020-01-01 → no match
		_, _ = runPurge(t, "--db", dbPath, "--before", "2020-01-01", "--confirm")
		if !nodeExists(t, dbPath, id) {
			t.Error("node should NOT have been purged (archived after --before date)")
		}
	})
}

// TestPurgeLogsToAuditLog: every purged node must have an audit_log entry
// with action='purge'.
func TestPurgeLogsToAuditLog(t *testing.T) {
	dbPath, store := newTestDB(t)
	id := mustAddNode(t, store, "Audit Log Purge Test", "test")
	store.ArchiveNode(id, "stale")
	store.Close()

	out, code := runPurge(t, "--db", dbPath, "--confirm")
	if code != 0 {
		t.Fatalf("--confirm exited %d; output:\n%s", code, out)
	}
	if !auditLogHasPurge(t, dbPath, id) {
		t.Error("audit_log should have a 'purge' entry for the deleted node")
	}
}
