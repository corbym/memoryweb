package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Node struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	WhyMatters  string     `json:"why_matters"`
	Tags        string     `json:"tags,omitempty"`
	Domain      string     `json:"domain"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	OccurredAt  *time.Time `json:"occurred_at,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
}

type Edge struct {
	ID           string    `json:"id"`
	FromNode     string    `json:"from_node"`
	ToNode       string    `json:"to_node"`
	Relationship string    `json:"relationship"`
	Narrative    string    `json:"narrative"`
	CreatedAt    time.Time `json:"created_at"`
}

type NodeWithEdges struct {
	Node  Node   `json:"node"`
	Edges []Edge `json:"edges"`
}

// DriftCandidate is a node flagged as potentially stale or conflicting.
type DriftCandidate struct {
	Node          Node   `json:"node"`
	ConflictsWith *Node  `json:"conflicts_with,omitempty"`
	Reason        string `json:"reason"`
}

// NodeInput is the input type for AddNodesBatch.
type NodeInput struct {
	Label       string
	Description string
	WhyMatters  string
	Tags        string
	Domain      string
	OccurredAt  *time.Time
}

// EdgeInput is the input type for AddEdgesBatch.
type EdgeInput struct {
	FromNode     string
	ToNode       string
	Relationship string
	Narrative    string
}

type DomainAlias struct {
	Alias     string    `json:"alias"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	return s, s.migrate()
}

func (s *Store) Close() {
	s.db.Close()
}

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

// ── domain aliases ────────────────────────────────────────────────────────────

// ResolveAlias returns the canonical domain for name, or name itself if no
// alias is registered.
func (s *Store) ResolveAlias(name string) string {
	var canonical string
	err := s.db.QueryRow(`SELECT domain FROM domain_aliases WHERE alias = ?`, name).Scan(&canonical)
	if err != nil {
		return name
	}
	return canonical
}

// AddAlias registers alias as an alternative name for domain.
func (s *Store) AddAlias(alias, domain string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		alias, domain, time.Now().UTC(),
	)
	return err
}

// ListAliases returns all registered domain aliases.
func (s *Store) ListAliases() ([]DomainAlias, error) {
	rows, err := s.db.Query(`SELECT alias, domain, created_at FROM domain_aliases ORDER BY alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DomainAlias
	for rows.Next() {
		var a DomainAlias
		rows.Scan(&a.Alias, &a.Domain, &a.CreatedAt)
		out = append(out, a)
	}
	return out, nil
}

// RemoveAlias deletes an alias. Returns an error if the alias does not exist.
func (s *Store) RemoveAlias(alias string) error {
	res, err := s.db.Exec(`DELETE FROM domain_aliases WHERE alias = ?`, alias)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("alias not found: %s", alias)
	}
	return nil
}

func (s *Store) AddNode(label, description, whyMatters, domain string, occurredAt *time.Time, tags string) (*Node, error) {
	id := slug(label) + "-" + shortID()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, label, description, whyMatters, domain, now, now, occurredAt, tags,
	)
	if err != nil {
		return nil, err
	}
	return &Node{
		ID:          id,
		Label:       label,
		Description: description,
		WhyMatters:  whyMatters,
		Tags:        tags,
		Domain:      domain,
		CreatedAt:   now,
		UpdatedAt:   now,
		OccurredAt:  occurredAt,
	}, nil
}

func (s *Store) AddEdge(fromID, toID, relationship, narrative string) (*Edge, error) {
	// Validate nodes exist
	for _, nodeID := range []string{fromID, toID} {
		var count int
		s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, nodeID).Scan(&count)
		if count == 0 {
			return nil, fmt.Errorf("node not found: %s", nodeID)
		}
	}
	id := "edge-" + shortID()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO edges (id, from_node, to_node, relationship, narrative, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, fromID, toID, relationship, narrative, now,
	)
	if err != nil {
		return nil, err
	}
	return &Edge{id, fromID, toID, relationship, narrative, now}, nil
}

