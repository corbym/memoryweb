package db

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
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
	ArchivedAt  *time.Time `json:"archived_at,omitempty"` // nil = live
	Transient   bool       `json:"transient,omitempty"`   // true = expected to become stale quickly
}

type Edge struct {
	ID           string    `json:"id"`
	FromNode     string    `json:"from_memory"`
	ToNode       string    `json:"to_memory"`
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
	EdgeCount     int    `json:"edge_count"` // total edges (from + to) incident to this node
}

// NodeInput is the input type for AddNodesBatch.
type NodeInput struct {
	Label       string
	Description string
	WhyMatters  string
	Tags        string
	Domain      string
	OccurredAt  *time.Time
	Transient   bool
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

// ollamaEmbedRequest is the JSON body for the Ollama /api/embed endpoint.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// ollamaEmbedResponse is the JSON response from the Ollama /api/embed endpoint.
type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

const ollamaModel = "snowflake-arctic-embed"
const ollamaEndpoint = "http://localhost:11434/api/embed"

// embed calls the local Ollama instance to generate an embedding for the
// given text using the snowflake-arctic-embed model. Returns nil if Ollama is
// not running or the model is unavailable — callers must treat nil as a signal
// to fall back to literal LIKE search.
//
// The endpoint may be overridden by MEMORYWEB_OLLAMA_ENDPOINT. Set it to
// "disabled" to make embed always fail, which is useful in tests that
// exercise LIKE search behaviour in isolation from Ollama.
func embed(text string) ([]float32, error) {
	endpoint := ollamaEndpoint
	if v := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT"); v != "" {
		if v == "disabled" {
			return nil, fmt.Errorf("embedding disabled by MEMORYWEB_OLLAMA_ENDPOINT")
		}
		endpoint = v
	}
	body, err := json.Marshal(ollamaEmbedRequest{Model: ollamaModel, Input: text})
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, raw)
	}

	var result ollamaEmbedResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama embed: empty embedding returned")
	}
	return result.Embeddings[0], nil
}

// Embed is the exported form of embed, used by external tools such as
// the embeddings backfill command.
func Embed(text string) ([]float32, error) {
	return embed(text)
}

