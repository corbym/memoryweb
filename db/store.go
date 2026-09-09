package db

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func init() {
	// Register sqlite-vec extension for all future SQLite3 connections.
	// Called once at process start, before any connection is opened.
	vec.Auto()

	// Register a custom driver whose connections gain the
	// memoryweb_normalise_label scalar function, so label matching can run
	// inside SQL instead of loading whole tables into memory.
	sql.Register("sqlite3_memoryweb", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("memoryweb_normalise_label", normaliseLabel, true)
		},
	})
}

type Store struct {
	db           *sql.DB
	vecAvailable bool
}

func New(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) + "?_journal_mode=WAL&_foreign_keys=on"
	db, err := sql.Open("sqlite3_memoryweb", dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		log.Printf("[memoryweb] chmod %s: %v", path, err)
	}
	store.checkVecAvailable()
	return store, nil
}

func (st *Store) Close() error {
	// Checkpoint the WAL back into the main .db file before closing so the file
	// is self-sufficient at rest. Without this, recently-written data can live
	// in the -wal sidecar, making naive file-copy backups (which may miss or
	// desync the -wal) lossy or corrupting. Best-effort: a failed checkpoint
	// must not prevent the connection from closing.
	st.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`) //nolint:errcheck
	return st.db.Close()
}

// validateBackupDest rejects destination paths that would inject SQL into
// the VACUUM INTO statement or escape the source database's directory.
func validateBackupDest(srcPath, destPath string) error {
	if strings.ContainsAny(destPath, ";'") || strings.Contains(destPath, "--") {
		return fmt.Errorf("backup destination path must not contain ;, ', or --")
	}
	srcDir := filepath.Dir(filepath.Clean(srcPath))
	destDir := filepath.Dir(filepath.Clean(destPath))
	if destDir != srcDir {
		return fmt.Errorf("backup destination must be in the same directory as the source database")
	}
	return nil
}

// Backup writes a transactionally-consistent standalone snapshot of the database
// at srcPath to destPath using VACUUM INTO. The result is a single self-contained
// file with no -wal/-shm sidecars, safe to copy or sync even while the source DB
// is in use. It refuses to overwrite an existing destination.
func Backup(srcPath, destPath string) error {
	if err := validateBackupDest(srcPath, destPath); err != nil {
		return err
	}
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination already exists: %s", destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination: %w", err)
	}

	s, err := New(srcPath)
	if err != nil {
		return err
	}
	defer s.Close() //nolint:errcheck

	// VACUUM INTO does not support bind parameters in all SQLite versions; the
	// destination path is sanitized above before being interpolated.
	if _, err := s.db.Exec(`VACUUM INTO '` + destPath + `'`); err != nil {
		return fmt.Errorf("vacuum into %s: %w", destPath, err)
	}
	return nil
}

// VecAvailable reports whether sqlite-vec is loaded and the node_embeddings
// table is available for semantic search.
func (st *Store) VecAvailable() bool {
	return st.vecAvailable
}

// DB returns the underlying *sql.DB. Used only in tests that need raw SQL access
// to internal tables (config, node_embeddings) to set up or assert state.
func (st *Store) DB() *sql.DB {
	return st.db
}

// checkVecAvailable verifies that the sqlite-vec extension is loaded and the
// node_embeddings table exists. Sets s.vecAvailable accordingly.
func (st *Store) checkVecAvailable() {
	var v string
	if err := st.db.QueryRow("SELECT vec_version()").Scan(&v); err != nil {
		log.Printf("[memoryweb] sqlite-vec not available: %v; falling back to text search", err)
		return
	}
	var dummy int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM node_embeddings").Scan(&dummy); err != nil {
		log.Printf("[memoryweb] node_embeddings table not available: %v; falling back to text search", err)
		return
	}
	st.vecAvailable = true
	log.Printf("[memoryweb] sqlite-vec %s loaded; semantic search enabled", v)
}

// ── doctor diagnostics ────────────────────────────────────────────────────────

// SchemaVersion returns the highest applied migration version and the highest
// version defined in the binary. applied is 0 if no migrations have been recorded.
func (st *Store) SchemaVersion() (applied, expected int, err error) {
	err = st.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&applied)
	if err != nil {
		// schema_migrations may not exist on a completely uninitialised DB.
		if strings.Contains(err.Error(), "no such table") {
			err = nil // treat as version 0
		}
		return 0, 0, err
	}
	for _, migration := range migrations {
		if migration.version > expected {
			expected = migration.version
		}
	}
	return applied, expected, nil
}

// VecVersion returns the sqlite-vec version string, or "" if unavailable.
func (st *Store) VecVersion() string {
	var v string
	if err := st.db.QueryRow("SELECT vec_version()").Scan(&v); err != nil {
		return ""
	}
	return v
}

// EmbeddingCoverage returns the count of live nodes and the count that have an
// embedding in node_embeddings. covered is always 0 if sqlite-vec is unavailable.
func (st *Store) EmbeddingCoverage() (live, covered int, err error) {
	if err = st.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NULL`).Scan(&live); err != nil {
		return
	}
	if !st.vecAvailable {
		return
	}
	err = st.db.QueryRow(`
		SELECT COUNT(*) FROM nodes n
		JOIN node_embeddings e ON e.node_id = n.id
		WHERE n.archived_at IS NULL
	`).Scan(&covered)
	return
}

// NodeCounts returns the count of live and archived nodes.
func (st *Store) NodeCounts() (live, archived int, err error) {
	if err = st.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NULL`).Scan(&live); err != nil {
		return
	}
	err = st.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NOT NULL`).Scan(&archived)
	return
}

// EdgeCount returns the total number of edges.
func (st *Store) EdgeCount() (int, error) {
	var n int
	return n, st.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n)
}

// AuditEntry is a single row from the audit_log table.
type AuditEntry struct {
	Action     string
	NodeLabel  string
	ActionedAt time.Time
}

// LastAuditEntry returns the most recent audit log entry.
// ok is false if the audit log is empty.
func (st *Store) LastAuditEntry() (entry AuditEntry, ok bool, err error) {
	err = st.db.QueryRow(
		`SELECT action, node_label, actioned_at FROM audit_log ORDER BY actioned_at DESC LIMIT 1`,
	).Scan(&entry.Action, &entry.NodeLabel, &entry.ActionedAt)
	if err == sql.ErrNoRows {
		return entry, false, nil
	}
	if err != nil {
		return entry, false, err
	}
	return entry, true, nil
}
