package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

// ── migrations ────────────────────────────────────────────────────────────────

// migration is a single versioned schema change.
type migration struct {
	version int
	desc    string
	up      func(tx *sql.Tx) error
}

// migrations is the ordered, append-only list of all schema changes.
// Never edit an existing entry — only add new ones at the end.
var migrations = []migration{
	{
		version: 1,
		desc:    "initial schema: nodes, edges, indexes",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS nodes (
					id          TEXT PRIMARY KEY,
					label       TEXT NOT NULL,
					description TEXT NOT NULL DEFAULT '',
					why_matters TEXT NOT NULL DEFAULT '',
					domain      TEXT NOT NULL DEFAULT '',
					created_at  DATETIME NOT NULL,
					updated_at  DATETIME NOT NULL
				);
				CREATE TABLE IF NOT EXISTS edges (
					id           TEXT PRIMARY KEY,
					from_node    TEXT NOT NULL,
					to_node      TEXT NOT NULL,
					relationship TEXT NOT NULL,
					narrative    TEXT NOT NULL DEFAULT '',
					created_at   DATETIME NOT NULL,
					FOREIGN KEY(from_node) REFERENCES nodes(id),
					FOREIGN KEY(to_node)   REFERENCES nodes(id)
				);
				CREATE INDEX IF NOT EXISTS idx_nodes_domain    ON nodes(domain);
				CREATE INDEX IF NOT EXISTS idx_nodes_updated   ON nodes(updated_at);
				CREATE INDEX IF NOT EXISTS idx_edges_from_node ON edges(from_node);
				CREATE INDEX IF NOT EXISTS idx_edges_to_node   ON edges(to_node);
			`)
			return err
		},
	},
	{
		version: 2,
		desc:    "nodes: add occurred_at column and index",
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE nodes ADD COLUMN occurred_at DATETIME`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_occurred ON nodes(occurred_at)`)
			return err
		},
	},
	{
		version: 3,
		desc:    "add domain_aliases table",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS domain_aliases (
					alias      TEXT PRIMARY KEY,
					domain     TEXT NOT NULL,
					created_at DATETIME NOT NULL
				)
			`)
			return err
		},
	},
	{
		version: 4,
		desc:    "nodes: add archived_at column and index",
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE nodes ADD COLUMN archived_at DATETIME`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_archived ON nodes(archived_at)`)
			return err
		},
	},
	{
		version: 5,
		desc:    "add audit_log table",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				CREATE TABLE IF NOT EXISTS audit_log (
					id          TEXT PRIMARY KEY,
					action      TEXT NOT NULL,
					node_id     TEXT NOT NULL,
					node_label  TEXT NOT NULL,
					reason      TEXT,
					actioned_at DATETIME NOT NULL
				)
			`)
			return err
		},
	},
	{
		version: 6,
		desc:    "nodes: add tags column and index",
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE nodes ADD COLUMN tags TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_tags ON nodes(tags)`)
			return err
		},
	},
	{
		version: 7,
		desc:    "nodes: add transient column",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE nodes ADD COLUMN transient INTEGER NOT NULL DEFAULT 0`)
			return err
		},
	},
	{
		version: 8,
		desc:    "add node_embeddings virtual table (sqlite-vec)",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS node_embeddings USING vec0(
				node_id   TEXT PRIMARY KEY,
				embedding FLOAT[384]
			)`)
			// 384 dimensions matches the snowflake-arctic-embed model output size.
			if err != nil {
				// sqlite-vec extension may not be available in all build environments.
				log.Printf("[memoryweb] note: could not create node_embeddings table (sqlite-vec may not be loaded): %v", err)
				return nil
			}
			return nil
		},
	},
	{
		version: 9,
		desc:    "resize node_embeddings to 1024 dimensions (snowflake-arctic-embed default model)",
		up: func(tx *sql.Tx) error {
			// The default Ollama snowflake-arctic-embed model returns 1024-dimensional
			// vectors, not 384. Drop and recreate; any stored embeddings are invalid
			// anyway since they could not have been inserted into the 384-dim table.
			if _, err := tx.Exec(`DROP TABLE IF EXISTS node_embeddings`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS node_embeddings USING vec0(
				node_id   TEXT PRIMARY KEY,
				embedding FLOAT[1024]
			)`)
			if err != nil {
				log.Printf("[memoryweb] note: could not recreate node_embeddings table (sqlite-vec may not be loaded): %v", err)
				return nil
			}
			return nil
		},
	},
	{
		version: 10,
		desc:    "audit_log: add provenance column",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE audit_log ADD COLUMN provenance TEXT`)
			return err
		},
	},
	{
		version: 11,
		desc:    "add significance_log table",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS significance_log (
    id          TEXT PRIMARY KEY,
    call_id     TEXT NOT NULL,
    called_at   DATETIME NOT NULL,
    domain      TEXT NOT NULL,
    limit_n     INTEGER NOT NULL,
    node_id     TEXT NOT NULL,
    node_label  TEXT NOT NULL,
    rank_type   TEXT NOT NULL,
    score       REAL
);
CREATE INDEX IF NOT EXISTS idx_significance_log_domain  ON significance_log(domain);
CREATE INDEX IF NOT EXISTS idx_significance_log_node    ON significance_log(node_id);
CREATE INDEX IF NOT EXISTS idx_significance_log_call_id ON significance_log(call_id);
`)
			return err
		},
	},
	{
		version: 12,
		desc:    "nodes: add decision_type TEXT column; migrate transient=1 to decision_type=transient; drop transient column",
		up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE nodes ADD COLUMN decision_type TEXT NOT NULL DEFAULT 'decision'`); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE nodes SET decision_type = 'transient' WHERE transient = 1`); err != nil {
				return err
			}
			_, err := tx.Exec(`ALTER TABLE nodes DROP COLUMN transient`)
			return err
		},
	},
	{
		version: 13,
		desc:    "nodes: rename decision_type column to node_kind",
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE nodes RENAME COLUMN decision_type TO node_kind`)
			return err
		},
	},
}

