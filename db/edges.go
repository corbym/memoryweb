package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

const edgeSelectColumns = `id, from_node, to_node, relationship, narrative, verdict, created_at`

type Edge struct {
	ID           string    `json:"id"`
	FromNode     string    `json:"from_memory"`
	ToNode       string    `json:"to_memory"`
	Relationship string    `json:"relationship"`
	Narrative    string    `json:"narrative"`
	Verdict      string    `json:"verdict,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// EdgeInput is the input type for AddEdgesBatch.
type EdgeInput struct {
	FromNode     string
	ToNode       string
	Relationship string
	Narrative    string
	Verdict      string
}

func scanEdge(scanner interface {
	Scan(dest ...any) error
}) (Edge, error) {
	var e Edge
	var verdict sql.NullString
	if err := scanner.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &verdict, &e.CreatedAt); err != nil {
		return Edge{}, err
	}
	if verdict.Valid {
		e.Verdict = verdict.String
	}
	return e, nil
}

func (s *Store) AddEdge(fromID, toID, relationship, narrative string, verdict ...string) (*Edge, error) {
	v := ""
	if len(verdict) > 0 {
		v = verdict[0]
	}
	if relationship != "resolved" {
		v = ""
	}
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
	var verdictVal interface{}
	if v != "" {
		verdictVal = v
	}
	_, err := s.db.Exec(
		`INSERT INTO edges (id, from_node, to_node, relationship, narrative, verdict, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, fromID, toID, relationship, narrative, verdictVal, now,
	)
	if err != nil {
		return nil, err
	}
	edge := Edge{id, fromID, toID, relationship, narrative, v, now}
	return &edge, nil
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
		verdict := inp.Verdict
		if inp.Relationship != "resolved" {
			verdict = ""
		}
		id := "edge-" + shortID()
		var verdictVal interface{}
		if verdict != "" {
			verdictVal = verdict
		}
		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_node, to_node, relationship, narrative, verdict, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, inp.FromNode, inp.ToNode, inp.Relationship, inp.Narrative, verdictVal, now,
		); err != nil {
			tx.Rollback()
			return nil, err
		}
		edges = append(edges, &Edge{id, inp.FromNode, inp.ToNode, inp.Relationship, inp.Narrative, verdict, now})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return edges, nil
}

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
	edgeQ := "SELECT " + edgeSelectColumns + " FROM edges WHERE from_node IN (" +
		ph + ") AND to_node IN (" + ph + ")"
	eRows, err := db.Query(edgeQ, append(ids, ids...)...)
	if err != nil {
		return nil
	}
	defer eRows.Close()
	var edges []Edge
	for eRows.Next() {
		e, err := scanEdge(eRows)
		if err != nil {
			log.Printf("[memoryweb] collectEdges scan: %v", err)
			continue
		}
		edges = append(edges, e)
	}
	return edges
}