func (s *Store) GetNode(id string) (*NodeWithEdges, error) {
	var n Node
	var oa sql.NullTime
	var aa sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	if err != nil {
		return nil, err
	}
	if oa.Valid {
		n.OccurredAt = &oa.Time
	}
	if aa.Valid {
		n.ArchivedAt = &aa.Time
	}

	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges
		 WHERE from_node = ? OR to_node = ?`, id, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt)
		edges = append(edges, e)
	}

	return &NodeWithEdges{Node: n, Edges: edges}, nil
}

type SearchResult struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

func (s *Store) SearchNodes(query, domain string, limit int) (*SearchResult, error) {
	domain = s.ResolveAlias(domain)
	q := "%" + query + "%"
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			domain, q, q, q, q, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			q, q, q, q, limit,
		)
	}
	if err != nil {
		return nil, err
	}

	nodes, err := scanNodeRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	// If the full-phrase LIKE returned nothing and the query contains multiple
	// words, fall back to an OR of individual-word LIKE terms so that nodes
	// whose fields collectively cover the query words are still surfaced.
	if len(nodes) == 0 {
		words := strings.Fields(query)
		if len(words) > 1 {
			log.Printf("[memoryweb] search: no results for %q (domain=%q), falling back to individual-word search", query, domain)
			nodes, err = s.searchByWords(words, domain, limit)
			if err != nil {
				return nil, err
			}
		}
	}

	// collect edges where both endpoints are in the result set
	var edges []Edge
	if len(nodes) > 1 {
		ids := make([]interface{}, len(nodes))
		for i, n := range nodes {
			ids[i] = n.ID
		}
		ph := make([]byte, 0, len(ids)*2)
		for i := range ids {
			if i > 0 {
				ph = append(ph, ',')
			}
			ph = append(ph, '?')
		}
		edgeQ := "SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges WHERE from_node IN (" +
			string(ph) + ") AND to_node IN (" + string(ph) + ")"
		eRows, err := s.db.Query(edgeQ, append(ids, ids...)...)
		if err != nil {
			return nil, err
		}
		defer eRows.Close()
		for eRows.Next() {
			var e Edge
			eRows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt)
			edges = append(edges, e)
		}
	}

	return &SearchResult{Nodes: nodes, Edges: edges}, nil
}

// scanNodeRows reads all node rows from rows into a slice.
// Caller is responsible for closing rows.
func scanNodeRows(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var n Node
		var oa sql.NullTime
		var aa sql.NullTime
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// searchByWords executes a fallback query that matches nodes containing ANY of
// the provided words in ANY of the searchable fields (label, description,
// why_matters, tags). Results are ordered by updated_at DESC.
func (s *Store) searchByWords(words []string, domain string, limit int) ([]Node, error) {
	// Build: (label LIKE ? OR desc LIKE ? OR why LIKE ? OR tags LIKE ?)
	//        OR (label LIKE ? OR ...)   ... one group per word.
	const fields = 4 // label, description, why_matters, tags
	wordClause := "(label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)"
	clauses := make([]string, len(words))
	for i := range words {
		clauses[i] = wordClause
	}
	combined := strings.Join(clauses, " OR ")

	args := []interface{}{}
	for _, w := range words {
		wq := "%" + w + "%"
		for j := 0; j < fields; j++ {
			args = append(args, wq)
		}
	}

	var q string
	if domain != "" {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
		     WHERE domain = ? AND archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		// domain goes first, limit last
		finalArgs := make([]interface{}, 0, 1+len(args)+1)
		finalArgs = append(finalArgs, domain)
		finalArgs = append(finalArgs, args...)
		finalArgs = append(finalArgs, limit)
		args = finalArgs
	} else {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
		     WHERE archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodeRows(rows)
}

type ConnectionResult struct {
	From  *Node  `json:"from"`
	To    *Node  `json:"to"`
	Edges []Edge `json:"edges"`
}

// bestMatch returns the first node whose label or description best matches the term.
func (s *Store) bestMatch(term, domain string) (*Node, error) {
	domain = s.ResolveAlias(domain)
	q := "%" + term + "%"
	var row *sql.Row
	if domain != "" {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			domain, q, q, q, q, q,
		)
	} else {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			q, q, q, q, q,
		)
	}
	var n Node
	var oa sql.NullTime
	var aa sql.NullTime
	err := row.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if oa.Valid {
		n.OccurredAt = &oa.Time
	}
	if aa.Valid {
		n.ArchivedAt = &aa.Time
	}
	return &n, nil
}

func (s *Store) FindConnections(fromTerm, toTerm, domain string) (*ConnectionResult, error) {
	from, err := s.bestMatch(fromTerm, domain)
	if err != nil {
		return nil, err
	}
	to, err := s.bestMatch(toTerm, domain)
	if err != nil {
		return nil, err
	}
	if from == nil || to == nil {
		return &ConnectionResult{From: from, To: to, Edges: nil}, nil
	}

	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges
		 WHERE (from_node = ? AND to_node = ?) OR (from_node = ? AND to_node = ?)`,
		from.ID, to.ID, to.ID, from.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt)
		edges = append(edges, e)
	}
	return &ConnectionResult{From: from, To: to, Edges: edges}, nil
}

