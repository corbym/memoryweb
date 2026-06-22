package db_test

import (
	"os"
	"path/filepath"
	"testing"

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
