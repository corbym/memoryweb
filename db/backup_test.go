package db_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
)

// Backup must produce a single, self-contained snapshot file with no -wal/-shm
// sidecars, readable on its own — even while the source DB is still open. This
// is the safe alternative to copying the live DB folder, which can capture the
// .db and -wal at different instants and corrupt on recombination.
func TestBackup_ProducesStandaloneSnapshotWhileSourceOpen(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")

	src, err := db.New(srcPath)
	if err != nil {
		t.Fatalf("db.New(src): %v", err)
	}
	defer src.Close()

	n := mustAddNode(t, src, "backup me", "testing")

	// Source intentionally left OPEN to prove the online backup works against a
	// live database (the real-world case: memoryweb running as an MCP server).
	destPath := filepath.Join(dir, "backup.db")
	if err := db.Backup(srcPath, destPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	for _, sidecar := range []string{destPath + "-wal", destPath + "-shm"} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Errorf("expected no sidecar %s, but it exists", sidecar)
		}
	}

	// Reopen the snapshot on its own and confirm the node is present.
	dest, err := db.New(destPath)
	if err != nil {
		t.Fatalf("db.New(dest): %v", err)
	}
	defer dest.Close()

	got, err := dest.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode from snapshot: %v", err)
	}
	if got.Node.Label != "backup me" {
		t.Errorf("snapshot node label = %q, want %q", got.Node.Label, "backup me")
	}
}

// Backup must refuse to overwrite an existing destination so a stray invocation
// cannot clobber a previous good snapshot.
func TestBackup_RefusesToOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	src, err := db.New(srcPath)
	if err != nil {
		t.Fatalf("db.New(src): %v", err)
	}
	defer src.Close()

	destPath := filepath.Join(dir, "exists.db")
	if err := os.WriteFile(destPath, []byte("not empty"), 0600); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	if err := db.Backup(srcPath, destPath); err == nil {
		t.Fatal("expected Backup to refuse an existing destination, got nil error")
	}
}

// Close must checkpoint the WAL into the main .db file so the file is
// self-sufficient at rest. A keeper connection is held open so SQLite's
// automatic last-connection-close checkpoint does NOT fire — isolating the
// behaviour of Store.Close() itself.
func TestClose_CheckpointsWALIntoMainFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mw.db")

	keeper, err := db.New(path)
	if err != nil {
		t.Fatalf("db.New(keeper): %v", err)
	}
	defer keeper.Close()

	s, err := db.New(path)
	if err != nil {
		t.Fatalf("db.New(s): %v", err)
	}
	n := mustAddNode(t, s, "checkpoint me", "testing")
	s.Close() // expected to checkpoint(TRUNCATE) before closing

	// Copy ONLY the main .db file, deliberately omitting any -wal/-shm. If the
	// data was checkpointed into the main file, it survives; if it was stranded
	// in the WAL, it is lost.
	standalone := filepath.Join(dir, "standalone.db")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read main db: %v", err)
	}
	if err := os.WriteFile(standalone, data, 0600); err != nil {
		t.Fatalf("write standalone copy: %v", err)
	}

	reopened, err := db.New(standalone)
	if err != nil {
		t.Fatalf("db.New(standalone): %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.GetNode(n.ID); err != nil {
		t.Fatalf("node missing from main file after Close — WAL was not checkpointed: %v", err)
	}
}

// Backup must reject destination paths that could break out of the intended
// target or smuggle extra SQL when the path is interpolated into VACUUM INTO.
func TestBackup_RejectsUnsafeDestination(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	src, err := db.New(srcPath)
	if err != nil {
		t.Fatalf("db.New(src): %v", err)
	}
	defer src.Close()

	for dest, reason := range map[string]string{
		filepath.Join(dir, "clean;drop.db"): "semicolon",
		filepath.Join(dir, "quote'.db"):     "single quote",
		filepath.Join(dir, "..", "up.db"):   "path traversal",
		filepath.Join(dir, "x--drop.db"):    "SQL comment (--)",
	} {
		if err := db.Backup(srcPath, dest); err == nil {
			t.Errorf("Backup(%q) should be rejected (%s), got nil", dest, reason)
		}
	}

	ok := filepath.Join(dir, "legit-backup.db")
	if err := db.Backup(srcPath, ok); err != nil {
		t.Errorf("Backup with a safe destination should succeed, got: %v", err)
	}
}