// migrate creates the schema_migrations tracking table (if needed) then applies
// any unapplied migrations in version order inside individual transactions.
func (s *Store) migrate() error {
	// Check whether schema_migrations already exists before we create it.
	var migrationsTableExisted int
	s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`,
	).Scan(&migrationsTableExisted)

	// Bootstrap: ensure schema_migrations exists.
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			desc       TEXT NOT NULL,
			applied_at DATETIME NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("bootstrap schema_migrations: %w", err)
	}

	// If schema_migrations did not exist before this call AND the nodes table
	// already exists, this is a pre-versioning DB being upgraded.
	// Stamp all known migrations as applied so we don't re-run them.
	if migrationsTableExisted == 0 {
		var nodesExists int
		s.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='nodes'`,
		).Scan(&nodesExists)
		if nodesExists > 0 {
			now := time.Now().UTC()
			for _, m := range migrations {
				if _, err := s.db.Exec(
					`INSERT OR IGNORE INTO schema_migrations (version, desc, applied_at) VALUES (?, ?, ?)`,
					m.version, m.desc, now,
				); err != nil {
					return fmt.Errorf("stamp migration v%d: %w", m.version, err)
				}
			}
			return nil
		}
	}

	for _, m := range migrations {
		var count int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version,
		).Scan(&count); err != nil {
			return fmt.Errorf("migration v%d: check: %w", m.version, err)
		}
		if count > 0 {
			continue // already applied
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("migration v%d: begin tx: %w", m.version, err)
		}
		if err := m.up(tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d (%s): %w", m.version, m.desc, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, desc, applied_at) VALUES (?, ?, ?)`,
			m.version, m.desc, time.Now().UTC(),
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d: record: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration v%d: commit: %w", m.version, err)
		}
	}
	return nil
}