// storeEmbedding inserts or replaces the embedding for a node in the
// node_embeddings virtual table. Returns true if the embedding was stored
// successfully. A failure only degrades search quality, not correctness.
func (s *Store) storeEmbedding(id string, embedding []float32) bool {
	if !s.vecAvailable || len(embedding) == 0 {
		return false
	}
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		log.Printf("[memoryweb] serialize embedding for %s: %v", id, err)
		return false
	}
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO node_embeddings(node_id, embedding) VALUES (?, ?)`,
		id, blob,
	); err != nil {
		log.Printf("[memoryweb] store embedding for %s: %v", id, err)
		return false
	}
	return true
}

// BackfillEmbeddings generates and stores embeddings for all live nodes that
// do not yet have one. Returns the count of embeddings successfully written.
// Requires Ollama to be running with the snowflake-arctic-embed model.
// progress is called after each successful embedding with (done, total);
// pass nil to disable progress reporting.
func (s *Store) BackfillEmbeddings(progress func(done, total int)) (int, error) {
	if !s.vecAvailable {
		return 0, fmt.Errorf("sqlite-vec not available; cannot backfill embeddings")
	}
	rows, err := s.db.Query(`
		SELECT n.id, n.label, n.description, n.why_matters
		FROM nodes n
		LEFT JOIN node_embeddings e ON e.node_id = n.id
		WHERE n.archived_at IS NULL AND e.node_id IS NULL
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type candidate struct {
		id, label, description, whyMatters string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.label, &c.description, &c.whyMatters); err != nil {
			return 0, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	n := 0
	for i, c := range candidates {
		text := c.label + " " + c.description + " " + c.whyMatters
		embedding, err := embed(text)
		if progress != nil {
			progress(i+1, len(candidates))
		}
		if err != nil {
			// Only log when there is no progress callback — if one is present,
			// the caller is rendering a progress bar and individual error lines
			// would corrupt it. The summary already conveys how many succeeded.
			if progress == nil {
				log.Printf("[memoryweb] backfill embed %s: %v", c.id, err)
			}
			continue
		}
		if s.storeEmbedding(c.id, embedding) {
			n++
		}
	}
	return n, nil
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

func (s *Store) AddNode(label, description, whyMatters, domain string, occurredAt *time.Time, tags string, transient bool) (*Node, error) {
	id := slug(label) + "-" + shortID()
	now := time.Now().UTC()

	// Atomically insert the node and (when occurred_at is set) its audit row.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags, transient)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, label, description, whyMatters, domain, now, now, occurredAt, tags, transient,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if occurredAt != nil {
		provenance := "agent-assigned"
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "occurred_at_set", id, label, provenance, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Generate and store an embedding for semantic search (best-effort, after commit).
	if embedding, err := embed(label + " " + description + " " + whyMatters); err == nil {
		s.storeEmbedding(id, embedding)
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
		Transient:   transient,
	}, nil
}

func (s *Store) AddEdge(fromID, toID, relationship, narrative string) (*Edge, error) {
	// Look up from node and get its domain.
	var fromDomain string
	if err := s.db.QueryRow(`SELECT domain FROM nodes WHERE id = ? AND archived_at IS NULL`, fromID).Scan(&fromDomain); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", fromID)
		}
		return nil, err
	}
	// Check to node exists.
	var toCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ? AND archived_at IS NULL`, toID).Scan(&toCount)
	if toCount == 0 {
		return nil, fmt.Errorf("node not found: %q was not found (searched domain %q). If this node is in a different domain, cross-domain connections are not yet supported — search for it first to confirm its domain", toID, fromDomain)
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
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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

// NodeResult is a single search result. SemanticDistance is set when the
// result was matched by vector-distance search; it is nil for LIKE results.
type NodeResult struct {
	Node
	SemanticDistance *float64 `json:"semantic_distance,omitempty"`
}

type SearchResult struct {
	Nodes     []NodeResult `json:"nodes"`
	Edges     []Edge       `json:"edges"`
	Truncated bool         `json:"truncated,omitempty"`
}

func (s *Store) SearchNodes(query, domain string, limit int) (*SearchResult, error) {
	domain = s.ResolveAlias(domain)

	// Try semantic search when sqlite-vec is loaded.
	if s.vecAvailable {
		embedding, err := embed(query)
		if err == nil && len(embedding) > 0 {
			result, err := s.searchNodesSemantic(query, domain, limit, embedding)
			if err == nil {
				return result, nil
			}
			log.Printf("[memoryweb] semantic search failed: %v; falling back to text search", err)
		}
	}

	return s.searchNodesLike(query, domain, limit)
}

// semanticDistanceThreshold is the maximum cosine distance for a node to be
// considered a semantic match. vec_distance_cosine returns values in [0, 2];
// 0 = identical, 2 = opposite. Results beyond this threshold are discarded
// and the LIKE fallback runs instead.
const semanticDistanceThreshold = 0.3

// searchNodesSemantic ranks nodes by cosine distance between the query
// embedding and stored node embeddings, then falls back to LIKE if no
// semantic results are found within the relevance threshold.
func (s *Store) searchNodesSemantic(query, domain string, limit int, embedding []float32) (*SearchResult, error) {
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, err
	}

	// Fetch one extra row so we can detect truncation without a separate COUNT
	// query. The threshold check still cuts off results beyond semanticDistanceThreshold.
	fetch := limit + 1

	var rows *sql.Rows
	if domain != "" {
		rows, err = s.db.Query(`
			SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.transient,
			       vec_distance_cosine(e.embedding, ?) AS dist
			FROM node_embeddings e
			JOIN nodes n ON n.id = e.node_id
			WHERE n.archived_at IS NULL AND n.domain = ?
			ORDER BY dist ASC
			LIMIT ?`,
			blob, domain, fetch)
	} else {
		rows, err = s.db.Query(`
			SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.transient,
			       vec_distance_cosine(e.embedding, ?) AS dist
			FROM node_embeddings e
			JOIN nodes n ON n.id = e.node_id
			WHERE n.archived_at IS NULL
			ORDER BY dist ASC
			LIMIT ?`,
			blob, fetch)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []NodeResult
	for rows.Next() {
		var n Node
		var occurredAt, archivedAt sql.NullTime
		var dist float64
		if err := rows.Scan(
			&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &occurredAt, &archivedAt, &n.Tags, &n.Transient,
			&dist,
		); err != nil {
			return nil, err
		}
		// Results are ordered by distance ASC; stop as soon as we exceed the threshold.
		if dist > semanticDistanceThreshold {
			break
		}
		if occurredAt.Valid {
			n.OccurredAt = &occurredAt.Time
		}
		if archivedAt.Valid {
			n.ArchivedAt = &archivedAt.Time
		}
		d := dist // copy for pointer stability
		results = append(results, NodeResult{Node: n, SemanticDistance: &d})
	}

	if len(results) == 0 {
		// No embeddings within threshold; fall back to literal search.
		return s.searchNodesLike(query, domain, limit)
	}

	truncated := len(results) > limit
	if truncated {
		results = results[:limit]
	}

	nodes := extractNodes(results)
	return &SearchResult{Nodes: results, Edges: collectEdges(s.db, nodes), Truncated: truncated}, nil
}

// searchNodesLike performs a full-phrase LIKE search with a multi-word fallback.
func (s *Store) searchNodesLike(query, domain string, limit int) (*SearchResult, error) {
	q := "%" + query + "%"
	var rows *sql.Rows
	var err error

	// Fetch one extra row to detect truncation without a separate COUNT query.
	fetch := limit + 1

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			domain, q, q, q, q, fetch,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			q, q, q, q, fetch,
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

	truncated := len(nodes) > limit
	if truncated {
		nodes = nodes[:limit]
	}

	// If the full-phrase LIKE returned nothing and the query contains multiple
	// words, fall back to an OR of individual-word LIKE terms so that nodes
	// whose fields collectively cover the query words are still surfaced.
	if len(nodes) == 0 && !truncated {
		words := strings.Fields(query)
		if len(words) > 1 {
			log.Printf("[memoryweb] search: no results for %q (domain=%q), falling back to individual-word search", query, domain)
			var wordTruncated bool
			nodes, wordTruncated, err = s.searchByWords(words, domain, limit)
			if err != nil {
				return nil, err
			}
			truncated = wordTruncated
		}
	}

	results := wrapNodes(nodes)
	return &SearchResult{Nodes: results, Edges: collectEdges(s.db, nodes), Truncated: truncated}, nil
}

// extractNodes extracts the embedded Node from each NodeResult.
func extractNodes(nrs []NodeResult) []Node {
	ns := make([]Node, len(nrs))
	for i, nr := range nrs {
		ns[i] = nr.Node
	}
	return ns
}

// wrapNodes wraps []Node into []NodeResult with nil SemanticDistance (LIKE results).
func wrapNodes(nodes []Node) []NodeResult {
	nrs := make([]NodeResult, len(nodes))
	for i, n := range nodes {
		nrs[i] = NodeResult{Node: n}
	}
	return nrs
}

// collectEdges returns edges whose both endpoints appear in nodes.
func collectEdges(db *sql.DB, nodes []Node) []Edge {
	if len(nodes) <= 1 {
		return nil
	}
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
	eRows, err := db.Query(edgeQ, append(ids, ids...)...)
	if err != nil {
		return nil
	}
	defer eRows.Close()
	var edges []Edge
	for eRows.Next() {
		var e Edge
		if err := eRows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt); err != nil {
			log.Printf("[memoryweb] collectEdges scan: %v", err)
			continue
		}
		edges = append(edges, e)
	}
	return edges
}

// scanNodeRows reads all node rows from rows into a slice.
// Caller is responsible for closing rows.
func scanNodeRows(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var n Node
		var oa sql.NullTime
		var aa sql.NullTime
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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

// PathResult holds the shortest path between two nodes and all edges
// incident to any node on that path (spine edges + context branches).
type PathResult struct {
	Path  []Node `json:"path"`
	Edges []Edge `json:"edges"`
}

// FindPath returns the shortest path between fromID and toID using a BFS
// traversal of edges. Only live (non-archived) nodes are traversed; archived
// nodes act as walls. maxDepth caps the search at that many hops (hard limit: 6).
// Returns an empty PathResult (no error) when no path exists.
func (s *Store) FindPath(fromID, toID string, maxDepth int) (*PathResult, error) {
	if maxDepth <= 0 || maxDepth > 6 {
		maxDepth = 6
	}
	if fromID == toID {
		// Trivial: source == destination.
		n, err := s.GetNode(fromID)
		if err != nil {
			return nil, err
		}
		return &PathResult{Path: []Node{n.Node}, Edges: nil}, nil
	}

	// BFS: each entry is a path (slice of node IDs) from fromID to the frontier.
	type path struct {
		nodes []string
		edges []string // edge IDs in order
	}
	queue := []path{{nodes: []string{fromID}}}
	visited := map[string]bool{fromID: true}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		tail := cur.nodes[len(cur.nodes)-1]
		if len(cur.nodes)-1 >= maxDepth {
			continue // depth limit reached without finding target
		}

		// Fetch all edges from or to the current tail node (undirected traversal).
		rows, err := s.db.Query(`
			SELECT e.id, e.from_node, e.to_node, e.relationship, e.narrative, e.created_at
			FROM edges e
			JOIN nodes nf ON nf.id = e.from_node AND nf.archived_at IS NULL
			JOIN nodes nt ON nt.id = e.to_node   AND nt.archived_at IS NULL
			WHERE e.from_node = ? OR e.to_node = ?`, tail, tail)
		if err != nil {
			return nil, err
		}
		var neighbours []struct {
			edge      Edge
			neighbour string
		}
		for rows.Next() {
			var e Edge
			if err := rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			next := e.ToNode
			if next == tail {
				next = e.FromNode
			}
			neighbours = append(neighbours, struct {
				edge      Edge
				neighbour string
			}{e, next})
		}
		rows.Close()

		for _, nb := range neighbours {
			if visited[nb.neighbour] {
				continue
			}
			newPath := path{
				nodes: append(append([]string{}, cur.nodes...), nb.neighbour),
				edges: append(append([]string{}, cur.edges...), nb.edge.ID),
			}
			if nb.neighbour == toID {
				// Found it — materialise the result.
				return s.materialisePath(newPath.nodes, newPath.edges)
			}
			visited[nb.neighbour] = true
			queue = append(queue, newPath)
		}
	}
	return &PathResult{}, nil // no path found
}

// materialisePath fetches full Node structs for the path and all edges
// incident to any node on the path (spine edges + context branches).
func (s *Store) materialisePath(nodeIDs, edgeIDs []string) (*PathResult, error) {
	_ = edgeIDs // we now fetch all incident edges instead of just spine edges
	nodes := make([]Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nwe, err := s.GetNode(id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, nwe.Node)
	}

	// Build placeholder list for IN clause.
	placeholders := make([]string, len(nodeIDs))
	args := make([]interface{}, len(nodeIDs)*2)
	for i, id := range nodeIDs {
		placeholders[i] = "?"
		args[i] = id
		args[len(nodeIDs)+i] = id
	}
	ph := strings.Join(placeholders, ", ")

	rows, err := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges
		 WHERE from_node IN (`+ph+`) OR to_node IN (`+ph+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &PathResult{Path: nodes, Edges: edges}, nil
}