// ── migration v17 ─────────────────────────────────────────────────────────────

// TestMigration17_IndexesCorrect verifies that migration v17 drops the two
// unused indexes and creates the composite trust-lookup index.
func TestMigration17_IndexesCorrect(t *testing.T) {
	s := newStore(t)
	rawDB := s.DB()

	for _, idx := range []string{"idx_significance_log_trust_lookup"} {
		var count int
		rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&count)
		if count == 0 {
			t.Errorf("expected index %q to exist after migration v17", idx)
		}
	}
	for _, idx := range []string{"idx_significance_log_domain", "idx_significance_log_call_id"} {
		var count int
		rawDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&count)
		if count != 0 {
			t.Errorf("expected index %q to be dropped by migration v17, but it still exists", idx)
		}
	}
}

// TestStartupRetentionPrune_RemovesOldNonTrustRows verifies that non-trust rows
// older than 30 days are deleted on store open, while recent non-trust rows
// (< 30 days) survive.
func TestStartupRetentionPrune_RemovesOldNonTrustRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}

	old := time.Now().Add(-31 * 24 * time.Hour).UTC()
	for i, rankType := range []string{"structural", "uncurated", "potentially_stale"} {
		if _, err := s.DB().Exec(
			`INSERT INTO significance_log
			 (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score)
			 VALUES (?, 'c1', ?, 'dom', 10, 'n1', 'N', ?, NULL)`,
			fmt.Sprintf("old-%d", i), old, rankType,
		); err != nil {
			t.Fatalf("insert old row: %v", err)
		}
	}

	recent := time.Now().Add(-1 * 24 * time.Hour).UTC()
	if _, err := s.DB().Exec(
		`INSERT INTO significance_log
		 (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score)
		 VALUES ('recent-structural', 'c2', ?, 'dom', 10, 'n1', 'N', 'structural', NULL)`,
		recent,
	); err != nil {
		t.Fatalf("insert recent row: %v", err)
	}
	s.Close()

	s2, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New reopen: %v", err)
	}
	defer s2.Close()

	var nonTrustCount int
	s2.DB().QueryRow(`SELECT COUNT(*) FROM significance_log WHERE rank_type != 'trust'`).Scan(&nonTrustCount)
	if nonTrustCount != 1 {
		t.Errorf("after retention prune: want 1 recent non-trust row, got %d", nonTrustCount)
	}

	var oldCount int
	s2.DB().QueryRow(`SELECT COUNT(*) FROM significance_log WHERE called_at < datetime('now', '-30 days')`).Scan(&oldCount)
	if oldCount != 0 {
		t.Errorf("after retention prune: want 0 old rows, got %d", oldCount)
	}
}

// TestStartupRetentionPrune_KeepsTrustRows verifies that trust rows of any age
// survive the startup retention prune.
func TestStartupRetentionPrune_KeepsTrustRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}

	old := time.Now().Add(-60 * 24 * time.Hour).UTC()
	score := 0.8
	for i := 0; i < 3; i++ {
		if _, err := s.DB().Exec(
			`INSERT INTO significance_log
			 (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score)
			 VALUES (?, 'c1', ?, 'dom', 10, ?, 'N', 'trust', ?)`,
			fmt.Sprintf("trust-%d", i), old, fmt.Sprintf("node-%d", i), score,
		); err != nil {
			t.Fatalf("insert trust row: %v", err)
		}
	}
	s.Close()

	s2, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New reopen: %v", err)
	}
	defer s2.Close()

	var count int
	s2.DB().QueryRow(`SELECT COUNT(*) FROM significance_log WHERE rank_type = 'trust'`).Scan(&count)
	if count != 3 {
		t.Errorf("old trust rows must survive prune: want 3, got %d", count)
	}
}

// ── Close / compile-time assertions ──────────────────────────────────────────

// Close must surface its error to callers (checkpoint + close), and a healthy
// close closes cleanly. The compile-time assertion guarantees the signature
// stays error-returning so nobody reintroduces the silently-discarded Close.
var _ interface{ Close() error } = (*db.Store)(nil)

func TestClose_HealthyStoreReturnsNil(t *testing.T) {
	dir := t.TempDir()
	s, err := db.New(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close on a healthy store should return nil, got: %v", err)
	}
}
