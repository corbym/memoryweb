package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type archivedNode struct {
	id         string
	label      string
	archivedAt time.Time
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.memoryweb.db"
}

func main() {
	dbFlag := flag.String("db", defaultDBPath(), "path to the SQLite database file")
	domainFlag := flag.String("domain", "", "scope purge to this domain only")
	beforeFlag := flag.String("before", "", "purge only nodes archived before this ISO8601 date")
	dryRun := flag.Bool("dry-run", false, "print what would be purged without deleting anything")
	confirm := flag.Bool("confirm", false, "required to actually execute; without it nothing is deleted")
	flag.Parse()

	if !*dryRun && !*confirm {
		fmt.Fprintln(os.Stderr, "warning: no action taken. Use --confirm to purge archived nodes, or --dry-run to preview.")
		os.Exit(1)
	}

	// Parse optional --before date.
	var beforeTime *time.Time
	if *beforeFlag != "" {
		t, err := time.Parse(time.RFC3339, *beforeFlag)
		if err != nil {
			t, err = time.Parse("2006-01-02", *beforeFlag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid --before value (use ISO8601 date or datetime): %s\n", *beforeFlag)
				os.Exit(1)
			}
		}
		beforeTime = &t
	}

	conn, err := sql.Open("sqlite3", *dbFlag+"?_journal_mode=WAL")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// ── find nodes to purge ───────────────────────────────────────────────────

	conds := []string{"archived_at IS NOT NULL"}
	args := []interface{}{}

	if *domainFlag != "" {
		conds = append(conds, "domain = ?")
		args = append(args, *domainFlag)
	}
	if beforeTime != nil {
		conds = append(conds, "archived_at < ?")
		args = append(args, beforeTime.UTC())
	}

	query := "SELECT id, label, archived_at FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY archived_at ASC"

	rows, err := conn.Query(query, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: query archived nodes: %v\n", err)
		os.Exit(1)
	}

	var nodes []archivedNode
	for rows.Next() {
		var n archivedNode
		var at sql.NullTime
		if err := rows.Scan(&n.id, &n.label, &at); err != nil {
			rows.Close()
			fmt.Fprintf(os.Stderr, "error: scan: %v\n", err)
			os.Exit(1)
		}
		if at.Valid {
			n.archivedAt = at.Time
		}
		nodes = append(nodes, n)
	}
	rows.Close()

	// ── dry-run: print and exit without touching anything ─────────────────────

	if *dryRun {
		fmt.Printf("DRY RUN — no changes will be made.\n")
		fmt.Printf("%d archived node(s) would be purged:\n", len(nodes))
		for _, n := range nodes {
			fmt.Printf("  - %s (id: %s, archived: %s)\n",
				n.label, n.id, n.archivedAt.UTC().Format(time.RFC3339))
		}
		return
	}

	// ── confirm: hard-delete nodes and their edges ────────────────────────────

	if len(nodes) == 0 {
		fmt.Println("0 node(s) purged, 0 edge(s) removed")
		return
	}

	totalEdges := 0
	now := time.Now().UTC()

	for i, n := range nodes {
		// Write audit_log entry before deleting.
		auditID := fmt.Sprintf("auditlog-purge-%d-%d", now.UnixNano(), i)
		if _, err := conn.Exec(
			`INSERT INTO audit_log (id, action, node_id, node_label, reason, actioned_at) VALUES (?, ?, ?, ?, ?, ?)`,
			auditID, "purge", n.id, n.label, nil, now,
		); err != nil {
			fmt.Fprintf(os.Stderr, "error: write audit_log for %s: %v\n", n.id, err)
			os.Exit(1)
		}

		// Delete edges referencing this node.
		res, err := conn.Exec(
			`DELETE FROM edges WHERE from_node = ? OR to_node = ?`, n.id, n.id,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: delete edges for %s: %v\n", n.id, err)
			os.Exit(1)
		}
		deleted, _ := res.RowsAffected()
		totalEdges += int(deleted)

		// Hard-delete the node.
		if _, err := conn.Exec(`DELETE FROM nodes WHERE id = ?`, n.id); err != nil {
			fmt.Fprintf(os.Stderr, "error: delete node %s: %v\n", n.id, err)
			os.Exit(1)
		}
	}

	fmt.Printf("%d node(s) purged, %d edge(s) removed\n", len(nodes), totalEdges)
}