// searchByWords executes a fallback query that matches nodes containing ANY of
// the provided words in ANY of the searchable fields (label, description,
// why_matters, tags). Results are ordered by updated_at DESC.
// Returns the matching nodes and a truncated flag (true when the result set
// was capped at limit).
func (s *Store) searchByWords(words []string, domain string, limit int) ([]Node, bool, error) {
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

	// Fetch one extra row to detect truncation.
	fetch := limit + 1

	var q string
	if domain != "" {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
		     WHERE domain = ? AND archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		// domain goes first, limit last
		finalArgs := make([]interface{}, 0, 1+len(args)+1)
		finalArgs = append(finalArgs, domain)
		finalArgs = append(finalArgs, args...)
		finalArgs = append(finalArgs, fetch)
		args = finalArgs
	} else {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
		     WHERE archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		args = append(args, fetch)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	nodes, err := scanNodeRows(rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(nodes) > limit
	if truncated {
		nodes = nodes[:limit]
	}
	return nodes, truncated, nil
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			domain, q, q, q, q, q,
		)
	} else {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			q, q, q, q, q,
		)
	}
	var n Node
	var oa sql.NullTime
	var aa sql.NullTime
	err := row.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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

// CountNodes returns the number of live (non-archived) nodes in a domain.
func (s *Store) CountNodes(domain string) (int, error) {
	domain = s.ResolveAlias(domain)
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`,
		domain,
	).Scan(&count)
	return count, err
}

func (s *Store) RecentChanges(domain string, limit int) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE domain = ? AND archived_at IS NULL ORDER BY updated_at DESC LIMIT ?`,
			domain, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
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
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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

// Timeline returns nodes ordered by COALESCE(occurred_at, created_at) ASC.
// When importantOnly is true, only nodes with occurred_at explicitly set are returned.
// tags filters to nodes matching at least one tag (whole-word match).
// from/to filter by effective date (COALESCE(occurred_at, created_at)).
func (s *Store) Timeline(domain string, importantOnly bool, tags []string, from, to *time.Time, limit int) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	if limit <= 0 {
		limit = 20
	}

	conds := []string{"archived_at IS NULL"}
	args := []interface{}{}

	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	if importantOnly {
		conds = append(conds, "occurred_at IS NOT NULL")
	}
	if from != nil {
		conds = append(conds, "COALESCE(occurred_at, created_at) >= ?")
		args = append(args, from)
	}
	if to != nil {
		conds = append(conds, "COALESCE(occurred_at, created_at) <= ?")
		args = append(args, to)
	}
	for _, tag := range tags {
		conds = append(conds,
			"(tags = ? OR tags LIKE ? || ' %' OR tags LIKE '% ' || ? OR tags LIKE '% ' || ? || ' %')")
		args = append(args, tag, tag, tag, tag)
	}
	args = append(args, limit)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY COALESCE(occurred_at, created_at) ASC LIMIT ?"

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
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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

// ArchiveNodesBatch archives multiple nodes in a single transaction.
// If any node ID does not exist, the whole transaction is rolled back and an
// error is returned — no nodes are archived on partial failure.
func (s *Store) ArchiveNodesBatch(items []struct{ ID, Reason string }) error {
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, item := range items {
		var label string
		if err := tx.QueryRow(`SELECT label FROM nodes WHERE id = ?`, item.ID).Scan(&label); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("node not found: %s", item.ID)
			}
			return err
		}
		if _, err := tx.Exec(`UPDATE nodes SET archived_at = ? WHERE id = ?`, now, item.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "archive", item.ID, label, item.Reason, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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

// PurgeResult holds the outcome of a Purge call.
type PurgeResult struct {
	Nodes      []Node // nodes that were (or would be) purged
	TotalEdges int    // total edges deleted (0 in dry-run)
}

// Purge hard-deletes archived nodes from the database.
// domain and before are optional filters. When dryRun is true the database is
// not modified and only the candidate list is returned.
func (s *Store) Purge(domain string, before *time.Time, dryRun bool) (PurgeResult, error) {
	if domain != "" {
		domain = s.ResolveAlias(domain)
	}

	conds := []string{"archived_at IS NOT NULL"}
	args := []interface{}{}

	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	if before != nil {
		conds = append(conds, "archived_at < ?")
		args = append(args, before.UTC())
	}

	query := "SELECT id, label, archived_at FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY archived_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("query archived nodes: %w", err)
	}

	type candidate struct {
		id         string
		label      string
		archivedAt time.Time
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		var at sql.NullTime
		if err := rows.Scan(&c.id, &c.label, &at); err != nil {
			rows.Close()
			return PurgeResult{}, fmt.Errorf("scan: %w", err)
		}
		if at.Valid {
			c.archivedAt = at.Time
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	// Build the Node slice (minimal fields) for the caller.
	result := PurgeResult{}
	for _, c := range candidates {
		at := c.archivedAt
		result.Nodes = append(result.Nodes, Node{
			ID:         c.id,
			Label:      c.label,
			ArchivedAt: &at,
		})
	}

	if dryRun || len(candidates) == 0 {
		return result, nil
	}

	// Hard-delete inside a transaction.
	tx, err := s.db.Begin()
	if err != nil {
		return PurgeResult{}, fmt.Errorf("begin transaction: %w", err)
	}

	now := time.Now().UTC()
	for _, c := range candidates {
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "purge", c.id, c.label, nil, now,
		); err != nil {
			tx.Rollback()
			return PurgeResult{}, fmt.Errorf("write audit_log for %s: %w", c.id, err)
		}

		res, err := tx.Exec(`DELETE FROM edges WHERE from_node = ? OR to_node = ?`, c.id, c.id)
		if err != nil {
			tx.Rollback()
			return PurgeResult{}, fmt.Errorf("delete edges for %s: %w", c.id, err)
		}
		deleted, _ := res.RowsAffected()
		result.TotalEdges += int(deleted)

		if _, err := tx.Exec(`DELETE FROM nodes WHERE id = ?`, c.id); err != nil {
			tx.Rollback()
			return PurgeResult{}, fmt.Errorf("delete node %s: %w", c.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return PurgeResult{}, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

// ListArchived returns all archived nodes, optionally filtered by domain.
func (s *Store) ListArchived(domain string) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
			 WHERE archived_at IS NOT NULL AND domain = ? ORDER BY archived_at DESC`,
			domain,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient FROM nodes
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
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient)
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
			`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, tags, transient)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, inp.Label, inp.Description, inp.WhyMatters, inp.Domain, now, now, inp.OccurredAt, inp.Tags, inp.Transient,
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
			Transient:   inp.Transient,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Audit occurred_at provenance for any nodes where it was set (best-effort, after commit).
	for _, n := range nodes {
		if n.OccurredAt != nil {
			now2 := time.Now().UTC()
			provenance := "agent-assigned"
			_, _ = s.db.Exec(
				`INSERT INTO audit_log (id, action, node_id, node_label, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
				"auditlog-"+shortID(), "occurred_at_set", n.ID, n.Label, provenance, now2,
			)
		}
	}

	// Generate and store embeddings for each node (best-effort, after commit).
	for _, n := range nodes {
		text := n.Label + " " + n.Description + " " + n.WhyMatters
		if embedding, err := embed(text); err == nil {
			s.storeEmbedding(n.ID, embedding)
		}
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
//  5. Transient node older than 7 days.
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

	// scanSingle scans 11 standard node columns from a *sql.Rows.
	scanSingle := func(r *sql.Rows) (Node, error) {
		var n Node
		var oa, aa sql.NullTime
		if err := r.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); err != nil {
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
		       a.created_at, a.updated_at, a.occurred_at, a.archived_at, a.tags, a.transient,
		       b.id, b.label, b.description, b.why_matters, b.domain,
		       b.created_at, b.updated_at, b.occurred_at, b.archived_at, b.tags, b.transient
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
			aTransient                        bool
			bID, bLabel, bDesc, bWhy, bDomain string
			bCreated, bUpdated                time.Time
			bOA, bAA                          sql.NullTime
			bTags                             string
			bTransient                        bool
		)
		if err := rows.Scan(
			&aID, &aLabel, &aDesc, &aWhy, &aDomain, &aCreated, &aUpdated, &aOA, &aAA, &aTags, &aTransient,
			&bID, &bLabel, &bDesc, &bWhy, &bDomain, &bCreated, &bUpdated, &bOA, &bAA, &bTags, &bTransient,
		); err != nil {
			rows.Close()
			return nil, err
		}
		a := Node{ID: aID, Label: aLabel, Description: aDesc, WhyMatters: aWhy, Domain: aDomain, CreatedAt: aCreated, UpdatedAt: aUpdated, Tags: aTags, Transient: aTransient}
		if aOA.Valid {
			a.OccurredAt = &aOA.Time
		}
		if aAA.Valid {
			a.ArchivedAt = &aAA.Time
		}
		b := Node{ID: bID, Label: bLabel, Description: bDesc, WhyMatters: bWhy, Domain: bDomain, CreatedAt: bCreated, UpdatedAt: bUpdated, Tags: bTags, Transient: bTransient}
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+supersededKW, domain)
	} else {
		rows2, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient ` +
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
	cutoff30 := time.Now().UTC().AddDate(0, 0, -30)
	const staleKW = `(LOWER(label) LIKE '%open question%' OR LOWER(label) LIKE '%unresolved%' OR ` +
		`LOWER(label) LIKE '%tbd%' OR LOWER(label) LIKE '%todo%' OR ` +
		`LOWER(description) LIKE '%open question%' OR LOWER(description) LIKE '%unresolved%' OR ` +
		`LOWER(description) LIKE '%tbd%' OR LOWER(description) LIKE '%todo%')`
	const ageFilter = `((occurred_at IS NOT NULL AND occurred_at < ?) OR (occurred_at IS NULL AND created_at < ?))`
	var rows3 *sql.Rows
	if domain != "" {
		rows3, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+staleKW+` AND `+ageFilter,
			domain, cutoff30, cutoff30)
	} else {
		rows3, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND `+staleKW+` AND `+ageFilter,
			cutoff30, cutoff30)
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+dupExists, domain)
	} else {
		rows4, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient ` +
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

	// ── Rule 5: transient nodes older than 7 days ─────────────────────────────
	cutoff7 := time.Now().UTC().AddDate(0, 0, -7)
	var rows5 *sql.Rows
	if domain != "" {
		rows5, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND transient = 1 AND created_at < ?`,
			domain, cutoff7)
	} else {
		rows5, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient `+
				`FROM nodes WHERE archived_at IS NULL AND transient = 1 AND created_at < ?`,
			cutoff7)
	}
	if err != nil {
		return nil, err
	}
	for rows5.Next() {
		n, err := scanSingle(rows5)
		if err != nil {
			rows5.Close()
			return nil, err
		}
		add(n, nil, "transient node older than 7 days — consider archiving once the related work is complete")
	}
	rows5.Close()
	if err = rows5.Err(); err != nil {
		return nil, err
	}

	if len(out) > limit {
		out = out[:limit]
	}

	// Enrich each candidate with its total edge count (from + to).
	if len(out) > 0 {
		ids := make([]string, len(out))
		for i, c := range out {
			ids[i] = c.Node.ID
		}
		ph := strings.Repeat("?,", len(ids))
		ph = ph[:len(ph)-1]
		args := make([]interface{}, len(ids)*2)
		for i, id := range ids {
			args[i] = id
			args[len(ids)+i] = id
		}
		ecRows, ecErr := s.db.Query(
			`SELECT id_val, COUNT(*) FROM (`+
				`SELECT from_node AS id_val FROM edges WHERE from_node IN (`+ph+`) `+
				`UNION ALL `+
				`SELECT to_node AS id_val FROM edges WHERE to_node IN (`+ph+`)`+
				`) GROUP BY id_val`,
			args...,
		)
		if ecErr == nil {
			counts := make(map[string]int)
			for ecRows.Next() {
				var id string
				var cnt int
				if ecRows.Scan(&id, &cnt) == nil {
					counts[id] = cnt
				}
			}
			ecRows.Close()
			for i := range out {
				out[i].EdgeCount = counts[out[i].Node.ID]
			}
		}
	}

	return out, nil
}

// GetDomainGraph returns the live nodes and the edges between them for a domain.
// Nodes are sorted by edge count descending so the most-connected appear first;
// the result is capped at limit (default 40, max 100). truncated is true when
// the full node set was larger than limit. nodesTotal is the full domain node
// count before any truncation; edgesTotal is the count of intra-domain edges
// across all nodes (not just the shown subset).
func (s *Store) GetDomainGraph(domain string, limit int) (nodes []Node, edges []Edge, truncated bool, nodesTotal int, edgesTotal int, err error) {
	domain = s.ResolveAlias(domain)
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}

	// Step 1: all live nodes in the domain.
	rows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE archived_at IS NULL AND domain = ?`, domain)
	if err != nil {
		return
	}
	var allNodes []Node
	for rows.Next() {
		var n Node
		var oa, aa sql.NullTime
		if scanErr := rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); scanErr != nil {
			rows.Close()
			err = scanErr
			return
		}
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		allNodes = append(allNodes, n)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return
	}
	if len(allNodes) == 0 {
		return
	}

	// Step 2: count edges (from + to) per node to rank by connectivity.
	ids := make([]string, len(allNodes))
	for i, n := range allNodes {
		ids[i] = n.ID
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	rankArgs := make([]interface{}, len(ids)*2)
	for i, id := range ids {
		rankArgs[i] = id
		rankArgs[len(ids)+i] = id
	}
	ecRows, ecErr := s.db.Query(
		`SELECT id_val, COUNT(*) FROM (`+
			`SELECT from_node AS id_val FROM edges WHERE from_node IN (`+ph+`) `+
			`UNION ALL `+
			`SELECT to_node AS id_val FROM edges WHERE to_node IN (`+ph+`)`+
			`) GROUP BY id_val`,
		rankArgs...,
	)
	counts := make(map[string]int)
	if ecErr == nil {
		for ecRows.Next() {
			var id string
			var cnt int
			if ecRows.Scan(&id, &cnt) == nil {
				counts[id] = cnt
			}
		}
		ecRows.Close()
	}

	// Step 3: record totals before any truncation, then sort and truncate.
	nodesTotal = len(allNodes)
	// Count intra-domain edges (both endpoints in domain) across the full node set.
	if nodesTotal > 0 {
		_ = s.db.QueryRow(
			`SELECT COUNT(*) FROM edges WHERE from_node IN (`+ph+`) AND to_node IN (`+ph+`)`,
			rankArgs...,
		).Scan(&edgesTotal)
	}

	sort.Slice(allNodes, func(i, j int) bool {
		return counts[allNodes[i].ID] > counts[allNodes[j].ID]
	})
	if len(allNodes) > limit {
		allNodes = allNodes[:limit]
		truncated = true
	}
	nodes = allNodes

	// Step 4: fetch edges whose both endpoints are in the result set.
	edgeArgs := make([]interface{}, len(nodes)*2)
	ph2 := strings.Repeat("?,", len(nodes))
	ph2 = ph2[:len(ph2)-1]
	for i, n := range nodes {
		edgeArgs[i] = n.ID
		edgeArgs[len(nodes)+i] = n.ID
	}
	eRows, eErr := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges `+
			`WHERE from_node IN (`+ph2+`) AND to_node IN (`+ph2+`)`,
		edgeArgs...,
	)
	if eErr != nil {
		err = eErr
		return
	}
	for eRows.Next() {
		var e Edge
		if eRows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt) == nil {
			edges = append(edges, e)
		}
	}
	eRows.Close()
	err = eRows.Err()
	return
}

// ── update ────────────────────────────────────────────────────────────────────

// UpdateNode merges the provided (non-nil) fields into an existing live node.
// Writes an audit_log entry recording which fields changed and their old values.
// Returns the full updated node. Returns an error if the node does not exist or
// has been archived.
func (s *Store) UpdateNode(id string, label, description, whyMatters, tags *string, occurredAt *time.Time) (*Node, error) {
	// Fetch current values for comparison and audit trail.
	var cur Node
	var curOA, curAA sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&cur.ID, &cur.Label, &cur.Description, &cur.WhyMatters, &cur.Domain,
		&cur.CreatedAt, &cur.UpdatedAt, &curOA, &curAA, &cur.Tags, &cur.Transient); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", id)
		}
		return nil, err
	}
	if curOA.Valid {
		cur.OccurredAt = &curOA.Time
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
	if occurredAt != nil {
		sets = append(sets, "occurred_at = ?")
		args = append(args, *occurredAt)
		oldVal := "(none)"
		if cur.OccurredAt != nil {
			oldVal = cur.OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		changes = append(changes, fmt.Sprintf("occurred_at (was %s)", oldVal))
	}
	args = append(args, id)

	reason := "no fields changed"
	if len(changes) > 0 {
		reason = "changed: " + strings.Join(changes, "; ")
	}
	var provenance *string
	if occurredAt != nil {
		p := "agent-assigned"
		provenance = &p
	}

	// Atomically update the node and write the audit row in a single transaction.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`UPDATE nodes SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
		args...,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO audit_log (id, action, node_id, node_label, reason, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"auditlog-"+shortID(), "update", id, cur.Label, reason, provenance, now,
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Re-fetch the updated node.
	var n Node
	var oa, aa sql.NullTime
	if err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); err != nil {
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

// NodeUpdateInput is a single entry in an UpdateNodesBatch call.
type NodeUpdateInput struct {
	ID          string
	Label       *string
	Description *string
	WhyMatters  *string
	Tags        *string
	OccurredAt  *time.Time
}

// UpdateNodesBatch updates multiple nodes in a single transaction.
// All updates succeed or all are rolled back.
func (s *Store) UpdateNodesBatch(inputs []NodeUpdateInput) ([]*Node, error) {
	if len(inputs) == 0 {
		return []*Node{}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nodes := make([]*Node, 0, len(inputs))

	for _, inp := range inputs {
		var cur Node
		var curOA, curAA sql.NullTime
		if err := tx.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
			 FROM nodes WHERE id = ? AND archived_at IS NULL`, inp.ID,
		).Scan(&cur.ID, &cur.Label, &cur.Description, &cur.WhyMatters, &cur.Domain,
			&cur.CreatedAt, &cur.UpdatedAt, &curOA, &curAA, &cur.Tags, &cur.Transient); err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("node not found: %s", inp.ID)
			}
			return nil, err
		}
		if curOA.Valid {
			cur.OccurredAt = &curOA.Time
		}

		sets := []string{"updated_at = ?"}
		args := []interface{}{now}
		var changes []string

		if inp.Label != nil {
			sets = append(sets, "label = ?")
			args = append(args, *inp.Label)
			if *inp.Label != cur.Label {
				changes = append(changes, fmt.Sprintf("label (was %q)", cur.Label))
			}
		}
		if inp.Description != nil {
			sets = append(sets, "description = ?")
			args = append(args, *inp.Description)
			if *inp.Description != cur.Description {
				changes = append(changes, fmt.Sprintf("description (was %q)", cur.Description))
			}
		}
		if inp.WhyMatters != nil {
			sets = append(sets, "why_matters = ?")
			args = append(args, *inp.WhyMatters)
			if *inp.WhyMatters != cur.WhyMatters {
				changes = append(changes, fmt.Sprintf("why_matters (was %q)", cur.WhyMatters))
			}
		}
		if inp.Tags != nil {
			sets = append(sets, "tags = ?")
			args = append(args, *inp.Tags)
			if *inp.Tags != cur.Tags {
				changes = append(changes, fmt.Sprintf("tags (was %q)", cur.Tags))
			}
		}
		if inp.OccurredAt != nil {
			sets = append(sets, "occurred_at = ?")
			args = append(args, *inp.OccurredAt)
			oldVal := "(none)"
			if cur.OccurredAt != nil {
				oldVal = cur.OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			changes = append(changes, fmt.Sprintf("occurred_at (was %s)", oldVal))
		}
		args = append(args, inp.ID)

		if _, err := tx.Exec(
			`UPDATE nodes SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
			args...,
		); err != nil {
			tx.Rollback()
			return nil, err
		}

		reason := "no fields changed"
		if len(changes) > 0 {
			reason = "changed: " + strings.Join(changes, "; ")
		}
		var batchProvenance *string
		if inp.OccurredAt != nil {
			p := "agent-assigned"
			batchProvenance = &p
		}
		if _, err := tx.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, provenance, actioned_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"auditlog-"+shortID(), "update", inp.ID, cur.Label, reason, batchProvenance, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}

		// Re-fetch within the tx.
		var n Node
		var oa, aa sql.NullTime
		if err := tx.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
			 FROM nodes WHERE id = ?`, inp.ID,
		).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); err != nil {
			tx.Rollback()
			return nil, err
		}
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		nodes = append(nodes, &n)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// ── edge suggestions ──────────────────────────────────────────────────────────

// EdgeSuggestion is a candidate connection returned by SuggestEdges.
type EdgeSuggestion struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
	Domain string `json:"domain"`
}

// SuggestEdges returns up to limit candidate connections for the given node:
// live nodes in the same domain that share tags or significant label words.
// It never creates edges — the caller must use AddEdge to act on suggestions.
func (s *Store) SuggestEdges(id string, limit int) ([]EdgeSuggestion, error) {
	if limit <= 0 {
		limit = 5
	}

	// Fetch the target node.
	var targetLabel, targetDomain, targetTags string
	if err := s.db.QueryRow(
		`SELECT label, domain, tags FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&targetLabel, &targetDomain, &targetTags); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", id)
		}
		return nil, err
	}

	// Extract meaningful keywords from label + tags (lowercased, deduplicated,
	// stop-words and very short words removed).
	keywords := suggestKeywords(targetLabel, targetTags)
	if len(keywords) == 0 {
		return []EdgeSuggestion{}, nil
	}

	// Fetch all other live nodes in the same domain (cap at 200 to bound work).
	rows, err := s.db.Query(
		`SELECT id, label, tags FROM nodes
		 WHERE id != ? AND domain = ? AND archived_at IS NULL
		 ORDER BY updated_at DESC LIMIT 200`,
		id, targetDomain,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		id     string
		label  string
		score  int
		reason string
	}

	var candidates []scored
	for rows.Next() {
		var cid, clabel, ctags string
		rows.Scan(&cid, &clabel, &ctags)

		cLabelLower := strings.ToLower(clabel)
		cTagsLower := strings.ToLower(ctags)

		var matchedTags, matchedLabels []string
		seen := map[string]bool{}
		for _, kw := range keywords {
			if seen[kw] {
				continue
			}
			if strings.Contains(cTagsLower, kw) {
				matchedTags = append(matchedTags, kw)
				seen[kw] = true
			} else if strings.Contains(cLabelLower, kw) {
				matchedLabels = append(matchedLabels, kw)
				seen[kw] = true
			}
		}

		score := len(matchedTags)*2 + len(matchedLabels)
		if score == 0 {
			continue
		}

		var reasons []string
		if len(matchedTags) > 0 {
			reasons = append(reasons, "shares tags: "+strings.Join(matchedTags, " "))
		}
		if len(matchedLabels) > 0 {
			reasons = append(reasons, "similar label words: "+strings.Join(matchedLabels, " "))
		}
		candidates = append(candidates, scored{
			id:     cid,
			label:  clabel,
			score:  score,
			reason: strings.Join(reasons, "; "),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result := make([]EdgeSuggestion, len(candidates))
	for i, c := range candidates {
		result[i] = EdgeSuggestion{ID: c.id, Label: c.label, Reason: c.reason, Domain: targetDomain}
	}
	return result, nil
}

// suggestKeywords extracts lowercase, deduplicated, meaningful words from label
// and tags, skipping common stop words and words shorter than 3 characters.
func suggestKeywords(label, tags string) []string {
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"of": true, "in": true, "to": true, "is": true, "it": true,
		"be": true, "for": true, "on": true, "at": true, "by": true,
		"we": true, "as": true, "so": true, "do": true, "not": true,
		"are": true, "was": true, "has": true, "had": true, "its": true,
	}
	seen := map[string]bool{}
	var keywords []string
	addWords := func(text string) {
		for _, w := range strings.Fields(strings.ToLower(text)) {
			w = strings.Trim(w, ".,!?;:-\"'()")
			if len(w) < 3 || stopWords[w] || seen[w] {
				continue
			}
			seen[w] = true
			keywords = append(keywords, w)
		}
	}
	addWords(tags) // tags first — higher signal
	addWords(label)
	return keywords
}

// ── list domains ──────────────────────────────────────────────────────────────

// ListDomains returns all distinct domains that have at least one live node,
// sorted alphabetically.
// RenameDomainResult holds the output of RenameDomain.
type RenameDomainResult struct {
	NodesRenamed int
	OldDomain    string
	NewDomain    string
}

// RenameDomain renames all live nodes in oldDomain to newDomain, then inserts
// a domain alias from oldDomain → newDomain so cached references continue to
// resolve. Both the UPDATE and alias INSERT are performed in a single
// transaction.
//
// Returns an error if:
//   - oldDomain has no live nodes (not found)
//   - newDomain already has live nodes (caller should use MergeDomains instead)
func (s *Store) RenameDomain(oldDomain, newDomain string) (*RenameDomainResult, error) {
	var oldCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, oldDomain,
	).Scan(&oldCount); err != nil {
		return nil, err
	}
	if oldCount == 0 {
		return nil, fmt.Errorf("domain %q has no live nodes", oldDomain)
	}

	var newCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, newDomain,
	).Scan(&newCount); err != nil {
		return nil, err
	}
	if newCount > 0 {
		return nil, fmt.Errorf("domain %q already has live nodes — use merge_domains instead", newDomain)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE nodes SET domain = ? WHERE domain = ?`, newDomain, oldDomain)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		oldDomain, newDomain, time.Now().UTC(),
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RenameDomainResult{NodesRenamed: int(n), OldDomain: oldDomain, NewDomain: newDomain}, nil
}

// MergeDomainsResult holds the output of MergeDomains.
type MergeDomainsResult struct {
	NodesMoved      int
	SourceDomain    string
	TargetDomain    string
	LabelCollisions []string
}

// MergeDomains moves all live nodes from sourceDomain into targetDomain, then
// inserts a domain alias from sourceDomain → targetDomain. Both the UPDATE and
// alias INSERT are performed in a single transaction.
//
// When dryRun is true, no writes are performed; the result describes what
// would happen.
//
// Returns an error if:
//   - sourceDomain has no live nodes (not found)
//   - targetDomain has no live nodes (caller should use RenameDomain instead)
func (s *Store) MergeDomains(sourceDomain, targetDomain string, dryRun bool) (*MergeDomainsResult, error) {
	var srcCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, sourceDomain,
	).Scan(&srcCount); err != nil {
		return nil, err
	}
	if srcCount == 0 {
		return nil, fmt.Errorf("source domain %q has no live nodes", sourceDomain)
	}

	var tgtCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM nodes WHERE domain = ? AND archived_at IS NULL`, targetDomain,
	).Scan(&tgtCount); err != nil {
		return nil, err
	}
	if tgtCount == 0 {
		return nil, fmt.Errorf("target domain %q has no live nodes — use rename_domain instead", targetDomain)
	}

	// Detect label collisions before any write.
	colRows, err := s.db.Query(`
		SELECT s.label FROM nodes s
		JOIN nodes t ON LOWER(s.label) = LOWER(t.label)
		WHERE s.domain = ? AND s.archived_at IS NULL
		  AND t.domain = ? AND t.archived_at IS NULL
	`, sourceDomain, targetDomain)
	if err != nil {
		return nil, err
	}
	var collisions []string
	for colRows.Next() {
		var label string
		if err := colRows.Scan(&label); err != nil {
			colRows.Close()
			return nil, err
		}
		collisions = append(collisions, label)
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	if dryRun {
		return &MergeDomainsResult{
			NodesMoved:      srcCount,
			SourceDomain:    sourceDomain,
			TargetDomain:    targetDomain,
			LabelCollisions: collisions,
		}, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE nodes SET domain = ? WHERE domain = ?`, targetDomain, sourceDomain)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO domain_aliases (alias, domain, created_at) VALUES (?, ?, ?)`,
		sourceDomain, targetDomain, time.Now().UTC(),
	); err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MergeDomainsResult{
		NodesMoved:      int(n),
		SourceDomain:    sourceDomain,
		TargetDomain:    targetDomain,
		LabelCollisions: collisions,
	}, nil
}

func (s *Store) ListDomains() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT domain FROM nodes WHERE archived_at IS NULL ORDER BY domain ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}

// ── disconnect ────────────────────────────────────────────────────────────────

// DeleteEdge hard-deletes an edge by ID. Returns an error if the edge does not exist.
func (s *Store) DeleteEdge(id string) error {
	res, err := s.db.Exec(`DELETE FROM edges WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("edge not found: %s", id)
	}
	return nil
}

// ── possible duplicates ───────────────────────────────────────────────────────

// FindPossibleDuplicates returns live nodes in the same domain whose normalised
// label closely matches the given label (lowercased, punctuation stripped).
// The node with the given excludeID is excluded (used to avoid self-match).
func (s *Store) FindPossibleDuplicates(label, domain, excludeID string) ([]Node, error) {
	norm := normaliseLabel(label)
	if norm == "" {
		return []Node{}, nil
	}
	rows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE domain = ? AND archived_at IS NULL AND id != ?`,
		domain, excludeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []Node
	for rows.Next() {
		var n Node
		var oa, aa sql.NullTime
		if err := rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); err != nil {
			return nil, err
		}
		if oa.Valid {
			n.OccurredAt = &oa.Time
		}
		if aa.Valid {
			n.ArchivedAt = &aa.Time
		}
		if normaliseLabel(n.Label) == norm {
			results = append(results, n)
		}
	}
	if results == nil {
		results = []Node{}
	}
	return results, nil
}

// normaliseLabel lowercases a label and strips non-alphanumeric characters
// (except spaces) so "Boot Crash!" and "boot crash" compare equal.
func normaliseLabel(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
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

// FindDisconnected returns live, non-transient nodes that have no edges// (neither as from_node nor as to_node), optionally scoped to a domain.
func (s *Store) FindDisconnected(domain string) ([]Node, error) {
	domain = s.ResolveAlias(domain)

	const baseQ = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		FROM nodes
		WHERE archived_at IS NULL
		  AND transient = 0
		  AND id NOT IN (SELECT from_node FROM edges UNION SELECT to_node FROM edges)`

	var (
		rows *sql.Rows
		err  error
	)
	if domain != "" {
		rows, err = s.db.Query(baseQ+` AND domain = ? ORDER BY created_at DESC LIMIT 50`, domain)
	} else {
		rows, err = s.db.Query(baseQ + ` ORDER BY created_at DESC LIMIT 50`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var oa, aa sql.NullTime
		if err := rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters,
			&n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.Transient); err != nil {
			return nil, err
		}
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

// GetNodeNeighbourhood returns the target node, all live nodes directly
// connected to it (depth 1), and all edges between those nodes.
// Returns an error if the node does not exist or is archived.
func (s *Store) GetNodeNeighbourhood(nodeID string) (nodes []Node, edges []Edge, err error) {
	var target Node
	var oa, aa sql.NullTime
	err = s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, nodeID,
	).Scan(&target.ID, &target.Label, &target.Description, &target.WhyMatters, &target.Domain,
		&target.CreatedAt, &target.UpdatedAt, &oa, &aa, &target.Tags, &target.Transient)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return nil, nil, err
	}
	if oa.Valid {
		target.OccurredAt = &oa.Time
	}
	if aa.Valid {
		target.ArchivedAt = &aa.Time
	}

	// Collect IDs of all direct neighbours via edges.
	eRows, err := s.db.Query(
		`SELECT CASE WHEN from_node = ? THEN to_node ELSE from_node END AS neighbour_id
		 FROM edges WHERE from_node = ? OR to_node = ?`, nodeID, nodeID, nodeID)
	if err != nil {
		return nil, nil, err
	}
	neighbourIDs := map[string]bool{}
	for eRows.Next() {
		var id string
		if scanErr := eRows.Scan(&id); scanErr != nil {
			eRows.Close()
			return nil, nil, scanErr
		}
		neighbourIDs[id] = true
	}
	eRows.Close()
	if err = eRows.Err(); err != nil {
		return nil, nil, err
	}

	// Build the full neighbourhood ID list (target + neighbours).
	allIDs := make([]string, 0, len(neighbourIDs)+1)
	allIDs = append(allIDs, nodeID)
	for id := range neighbourIDs {
		allIDs = append(allIDs, id)
	}

	// Fetch all live nodes in the neighbourhood.
	ph := strings.Repeat("?,", len(allIDs))
	ph = ph[:len(ph)-1]
	nArgs := make([]interface{}, len(allIDs))
	for i, id := range allIDs {
		nArgs[i] = id
	}
	nRows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, transient
		 FROM nodes WHERE archived_at IS NULL AND id IN (`+ph+`)`, nArgs...)
	if err != nil {
		return nil, nil, err
	}
	for nRows.Next() {
		var n Node
		var oa2, aa2 sql.NullTime
		if scanErr := nRows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa2, &aa2, &n.Tags, &n.Transient); scanErr != nil {
			nRows.Close()
			return nil, nil, scanErr
		}
		if oa2.Valid {
			n.OccurredAt = &oa2.Time
		}
		if aa2.Valid {
			n.ArchivedAt = &aa2.Time
		}
		nodes = append(nodes, n)
	}
	nRows.Close()
	if err = nRows.Err(); err != nil {
		return nil, nil, err
	}

	// Fetch all edges where both endpoints are in the live neighbourhood set.
	eArgs := make([]interface{}, len(allIDs)*2)
	for i, id := range allIDs {
		eArgs[i] = id
		eArgs[len(allIDs)+i] = id
	}
	edgeRows, err := s.db.Query(
		`SELECT id, from_node, to_node, relationship, narrative, created_at
		 FROM edges WHERE from_node IN (`+ph+`) AND to_node IN (`+ph+`)`, eArgs...)
	if err != nil {
		return nil, nil, err
	}
	for edgeRows.Next() {
		var e Edge
		if scanErr := edgeRows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt); scanErr != nil {
			edgeRows.Close()
			return nil, nil, scanErr
		}
		edges = append(edges, e)
	}
	edgeRows.Close()
	err = edgeRows.Err()
	return
}

