package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

type Edge struct {
	ID           string    `json:"id"`
	FromNode     string    `json:"from_memory"`
	ToNode       string    `json:"to_memory"`
	Relationship string    `json:"relationship"`
	Narrative    string    `json:"narrative"`
	CreatedAt    time.Time `json:"created_at"`
}

// EdgeInput is the input type for AddEdgesBatch.
type EdgeInput struct {
	FromNode     string
	ToNode       string
	Relationship string
	Narrative    string
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
	// Check to node exists (live only).
	var toCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ? AND archived_at IS NULL`, toID).Scan(&toCount)
	if toCount == 0 {
		// Distinguish: archived vs. genuinely missing.
		var archivedCount int
		s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ? AND archived_at IS NOT NULL`, toID).Scan(&archivedCount)
		if archivedCount > 0 {
			return nil, fmt.Errorf("memory archived; use restore first (id %q)", toID)
		}
		return nil, fmt.Errorf("memory not found: %q — verify the ID with recall or search (filing domain was %q)", toID, fromDomain)
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

// AddEdgesBatch inserts all edges in a single transaction.
// If any edge references a non-existent or archived node the transaction is rolled back.
func (s *Store) AddEdgesBatch(inputs []EdgeInput) ([]*Edge, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	edges := make([]*Edge, 0, len(inputs))
	for _, inp := range inputs {
		// Verify from-node exists live; get its domain for diagnostic messages.
		var fromDomain string
		if err := tx.QueryRow(`SELECT domain FROM nodes WHERE id = ? AND archived_at IS NULL`, inp.FromNode).Scan(&fromDomain); err != nil {
			tx.Rollback()
			if err == sql.ErrNoRows {
				return nil, fmt.Errorf("node not found: %s", inp.FromNode)
			}
			return nil, err
		}
		// Verify to-node exists live.
		var toCount int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ? AND archived_at IS NULL`, inp.ToNode).Scan(&toCount); err != nil {
			tx.Rollback()
			return nil, err
		}
		if toCount == 0 {
			tx.Rollback()
			// Distinguish: archived vs. genuinely missing.
			var archivedCount int
			s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ? AND archived_at IS NOT NULL`, inp.ToNode).Scan(&archivedCount)
			if archivedCount > 0 {
				return nil, fmt.Errorf("memory archived; use restore first (id %q)", inp.ToNode)
			}
			return nil, fmt.Errorf("memory not found: %q — verify the ID with recall or search (filing domain was %q)", inp.ToNode, fromDomain)
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

// collectEdges returns edges whose both endpoints appear in nodes.
func collectEdges(db *sql.DB, nodes []Node) []Edge {
	if len(nodes) <= 1 {
		return nil
	}
	nodeIDs := mapSlice(nodes, func(n Node) string { return n.ID })
	ph, ids := inClause(nodeIDs)
	edgeQ := "SELECT id, from_node, to_node, relationship, narrative, created_at FROM edges WHERE from_node IN (" +
		ph + ") AND to_node IN (" + ph + ")"
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