func (s *Store) RecentChanges(domain string, limit int) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE domain = ? AND archived_at IS NULL ORDER BY updated_at DESC LIMIT ?`,
			domain, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE archived_at IS NULL ORDER BY updated_at DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var oa sql.NullTime
		var aa sql.NullTime
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// Timeline returns nodes ordered by occurred_at ASC where occurred_at is not null,
// optionally filtered by domain and date range.
func (s *Store) Timeline(domain string, from, to *time.Time, limit int) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	if limit <= 0 {
		limit = 20
	}

	conds := []string{"occurred_at IS NOT NULL", "archived_at IS NULL"}
	args := []interface{}{}

	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	if from != nil {
		conds = append(conds, "occurred_at >= ?")
		args = append(args, from)
	}
	if to != nil {
		conds = append(conds, "occurred_at <= ?")
		args = append(args, to)
	}
	args = append(args, limit)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY occurred_at ASC LIMIT ?"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var oa sql.NullTime
		var aa sql.NullTime
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// ── archive / restore ─────────────────────────────────────────────────────────

// ArchiveNode soft-deletes a node by setting archived_at and records an audit_log entry.
func (s *Store) ArchiveNode(id, reason string) error {
	now := time.Now().UTC()

	var label string
	if err := s.db.QueryRow(`SELECT label FROM nodes WHERE id = ?`, id).Scan(&label); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("node not found: %s", id)
		}
		return err
	}

	if _, err := s.db.Exec(`UPDATE nodes SET archived_at = ? WHERE id = ?`, now, id); err != nil {
		return err
	}

	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "archive", id, label, reason, now,
	)
	return err
}

// RestoreNode clears archived_at on a node and records an audit_log entry.
func (s *Store) RestoreNode(id string) error {
	now := time.Now().UTC()

	var label string
	if err := s.db.QueryRow(`SELECT label FROM nodes WHERE id = ?`, id).Scan(&label); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("node not found: %s", id)
		}
		return err
	}

	if _, err := s.db.Exec(`UPDATE nodes SET archived_at = NULL WHERE id = ?`, id); err != nil {
		return err
	}

	_, err := s.db.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "restore", id, label, nil, now,
	)
	return err
}

// ListArchived returns all archived nodes, optionally filtered by domain.
func (s *Store) ListArchived(domain string) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE archived_at IS NOT NULL AND domain = ? ORDER BY archived_at DESC`,
			domain,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags FROM nodes
			 WHERE archived_at IS NOT NULL ORDER BY archived_at DESC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var oa sql.NullTime
		var aa sql.NullTime
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags)
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// ── batch insert ──────────────────────────────────────────────────────────────