// ── GetSignificance ───────────────────────────────────────────────────────────

// ScoredNode is a Node decorated with a structural importance score.
type ScoredNode struct {
	Node
	ImportanceScore float64 `json:"importance_score"`
}

// SignificanceResult holds the four sections returned by GetSignificance.
type SignificanceResult struct {
	Declared         []Node       `json:"declared"`
	Structural       []ScoredNode `json:"structural"`
	Uncurated        []ScoredNode `json:"uncurated"`
	PotentiallyStale []Node       `json:"potentially_stale"`
	CallID           string       `json:"call_id"`
}

// GetSignificance returns a dual-signal importance analysis for a domain.
//
//   - declared:          live nodes with occurred_at set, ordered by occurred_at ASC.
//   - structural:        live nodes ranked by weighted inbound degree (decay by linker age),
//     capped at limit, ordered by importance_score DESC.
//   - uncurated:         structural top-N nodes that have no occurred_at.
//   - potentially_stale: declared nodes whose ID does not appear in structural top-N.
//
// Every call writes rows to significance_log (one per returned node in
// structural, uncurated, potentially_stale) so the decay function can be
// validated over time.
func (s *Store) GetSignificance(domain string, limit int, recencyWindowDays int) (SignificanceResult, error) {
	callID := shortID()
	var res SignificanceResult
	res.CallID = callID

	// ── declared ─────────────────────────────────────────────────────────────
	declaredRows, err := s.db.Query(`
		SELECT id, label, description, why_matters, tags, domain,
		       created_at, updated_at, occurred_at, archived_at, transient
		FROM nodes
		WHERE domain = ?
		  AND occurred_at IS NOT NULL
		  AND archived_at IS NULL
		ORDER BY occurred_at ASC`, domain)
	if err != nil {
		return res, fmt.Errorf("GetSignificance declared: %w", err)
	}
	defer declaredRows.Close()
	for declaredRows.Next() {
		n, err := scanNode(declaredRows)
		if err != nil {
			return res, fmt.Errorf("GetSignificance scan declared: %w", err)
		}
		res.Declared = append(res.Declared, n)
	}
	if err := declaredRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance declared rows: %w", err)
	}
	if res.Declared == nil {
		res.Declared = []Node{}
	}

	// ── structural ────────────────────────────────────────────────────────────
	structRows, err := s.db.Query(`
		SELECT n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
		       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.transient,
		       SUM(1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS importance_score
		FROM edges e
		JOIN nodes n  ON e.to_node   = n.id
		JOIN nodes n2 ON e.from_node = n2.id
		WHERE n.domain = ?
		  AND n.archived_at IS NULL
		  AND n2.archived_at IS NULL
		  AND (julianday('now') - julianday(n2.updated_at)) <= ?
		GROUP BY n.id
		ORDER BY importance_score DESC
		LIMIT ?`, domain, recencyWindowDays, limit)
	if err != nil {
		return res, fmt.Errorf("GetSignificance structural: %w", err)
	}
	defer structRows.Close()
	structIDs := map[string]bool{}
	for structRows.Next() {
		var sn ScoredNode
		var tags, desc, why sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var transient int
		if err := structRows.Scan(
			&sn.ID, &sn.Label, &desc, &why, &tags, &sn.Domain,
			&sn.CreatedAt, &sn.UpdatedAt, &occurredAt, &archivedAt, &transient,
			&sn.ImportanceScore,
		); err != nil {
			return res, fmt.Errorf("GetSignificance scan structural: %w", err)
		}
		sn.Description = desc.String
		sn.WhyMatters = why.String
		sn.Tags = tags.String
		if occurredAt.Valid {
			sn.OccurredAt = &occurredAt.Time
		}
		if archivedAt.Valid {
			sn.ArchivedAt = &archivedAt.Time
		}
		sn.Transient = transient != 0
		res.Structural = append(res.Structural, sn)
		structIDs[sn.ID] = true
	}
	if err := structRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance structural rows: %w", err)
	}
	if res.Structural == nil {
		res.Structural = []ScoredNode{}
	}

	// ── uncurated: structural top-N with no occurred_at ───────────────────────
	for _, sn := range res.Structural {
		if sn.OccurredAt == nil {
			res.Uncurated = append(res.Uncurated, sn)
		}
	}
	if res.Uncurated == nil {
		res.Uncurated = []ScoredNode{}
	}

	// ── potentially_stale: declared but not in structural ─────────────────────
	for _, n := range res.Declared {
		if !structIDs[n.ID] {
			res.PotentiallyStale = append(res.PotentiallyStale, n)
		}
	}
	if res.PotentiallyStale == nil {
		res.PotentiallyStale = []Node{}
	}

	// ── log ───────────────────────────────────────────────────────────────────
	calledAt := time.Now().UTC()
	logged := map[string]bool{}
	for _, sn := range res.Structural {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, sn.ID, sn.Label, "structural", &sn.ImportanceScore); err != nil {
				return res, fmt.Errorf("GetSignificance log structural: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, sn := range res.Uncurated {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, sn.ID, sn.Label, "uncurated", nil); err != nil {
				return res, fmt.Errorf("GetSignificance log uncurated: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, n := range res.PotentiallyStale {
		if !logged[n.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, n.ID, n.Label, "potentially_stale", nil); err != nil {
				return res, fmt.Errorf("GetSignificance log potentially_stale: %w", err)
			}
			logged[n.ID] = true
		}
	}

	return res, nil
}

