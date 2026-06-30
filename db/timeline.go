package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RecentChanges orders by updated_at DESC, breaking ties on rowid DESC so two
// nodes written within the same clock tick (observed on Windows) still come
// back in insertion order instead of in SQLite's unspecified tie order.
func (s *Store) RecentChanges(domain string, limit int, nodeKinds []string) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	conds := []string{"archived_at IS NULL"}
	args := []interface{}{}
	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, limit)

	q := `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE ` +
		strings.Join(conds, " AND ") + ` ORDER BY updated_at DESC, rowid DESC LIMIT ?`

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
		rows.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind)
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

// RecentChangesScoped is a composable variant of RecentChanges that supports
// tag filtering and neighbourhood scoping via a memory_id.
//
// When memoryID is non-empty the query is restricted to the depth-hop
// neighbourhood of that memory; domain is ignored in that case.
// When tags is non-nil and non-empty, only nodes whose tags column contains
// at least one of the supplied tags (whole-word OR match) are returned.
func (s *Store) RecentChangesScoped(memoryID string, depth int, domain string, tags, nodeKinds []string, limit int) ([]Node, error) {
	domain = s.ResolveAlias(domain)
	conds := []string{"archived_at IS NULL"}
	args := []interface{}{}

	if memoryID != "" {
		ids, _, err := s.neighbourhoodIDs(memoryID, depth)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil
		}
		ph, phArgs := inClause(ids)
		conds = append(conds, "id IN ("+ph+")")
		args = append(args, phArgs...)
	} else if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}

	conds, args = tagFilter("tags", tags, conds, args)
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, limit)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY updated_at DESC, rowid DESC LIMIT ?"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodeRows(rows)
}

// Timeline returns nodes ordered by COALESCE(occurred_at, created_at) ASC.
// When importantOnly is true, only nodes with occurred_at explicitly set are returned.
// tags filters to nodes matching at least one tag (whole-word match).
// from/to filter by effective date (COALESCE(occurred_at, created_at)).
func (s *Store) Timeline(domain string, importantOnly bool, tags, nodeKinds []string, from, to *time.Time, limit int) ([]Node, error) {
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
	conds, args = tagFilter("tags", tags, conds, args)
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, limit)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY COALESCE(occurred_at, created_at) ASC LIMIT ?"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodeRows(rows)
}

// GetHistoryForMemoryID returns the chronological timeline of a memory's
// neighbourhood (depth hops from nodeID, domain-clipped). Applies the same
// filters as Timeline: importantOnly, tags, from/to date range.
func (s *Store) GetHistoryForMemoryID(nodeID string, depth int, importantOnly bool, tags, nodeKinds []string, from, to *time.Time, limit int) ([]Node, error) {
	ids, _, err := s.neighbourhoodIDs(nodeID, depth)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Node{}, nil
	}

	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]

	conds := []string{"archived_at IS NULL", "id IN (" + ph + ")"}
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
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
	conds, args = tagFilter("tags", tags, conds, args)
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	if limit <= 0 {
		limit = 20
	}
	args = append(args, limit)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY COALESCE(occurred_at, created_at) ASC LIMIT ?"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("GetHistoryForMemoryID: %w", err)
	}
	defer rows.Close()

	nodes, err := scanNodeRows(rows)
	if err != nil {
		return nil, fmt.Errorf("GetHistoryForMemoryID rows: %w", err)
	}
	return nodes, nil
}
