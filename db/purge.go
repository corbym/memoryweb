package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

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
