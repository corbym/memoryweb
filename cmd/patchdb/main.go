// cmd/patchdb cleans noise nodes (and their orphaned edges) from memoryweb.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbPath := flag.String("db", "", "Path to the memoryweb SQLite database (default: ~/.memoryweb/.memoryweb.db)")
	dryRun := flag.Bool("dry-run", false, "Print what would be deleted without deleting anything")
	flag.Parse()

	resolvedDB := *dbPath
	if resolvedDB == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
			os.Exit(1)
		}
		resolvedDB = filepath.Join(home, ".memoryweb", ".memoryweb.db")
	}

	db, err := sql.Open("sqlite3", resolvedDB+"?_journal_mode=WAL")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot open database:", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── identify noise nodes ────────────────────────────────────────────────

	noiseQuery := `
		SELECT id, label FROM nodes
		WHERE domain = 'sedex'
		AND (
			label LIKE 'Merged in renovate/%'
			OR label LIKE 'Merged in renovate%'
			OR label LIKE 'Update dependency%'
			OR label LIKE 'Update flyway%'
			OR label LIKE 'Update % to v%'
			OR (description LIKE 'Update dependency%' AND why_matters = '')
		)
		ORDER BY label
	`

	rows, err := db.Query(noiseQuery)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: query failed:", err)
		os.Exit(1)
	}

	type nodeRow struct {
		id    string
		label string
	}
	var noiseNodes []nodeRow
	for rows.Next() {
		var n nodeRow
		if err := rows.Scan(&n.id, &n.label); err != nil {
			rows.Close()
			fmt.Fprintln(os.Stderr, "error: scan failed:", err)
			os.Exit(1)
		}
		noiseNodes = append(noiseNodes, n)
	}
	rows.Close()

	if len(noiseNodes) == 0 {
		fmt.Println("No noise nodes found — nothing to do.")
		return
	}

	// ── identify orphaned edges ─────────────────────────────────────────────
	// An edge is orphaned if either endpoint is in our delete set.

	// Build an IN list from the noise node IDs.
	ids := make([]interface{}, len(noiseNodes))
	placeholders := make([]byte, 0, len(noiseNodes)*2)
	for i, n := range noiseNodes {
		ids[i] = n.id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	ph := string(placeholders)

	orphanQuery := "SELECT id FROM edges WHERE from_node IN (" + ph + ") OR to_node IN (" + ph + ")"
	eRows, err := db.Query(orphanQuery, append(ids, ids...)...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: edge query failed:", err)
		os.Exit(1)
	}

	var orphanEdgeIDs []string
	for eRows.Next() {
		var eid string
		if err := eRows.Scan(&eid); err != nil {
			eRows.Close()
			fmt.Fprintln(os.Stderr, "error: edge scan failed:", err)
			os.Exit(1)
		}
		orphanEdgeIDs = append(orphanEdgeIDs, eid)
	}
	eRows.Close()

	// ── report ──────────────────────────────────────────────────────────────

	fmt.Printf("Noise nodes to delete (%d):\n", len(noiseNodes))
	for _, n := range noiseNodes {
		fmt.Printf("  [%s]  %s\n", n.id, n.label)
	}
	fmt.Printf("\nOrphaned edges to delete: %d\n", len(orphanEdgeIDs))

	if *dryRun {
		fmt.Println("\n--dry-run: nothing deleted.")
		return
	}

	// ── delete ──────────────────────────────────────────────────────────────

	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not begin transaction:", err)
		os.Exit(1)
	}

	// Delete orphaned edges first (FK order)
	var deletedEdges int64
	if len(orphanEdgeIDs) > 0 {
		eph := make([]byte, 0, len(orphanEdgeIDs)*2)
		eids := make([]interface{}, len(orphanEdgeIDs))
		for i, eid := range orphanEdgeIDs {
			eids[i] = eid
			if i > 0 {
				eph = append(eph, ',')
			}
			eph = append(eph, '?')
		}
		res, err := tx.Exec("DELETE FROM edges WHERE id IN ("+string(eph)+")", eids...)
		if err != nil {
			tx.Rollback()
			fmt.Fprintln(os.Stderr, "error: edge delete failed:", err)
			os.Exit(1)
		}
		deletedEdges, _ = res.RowsAffected()
	}

	// Delete noise nodes
	res, err := tx.Exec("DELETE FROM nodes WHERE id IN ("+ph+")", ids...)
	if err != nil {
		tx.Rollback()
		fmt.Fprintln(os.Stderr, "error: node delete failed:", err)
		os.Exit(1)
	}
	deletedNodes, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		fmt.Fprintln(os.Stderr, "error: commit failed:", err)
		os.Exit(1)
	}

	fmt.Printf("\nDeleted %d nodes, %d edges.\n", deletedNodes, deletedEdges)
}

