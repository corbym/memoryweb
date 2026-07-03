package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PurgeResult holds the outcome of a Purge call.
type PurgeResult struct {
	Nodes         []Node // nodes that were (or would be) purged
	TotalEdges    int    // total edges deleted (0 in dry-run)
	LiveRemaining int    // live (non-archived) nodes still matching the domain filter, untouched
}

// Purge hard-deletes nodes from the database. By default only archived nodes
// are eligible — this is the only sanctioned hard-delete path and it never
// touches live data unless includeLive is explicitly set.
//
// domain and before are optional filters. When dryRun is true the database is
// not modified and only the candidate list is returned. When includeLive is
// true, live (non-archived) nodes matching domain are hard-deleted too — this
// is a genuine, irreversible removal of live data and callers must gate it
// behind an explicit opt-in (the CLI requires --domain alongside
// --include-live to prevent an accidental whole-graph wipe).
//
// When domain is set, LiveRemaining on the result reports how many live
// nodes match that domain and were left untouched (0 when includeLive is
// true, or when there is no domain filter). This exists so an operator who
// only archives-then-purges doesn't mistake "0 archived candidates" for
// "domain is empty" — the domain can still have live nodes.
func (s *Store) Purge(domain string, before *time.Time, dryRun bool, includeLive bool) (PurgeResult, error) {
	if domain != "" {
		domain = s.ResolveAlias(domain)
	}

	var conds []string
	args := []interface{}{}

	if !includeLive {
		conds = append(conds, "archived_at IS NOT NULL")
	}
	if domain != "" {
		// Case/whitespace-normalized match: on a long-lived database, archived
		// nodes may carry raw domain strings typed inconsistently across
		// sessions or years (e.g. "Sedex" vs "sedex" vs "sedex " vs "SEDEX").
		// A byte-exact match here would silently leave those variants archived
		// forever — a follow-up --dry-run would then report 0 for the literal
		// string typed, even though nodes/edges for the same conceptual domain
		// are still in the table.
		conds = append(conds, "LOWER(TRIM(domain)) = LOWER(TRIM(?))")
		args = append(args, domain)
	}
	if before != nil {
		conds = append(conds, "archived_at < ?")
		args = append(args, before.UTC())
	}

	query := "SELECT id, label, archived_at FROM nodes ORDER BY archived_at ASC"
	if len(conds) > 0 {
		query = "SELECT id, label, archived_at FROM nodes WHERE " +
			strings.Join(conds, " AND ") + " ORDER BY archived_at ASC"
	}

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
	if domain != "" && !includeLive {
		s.db.QueryRow( //nolint:errcheck // best-effort informational count, never fatal
			`SELECT COUNT(*) FROM nodes WHERE LOWER(TRIM(domain)) = LOWER(TRIM(?)) AND archived_at IS NULL`,
			domain,
		).Scan(&result.LiveRemaining)
	}
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