// logSignificance inserts one row into significance_log.
func (s *Store) logSignificance(callID string, calledAt time.Time, domain string, limitN int, nodeID, nodeLabel, rankType string, score *float64) error {
	id := shortID()
	_, err := s.db.Exec(
		`INSERT INTO significance_log (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, callID, calledAt, domain, limitN, nodeID, nodeLabel, rankType, score,
	)
	return err
}

// scanNode scans a single row from a query that SELECTs the standard 11 node
// columns in the order: id, label, description, why_matters, tags, domain,
// created_at, updated_at, occurred_at, archived_at, transient.
func scanNode(rows *sql.Rows) (Node, error) {
	var n Node
	var desc, why, tags sql.NullString
	var occurredAt, archivedAt sql.NullTime
	var transient int
	if err := rows.Scan(
		&n.ID, &n.Label, &desc, &why, &tags, &n.Domain,
		&n.CreatedAt, &n.UpdatedAt, &occurredAt, &archivedAt, &transient,
	); err != nil {
		return n, err
	}
	n.Description = desc.String
	n.WhyMatters = why.String
	n.Tags = tags.String
	if occurredAt.Valid {
		n.OccurredAt = &occurredAt.Time
	}
	if archivedAt.Valid {
		n.ArchivedAt = &archivedAt.Time
	}
	n.Transient = transient != 0
	return n, nil
}