// AddNodesBatch inserts all nodes in a single transaction.
// If any node fails validation or insertion the transaction is rolled back.
func (s *Store) AddNodesBatch(inputs []NodeInput) ([]*Node, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nodes := make([]*Node, 0, len(inputs))
	for i, inp := range inputs {
		if inp.Label == "" {
			tx.Rollback()
			return nil, fmt.Errorf("node %d: label is required", i)
		}
		if inp.Domain == "" {
			tx.Rollback()
			return nil, fmt.Errorf("node %d: domain is required", i)
		}
		id := slug(inp.Label) + "-" + shortID()
		if _, err := tx.Exec(
			`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, inp.Label, inp.Description, inp.WhyMatters, inp.Domain, now, now, inp.OccurredAt, inp.Tags,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
		nodes = append(nodes, &Node{
			ID:          id,
			Label:       inp.Label,
			Description: inp.Description,
			WhyMatters:  inp.WhyMatters,
			Tags:        inp.Tags,
			Domain:      inp.Domain,
			CreatedAt:   now,
			UpdatedAt:   now,
			OccurredAt:  inp.OccurredAt,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// AddEdgesBatch inserts all edges in a single transaction.
// If any edge references a non-existent node the transaction is rolled back.
func (s *Store) AddEdgesBatch(inputs []EdgeInput) ([]*Edge, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	edges := make([]*Edge, 0, len(inputs))
	for _, inp := range inputs {
		for _, nodeID := range []string{inp.FromNode, inp.ToNode} {
			var count int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, nodeID).Scan(&count); err != nil {
				tx.Rollback()
				return nil, err
			}
			if count == 0 {
				tx.Rollback()
				return nil, fmt.Errorf("node not found: %s", nodeID)
			}
		}
		id := "edge-" + shortID()
		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_node, to_node, relationship, narrative, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			id, inp.FromNode, inp.ToNode, inp.Relationship, inp.Narrative, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
		edges = append(edges, &Edge{
			ID:           id,
			FromNode:     inp.FromNode,
			ToNode:       inp.ToNode,
			Relationship: inp.Relationship,
			Narrative:    inp.Narrative,
			CreatedAt:    now,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return edges, nil
}

// ── drift detection ───────────────────────────────────────────────────────────

// FindDrift returns nodes that may be stale, contradicted, or superseded.
// Rules are applied in order; the first match per node wins:
//  1. Contradiction: connected by a "contradicts" edge.
//  2. Superseded label: contains "old", "deprecated", "replaced", "legacy", "previous".
//  3. Stale open question: contains open-question keywords and is older than 30 days.
//  4. Duplicate label: identical lowercased label in the same domain.
func (s *Store) FindDrift(domain string, limit int) ([]DriftCandidate, error) {
	if limit <= 0 {
		limit = 10
	}
	domain = s.ResolveAlias(domain)

	var out []DriftCandidate
	seen := make(map[string]bool)

	add := func(n Node, cw *Node, reason string) {
		if !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, DriftCandidate{Node: n, ConflictsWith: cw, Reason: reason})
		}
	}

	// scanSingle scans 10 standard node columns from a *sql.Rows.
	scanSingle := func(r *sql.Rows) (Node, error) {
		var n Node
		var oa, aa sql.NullTime
		if err := r.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags); err != nil {
			return Node{}, err
		}
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		return n, nil
	}

	var err error

	// ── Rule 1: contradicts edges ─────────────────────────────────────────────
	rows, err := s.db.Query(`
		SELECT a.id, a.label, a.description, a.why_matters, a.domain,
		       a.created_at, a.updated_at, a.occurred_at, a.archived_at, a.tags,
		       b.id, b.label, b.description, b.why_matters, b.domain,
		       b.created_at, b.updated_at, b.occurred_at, b.archived_at, b.tags
		FROM edges e
		JOIN nodes a ON a.id = e.from_node AND a.archived_at IS NULL
		JOIN nodes b ON b.id = e.to_node   AND b.archived_at IS NULL
		WHERE e.relationship = 'contradicts'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			aID, aLabel, aDesc, aWhy, aDomain string
			aCreated, aUpdated                time.Time
			aOA, aAA                          sql.NullTime
			aTags                             string
			bID, bLabel, bDesc, bWhy, bDomain string
			bCreated, bUpdated                time.Time
			bOA, bAA                          sql.NullTime
			bTags                             string
		)
		if err := rows.Scan(
			&aID, &aLabel, &aDesc, &aWhy, &aDomain, &aCreated, &aUpdated, &aOA, &aAA, &aTags,
			&bID, &bLabel, &bDesc, &bWhy, &bDomain, &bCreated, &bUpdated, &bOA, &bAA, &bTags,
		); err != nil {
			rows.Close()
			return nil, err
		}
		a := Node{ID: aID, Label: aLabel, Description: aDesc, WhyMatters: aWhy, Domain: aDomain, CreatedAt: aCreated, UpdatedAt: aUpdated, Tags: aTags}
		if aOA.Valid {
			a.OccurredAt = &aOA.Time
		}
		if aAA.Valid {
			a.ArchivedAt = &aAA.Time
		}
		b := Node{ID: bID, Label: bLabel, Description: bDesc, WhyMatters: bWhy, Domain: bDomain, CreatedAt: bCreated, UpdatedAt: bUpdated, Tags: bTags}
		if bOA.Valid {
			b.OccurredAt = &bOA.Time
		}
		if bAA.Valid {
			b.ArchivedAt = &bAA.Time
		}
		if domain == "" || a.Domain == domain {
			bc := b
			add(a, &bc, "explicitly marked as contradicting each other")
		}
		if domain == "" || b.Domain == domain {
			ac := a
			add(b, &ac, "explicitly marked as contradicting each other")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// ── Rule 2: superseded labels ─────────────────────────────────────────────
	const supersededKW = `(LOWER(label) LIKE '%old%' OR LOWER(label) LIKE '%deprecated%' OR ` +
		`LOWER(label) LIKE '%replaced%' OR LOWER(label) LIKE '%legacy%' OR LOWER(label) LIKE '%previous%')`
	var rows2 *sql.Rows
	if domain != "" {
		rows2, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+supersededKW, domain)
	} else {
		rows2, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags ` +
				`FROM nodes WHERE archived_at IS NULL AND ` + supersededKW)
	}
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		n, err := scanSingle(rows2)
		if err != nil {
			rows2.Close()
			return nil, err
		}
		add(n, nil, "label suggests this may be superseded")
	}
	rows2.Close()
	if err = rows2.Err(); err != nil {
		return nil, err
	}

	// ── Rule 3: stale open questions ──────────────────────────────────────────
	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	const staleKW = `(LOWER(label) LIKE '%open question%' OR LOWER(label) LIKE '%unresolved%' OR ` +
		`LOWER(label) LIKE '%tbd%' OR LOWER(label) LIKE '%todo%' OR ` +
		`LOWER(description) LIKE '%open question%' OR LOWER(description) LIKE '%unresolved%' OR ` +
		`LOWER(description) LIKE '%tbd%' OR LOWER(description) LIKE '%todo%')`
	const ageFilter = `((occurred_at IS NOT NULL AND occurred_at < ?) OR (occurred_at IS NULL AND created_at < ?))`
	var rows3 *sql.Rows
	if domain != "" {
		rows3, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+staleKW+` AND `+ageFilter,
			domain, cutoff, cutoff)
	} else {
		rows3, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags `+
				`FROM nodes WHERE archived_at IS NULL AND `+staleKW+` AND `+ageFilter,
			cutoff, cutoff)
	}
	if err != nil {
		return nil, err
	}
	for rows3.Next() {
		n, err := scanSingle(rows3)
		if err != nil {
			rows3.Close()
			return nil, err
		}
		add(n, nil, "open question older than 30 days")
	}
	rows3.Close()
	if err = rows3.Err(); err != nil {
		return nil, err
	}

	// ── Rule 4: duplicate labels ──────────────────────────────────────────────
	const dupExists = `EXISTS (SELECT 1 FROM nodes n2 WHERE n2.archived_at IS NULL ` +
		`AND n2.domain = nodes.domain AND LOWER(n2.label) = LOWER(nodes.label) AND n2.id != nodes.id)`
	var rows4 *sql.Rows
	if domain != "" {
		rows4, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+dupExists, domain)
	} else {
		rows4, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags ` +
				`FROM nodes WHERE archived_at IS NULL AND ` + dupExists)
	}
	if err != nil {
		return nil, err
	}
	for rows4.Next() {
		n, err := scanSingle(rows4)
		if err != nil {
			rows4.Close()
			return nil, err
		}
		add(n, nil, "possible duplicate of newer node")
	}
	rows4.Close()
	if err = rows4.Err(); err != nil {
		return nil, err
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ── update ────────────────────────────────────────────────────────────────────

// UpdateNode merges the provided (non-nil) fields into an existing live node.
// Writes an audit_log entry recording which fields changed and their old values.
// Returns the full updated node. Returns an error if the node does not exist or
// has been archived.
func (s *Store) UpdateNode(id string, label, description, whyMatters, tags *string) (*Node, error) {
	// Fetch current values for comparison and audit trail.
	var cur Node
	var curOA, curAA sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&cur.ID, &cur.Label, &cur.Description, &cur.WhyMatters, &cur.Domain,
		&cur.CreatedAt, &cur.UpdatedAt, &curOA, &curAA, &cur.Tags); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", id)
		}
		return nil, err
	}

	now := time.Now().UTC()
	sets := []string{"updated_at = ?"}
	args := []interface{}{now}

	// Build audit reason describing each changed field with its old value.
	var changes []string

	if label != nil {
		sets = append(sets, "label = ?")
		args = append(args, *label)
		if *label != cur.Label {
			changes = append(changes, fmt.Sprintf("label (was %q)", cur.Label))
		}
	}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *description)
		if *description != cur.Description {
			changes = append(changes, fmt.Sprintf("description (was %q)", cur.Description))
		}
	}
	if whyMatters != nil {
		sets = append(sets, "why_matters = ?")
		args = append(args, *whyMatters)
		if *whyMatters != cur.WhyMatters {
			changes = append(changes, fmt.Sprintf("why_matters (was %q)", cur.WhyMatters))
		}
	}
	if tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, *tags)
		if *tags != cur.Tags {
			changes = append(changes, fmt.Sprintf("tags (was %q)", cur.Tags))
		}
	}
	args = append(args, id)

	if _, err := s.db.Exec(
		`UPDATE nodes SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
		args...,
	); err != nil {
		return nil, err
	}

	// Write audit log entry.
	reason := "no fields changed"
	if len(changes) > 0 {
		reason = "changed: " + strings.Join(changes, "; ")
	}
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "update", id, cur.Label, reason, now,
	); err != nil {
		return nil, err
	}

	// Re-fetch the updated node.
	var n Node
	var oa, aa sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags
		 FROM nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags); err != nil {
		return nil, err
	}
	if oa.Valid {
		n.OccurredAt = &oa.Time
	}
	if aa.Valid {
		n.ArchivedAt = &aa.Time
	}
	return &n, nil
}
