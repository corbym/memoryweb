package db

import (
	"database/sql"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func init() {
	// Register sqlite-vec extension for all future SQLite3 connections.
	// Called once at process start, before any connection is opened.
	vec.Auto()
}

type Store struct {
	db           *sql.DB
	vecAvailable bool
}

func New(path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) + "?_journal_mode=WAL&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	os.Chmod(path, 0600) //nolint:errcheck
	s.checkVecAvailable()
	return s, nil
}

func (s *Store) Close() {
	s.db.Close()
}

// VecAvailable reports whether sqlite-vec is loaded and the node_embeddings
// table is available for semantic search.
func (s *Store) VecAvailable() bool {
	return s.vecAvailable
}

// checkVecAvailable verifies that the sqlite-vec extension is loaded and the
// node_embeddings table exists. Sets s.vecAvailable accordingly.
func (s *Store) checkVecAvailable() {
	var v string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&v); err != nil {
		log.Printf("[memoryweb] sqlite-vec not available: %v; falling back to text search", err)
		return
	}
	var dummy int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM node_embeddings").Scan(&dummy); err != nil {
		log.Printf("[memoryweb] node_embeddings table not available: %v; falling back to text search", err)
		return
	}
	s.vecAvailable = true
	log.Printf("[memoryweb] sqlite-vec %s loaded; semantic search enabled", v)
}

// ── doctor diagnostics ────────────────────────────────────────────────────────

// SchemaVersion returns the highest applied migration version and the highest
// version defined in the binary. applied is 0 if no migrations have been recorded.
func (s *Store) SchemaVersion() (applied, expected int, err error) {
	err = s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&applied)
	if err != nil {
		// schema_migrations may not exist on a completely uninitialised DB.
		if strings.Contains(err.Error(), "no such table") {
			err = nil // treat as version 0
		}
		return 0, 0, err
	}
	for _, m := range migrations {
		if m.version > expected {
			expected = m.version
		}
	}
	return applied, expected, nil
}

// VecVersion returns the sqlite-vec version string, or "" if unavailable.
func (s *Store) VecVersion() string {
	var v string
	if err := s.db.QueryRow("SELECT vec_version()").Scan(&v); err != nil {
		return ""
	}
	return v
}

// EmbeddingCoverage returns the count of live nodes and the count that have an
// embedding in node_embeddings. covered is always 0 if sqlite-vec is unavailable.
func (s *Store) EmbeddingCoverage() (live, covered int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NULL`).Scan(&live); err != nil {
		return
	}
	if !s.vecAvailable {
		return
	}
	err = s.db.QueryRow(`
		SELECT COUNT(*) FROM nodes n
		JOIN node_embeddings e ON e.node_id = n.id
		WHERE n.archived_at IS NULL
	`).Scan(&covered)
	return
}

// NodeCounts returns the count of live and archived nodes.
func (s *Store) NodeCounts() (live, archived int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NULL`).Scan(&live); err != nil {
		return
	}
	err = s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE archived_at IS NOT NULL`).Scan(&archived)
	return
}

// EdgeCount returns the total number of edges.
func (s *Store) EdgeCount() (int, error) {
	var n int
	return n, s.db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n)
}

// AuditEntry is a single row from the audit_log table.
type AuditEntry struct {
	Action     string
	NodeLabel  string
	ActionedAt time.Time
}

// LastAuditEntry returns the most recent audit log entry.
// ok is false if the audit log is empty.
func (s *Store) LastAuditEntry() (entry AuditEntry, ok bool, err error) {
	err = s.db.QueryRow(
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
