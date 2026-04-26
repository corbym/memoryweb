package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Node struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	WhyMatters  string    `json:"why_matters"`
	Domain      string    `json:"domain"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
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
}

func (s *Store) AddNode(label, description, whyMatters, domain string) (*Node, error) {
	id := slug(label) + "-" + shortID()
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, label, description, why_matters, domain, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, label, description, whyMatters, domain, now, now,
	)
	if err != nil {
		return nil, err
	}
	return &Node{id, label, description, whyMatters, domain, now, now}, nil
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
	err := s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	if err != nil {
		return nil, err
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
	q := "%" + query + "%"
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 WHERE domain = ? AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			domain, q, q, q, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 WHERE label LIKE ? OR description LIKE ? OR why_matters LIKE ?
			 ORDER BY updated_at DESC LIMIT ?`,
			q, q, q, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt)
		nodes = append(nodes, n)
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

type ConnectionResult struct {
	From  *Node  `json:"from"`
	To    *Node  `json:"to"`
	Edges []Edge `json:"edges"`
}

// bestMatch returns the first node whose label or description best matches the term.
func (s *Store) bestMatch(term, domain string) (*Node, error) {
	q := "%" + term + "%"
	var row *sql.Row
	if domain != "" {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 WHERE domain = ? AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			domain, q, q, q, q,
		)
	} else {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 WHERE label LIKE ? OR description LIKE ? OR why_matters LIKE ?
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			q, q, q, q,
		)
	}
	var n Node
	err := row.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
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
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 WHERE domain = ? ORDER BY updated_at DESC LIMIT ?`,
			domain, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at FROM nodes
			 ORDER BY updated_at DESC LIMIT ?`,
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
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt)
		nodes = append(nodes, n)
	}
	return nodes, nil
}
