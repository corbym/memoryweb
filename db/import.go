package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// ImportOptions controls the behaviour of ImportDB.
type ImportOptions struct {
	SkipArchived    bool   // exclude archived nodes (and their edges) from import
	IncludeAuditLog bool   // import audit_log rows whose node_id was imported
	OnConflict      string // "error" (default) or "skip"
	BatchSize       int    // nodes/edges per transaction; default 500
	DryRun          bool   // report what would be imported without writing anything
}

// ImportSkip records a node or edge that was skipped and why.
type ImportSkip struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Reason string `json:"reason"`
}

// ImportConflict records a node whose ID exists in the destination with different content.
type ImportConflict struct {
	ID          string `json:"id"`
	SourceLabel string `json:"source_label"`
	DestLabel   string `json:"dest_label"`
}

// ImportReport is the structured output of an ImportDB run.
type ImportReport struct {
	SourceNodes         int              `json:"source_nodes"`
	SourceEdges         int              `json:"source_edges"`
	ImportedNodes       int              `json:"imported_nodes"`
	ImportedEdges       int              `json:"imported_edges"`
	SkippedNodes        []ImportSkip     `json:"skipped_nodes,omitempty"`
	SkippedEdges        []ImportSkip     `json:"skipped_edges,omitempty"`
	ConflictNodes       []ImportConflict `json:"conflict_nodes,omitempty"`
	HasSourceEmbeddings bool             `json:"has_source_embeddings"`
	DryRun              bool             `json:"dry_run"`
}

// ImportDB merges the source database at srcPath into the destination database
// at dstPath. Node IDs, all four timestamps, and every relationship type string
// are preserved verbatim. Embeddings are not migrated — run "memoryweb backfill"
// after import to regenerate them. The source is opened read-only. Pending schema
// migrations are applied to the destination before the first write.
//
// Conflict rules (same ID, different label or domain):
//   - OnConflict="error" (default): returns an error immediately.
//   - OnConflict="skip": records the conflict in the report and continues.
//
// Idempotency: same ID with identical label and domain is skipped silently.
// Re-running a previously interrupted import is safe.
func ImportDB(srcPath, dstPath string, opts ImportOptions) (*ImportReport, error) {
	// Use os.SameFile to correctly handle case-insensitive filesystems (Windows, macOS).
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open source %s: %w", srcPath, err)
	}
	if dstInfo, statErr := os.Stat(dstPath); statErr == nil && os.SameFile(srcInfo, dstInfo) {
		return nil, fmt.Errorf("source and destination must be different files")
	}

	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = "error"
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	// Open source read-only — do not run migrations or write anything.
	srcDSN := "file:" + url.PathEscape(srcPath) + "?mode=ro"
	src, err := sql.Open("sqlite3_memoryweb", srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source %s: %w", srcPath, err)
	}
	defer src.Close()
	if err := src.Ping(); err != nil {
		return nil, fmt.Errorf("cannot open source %s: %w", srcPath, err)
	}

	// Open destination through the normal path — applies pending migrations.
	dst, err := New(dstPath)
	if err != nil {
		return nil, fmt.Errorf("open destination %s: %w", dstPath, err)
	}
	defer dst.Close()

	report := &ImportReport{DryRun: opts.DryRun}

	// ── probe source schema ───────────────────────────────────────────────────
	nodeCols, err := probeTableColumns(src, "nodes")
	if err != nil {
		return nil, fmt.Errorf("probe source nodes schema: %w", err)
	}
	edgeCols, err := probeTableColumns(src, "edges")
	if err != nil {
		return nil, fmt.Errorf("probe source edges schema: %w", err)
	}

	// Check whether the source has any embeddings (informational hint).
	var embCount int
	if src.QueryRow(`SELECT COUNT(*) FROM node_embeddings`).Scan(&embCount) == nil && embCount > 0 {
		report.HasSourceEmbeddings = true
	}

	// ── read and classify source nodes ────────────────────────────────────────
	srcNodes, err := readSourceNodes(src, nodeCols, opts.SkipArchived)
	if err != nil {
		return nil, fmt.Errorf("read source nodes: %w", err)
	}
	report.SourceNodes = len(srcNodes)

	// Batch-check which source node IDs already exist in the destination.
	srcNodeIDs := mapSlice(srcNodes, func(n Node) string { return n.ID })
	destNodeMap, err := batchCheckDestNodes(dst.db, srcNodeIDs)
	if err != nil {
		return nil, fmt.Errorf("classify source nodes: %w", err)
	}

	var toImportNodes []Node
	// importedNodeIDs: IDs that are (or will be) present in the destination.
	importedNodeIDs := map[string]bool{}

	for _, n := range srcNodes {
		existing, exists := destNodeMap[n.ID]
		switch {
		case !exists:
			toImportNodes = append(toImportNodes, n)
		case existing[0] == n.Label && existing[1] == n.Domain:
			// Already present with same identity — idempotent skip.
			report.SkippedNodes = append(report.SkippedNodes, ImportSkip{
				ID:     n.ID,
				Label:  n.Label,
				Reason: "already present (idempotent skip)",
			})
			importedNodeIDs[n.ID] = true
		default:
			// Same ID, different label or domain — conflict.
			report.ConflictNodes = append(report.ConflictNodes, ImportConflict{
				ID:          n.ID,
				SourceLabel: n.Label,
				DestLabel:   existing[0],
			})
			if onConflict == "error" {
				return nil, fmt.Errorf(
					"conflict: node %s exists in destination with different content "+
						"(source label: %q domain: %q; dest label: %q domain: %q) "+
						"— use --on-conflict skip to record and continue",
					n.ID, n.Label, n.Domain, existing[0], existing[1],
				)
			}
			report.SkippedNodes = append(report.SkippedNodes, ImportSkip{
				ID:    n.ID,
				Label: n.Label,
				Reason: fmt.Sprintf(
					"conflict: destination already has ID with label %q domain %q",
					existing[0], existing[1],
				),
			})
		}
	}

	// ── insert nodes in batches ───────────────────────────────────────────────
	if !opts.DryRun {
		for i := 0; i < len(toImportNodes); i += batchSize {
			end := min(i+batchSize, len(toImportNodes))
			if err := importNodesChunk(dst.db, toImportNodes[i:end]); err != nil {
				return nil, fmt.Errorf("insert nodes (batch at offset %d): %w", i, err)
			}
		}
	}
	for _, n := range toImportNodes {
		importedNodeIDs[n.ID] = true
	}
	report.ImportedNodes = len(toImportNodes)

	// ── read source edges ─────────────────────────────────────────────────────
	srcEdges, err := readSourceEdges(src, edgeCols)
	if err != nil {
		return nil, fmt.Errorf("read source edges: %w", err)
	}
	report.SourceEdges = len(srcEdges)

	// Batch-check edge endpoints that are not already in importedNodeIDs against
	// the destination (they may be pre-existing nodes from a prior import or the
	// destination's own data).
	unknownEndpoints := collectUnknownEndpoints(srcEdges, importedNodeIDs)
	preExistingInDst, err := batchCheckExistence(dst.db, "nodes", unknownEndpoints)
	if err != nil {
		return nil, fmt.Errorf("check destination nodes for edge endpoints: %w", err)
	}

	nodeAvailable := func(id string) bool {
		return importedNodeIDs[id] || preExistingInDst[id]
	}

	// Batch-check which source edge IDs already exist in the destination.
	srcEdgeIDs := mapSlice(srcEdges, func(e Edge) string { return e.ID })
	destEdgeSet, err := batchCheckExistence(dst.db, "edges", srcEdgeIDs)
	if err != nil {
		return nil, fmt.Errorf("check destination edges: %w", err)
	}

	// Classify each source edge.
	var toImportEdges []Edge
	for _, e := range srcEdges {
		if destEdgeSet[e.ID] {
			report.SkippedEdges = append(report.SkippedEdges, ImportSkip{
				ID:     e.ID,
				Reason: "already present (idempotent skip)",
			})
			continue
		}

		fromOK := nodeAvailable(e.FromNode)
		toOK := nodeAvailable(e.ToNode)
		if !fromOK || !toOK {
			var reason string
			switch {
			case !fromOK && !toOK:
				reason = fmt.Sprintf("both endpoints missing in destination (%s, %s)", e.FromNode, e.ToNode)
			case !fromOK:
				reason = fmt.Sprintf("from-node missing in destination (%s)", e.FromNode)
			default:
				reason = fmt.Sprintf("to-node missing in destination (%s)", e.ToNode)
			}
			report.SkippedEdges = append(report.SkippedEdges, ImportSkip{ID: e.ID, Reason: reason})
			continue
		}
		toImportEdges = append(toImportEdges, e)
	}

	// ── insert edges in batches ───────────────────────────────────────────────
	if !opts.DryRun {
		for i := 0; i < len(toImportEdges); i += batchSize {
			end := min(i+batchSize, len(toImportEdges))
			if err := importEdgesChunk(dst.db, toImportEdges[i:end]); err != nil {
				return nil, fmt.Errorf("insert edges (batch at offset %d): %w", i, err)
			}
		}
	}
	report.ImportedEdges = len(toImportEdges)

	// ── optionally import audit_log ───────────────────────────────────────────
	if opts.IncludeAuditLog && !opts.DryRun {
		if err := importAuditLog(src, dst.db, importedNodeIDs); err != nil {
			return nil, fmt.Errorf("import audit log: %w", err)
		}
	}

	return report, nil
}

// ── private helpers ───────────────────────────────────────────────────────────

// probeTableColumns returns a set of column names present in the given table.
// Returns an empty map (not an error) when the table does not exist.
func probeTableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// readSourceNodes reads all nodes from src using only the columns that exist,
// allowing import from databases built by older versions of memoryweb.
func readSourceNodes(src *sql.DB, cols map[string]bool, skipArchived bool) ([]Node, error) {
	hasOccurredAt := cols["occurred_at"]
	hasTags := cols["tags"]
	hasArchivedAt := cols["archived_at"]
	hasNodeKind := cols["node_kind"]
	hasDecisionType := cols["decision_type"] // migration v12, renamed to node_kind in v13

	sel := []string{"id", "label", "description", "why_matters", "domain", "created_at", "updated_at"}
	if hasOccurredAt {
		sel = append(sel, "occurred_at")
	}
	if hasTags {
		sel = append(sel, "tags")
	}
	if hasArchivedAt {
		sel = append(sel, "archived_at")
	}
	if hasNodeKind {
		sel = append(sel, "node_kind")
	}
	if !hasNodeKind && hasDecisionType {
		sel = append(sel, "decision_type")
	}

	where := ""
	if skipArchived && hasArchivedAt {
		where = " WHERE archived_at IS NULL"
	}

	rows, err := src.Query("SELECT " + strings.Join(sel, ", ") + " FROM nodes" + where)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var occurredAt, archivedAt sql.NullTime
		var tags, nodeKind sql.NullString

		dest := []any{&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt}
		if hasOccurredAt {
			dest = append(dest, &occurredAt)
		}
		if hasTags {
			dest = append(dest, &tags)
		}
		if hasArchivedAt {
			dest = append(dest, &archivedAt)
		}
		if hasNodeKind || hasDecisionType {
			dest = append(dest, &nodeKind)
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		n.OccurredAt = nullTimeToPtr(occurredAt)
		n.Tags = tags.String
		n.ArchivedAt = nullTimeToPtr(archivedAt)
		n.NodeKind = nodeKind.String
		if n.NodeKind == "" {
			n.NodeKind = "decision"
		}

		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// readSourceEdges reads all edges from src with column probing for the verdict
// column (added in migration v14).
func readSourceEdges(src *sql.DB, cols map[string]bool) ([]Edge, error) {
	hasVerdict := cols["verdict"]

	sel := []string{"id", "from_node", "to_node", "relationship", "narrative", "created_at"}
	if hasVerdict {
		sel = append(sel, "verdict")
	}

	rows, err := src.Query("SELECT " + strings.Join(sel, ", ") + " FROM edges")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		var verdict sql.NullString

		dest := []any{&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt}
		if hasVerdict {
			dest = append(dest, &verdict)
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if verdict.Valid {
			e.Verdict = verdict.String
		}

		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// importNodesChunk inserts a slice of nodes into dst in a single transaction,
// preserving IDs, all four timestamps, and relationship types verbatim.
// INSERT OR IGNORE is used so that concurrent imports do not abort each other —
// the conflict classification step already gates what reaches this function.
func importNodesChunk(dst *sql.DB, nodes []Node) error {
	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, n := range nodes {
		nodeKind := n.NodeKind
		if nodeKind == "" {
			nodeKind = "decision"
		}
		var archivedAt, occurredAt any
		if n.ArchivedAt != nil {
			archivedAt = n.ArchivedAt.UTC()
		}
		if n.OccurredAt != nil {
			occurredAt = n.OccurredAt.UTC()
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO nodes
			 (id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Label, n.Description, n.WhyMatters, n.Domain,
			n.CreatedAt.UTC(), n.UpdatedAt.UTC(), occurredAt, archivedAt,
			n.Tags, nodeKind,
		); err != nil {
			return fmt.Errorf("node %s (%q): %w", n.ID, n.Label, err)
		}
	}
	return tx.Commit()
}

// importEdgesChunk inserts a slice of edges into dst in a single transaction,
// preserving IDs, relationship type strings, and timestamps verbatim.
func importEdgesChunk(dst *sql.DB, edges []Edge) error {
	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, e := range edges {
		var verdict any
		if e.Verdict != "" {
			verdict = e.Verdict
		}
		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_node, to_node, relationship, narrative, verdict, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.FromNode, e.ToNode, e.Relationship, e.Narrative, verdict, e.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("edge %s: %w", e.ID, err)
		}
	}
	return tx.Commit()
}

// importAuditLog copies audit_log rows from src to dst for nodes that were
// imported (their IDs appear in importedIDs). The filter is pushed into SQL to
// avoid loading the entire audit_log into memory. Handles older source schemas
// that are missing the provenance column (added in migration v10).
func importAuditLog(src, dst *sql.DB, importedIDs map[string]bool) error {
	auditCols, err := probeTableColumns(src, "audit_log")
	if err != nil || len(auditCols) == 0 {
		return nil // table absent in very old schemas
	}
	hasProvenance := auditCols["provenance"]

	sel := []string{"id", "action", "node_id", "node_label", "reason", "actioned_at"}
	if hasProvenance {
		sel = append(sel, "provenance")
	}

	importedList := make([]string, 0, len(importedIDs))
	for id := range importedIDs {
		importedList = append(importedList, id)
	}
	if len(importedList) == 0 {
		return nil
	}

	type auditRow struct {
		id, action, nodeID, nodeLabel string
		reason, provenance            sql.NullString
		actionedAt                    time.Time
	}

	var toInsert []auditRow
	for i := 0; i < len(importedList); i += 900 {
		end := min(i+900, len(importedList))
		ph, args := inClause(importedList[i:end])
		rows, err := src.Query(
			"SELECT "+strings.Join(sel, ", ")+" FROM audit_log WHERE node_id IN ("+ph+")",
			args...,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r auditRow
			dest := []any{&r.id, &r.action, &r.nodeID, &r.nodeLabel, &r.reason, &r.actionedAt}
			if hasProvenance {
				dest = append(dest, &r.provenance)
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			toInsert = append(toInsert, r)
		}
		rerr := rows.Err()
		rows.Close()
		if rerr != nil {
			return rerr
		}
	}

	if len(toInsert) == 0 {
		return nil
	}

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, r := range toInsert {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO audit_log
			 (id, action, node_id, node_label, reason, provenance, actioned_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.id, r.action, r.nodeID, r.nodeLabel, r.reason, r.provenance, r.actionedAt.UTC(),
		); err != nil {
			return fmt.Errorf("audit_log row %s: %w", r.id, err)
		}
	}
	return tx.Commit()
}

// batchCheckDestNodes returns a map of id → [label, domain] for the subset of
// ids that already exist in the destination, checked in 900-item batches to
// respect SQLite's variable limit on older builds.
func batchCheckDestNodes(dst *sql.DB, ids []string) (map[string][2]string, error) {
	result := make(map[string][2]string, len(ids))
	for i := 0; i < len(ids); i += 900 {
		end := min(i+900, len(ids))
		ph, args := inClause(ids[i:end])
		rows, err := dst.Query(`SELECT id, label, domain FROM nodes WHERE id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, label, domain string
			if err := rows.Scan(&id, &label, &domain); err != nil {
				rows.Close()
				return nil, err
			}
			result[id] = [2]string{label, domain}
		}
		rerr := rows.Err()
		rows.Close()
		if rerr != nil {
			return nil, rerr
		}
	}
	return result, nil
}

// batchCheckExistence returns the set of IDs from ids that exist in the named
// table, checked in 900-item batches.
func batchCheckExistence(db *sql.DB, table string, ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	for i := 0; i < len(ids); i += 900 {
		end := min(i+900, len(ids))
		ph, args := inClause(ids[i:end])
		rows, err := db.Query(`SELECT id FROM `+table+` WHERE id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			result[id] = true
		}
		rerr := rows.Err()
		rows.Close()
		if rerr != nil {
			return nil, rerr
		}
	}
	return result, nil
}

// collectUnknownEndpoints returns the unique edge endpoint IDs that are not
// already in knownIDs, for batch-checking against the destination.
func collectUnknownEndpoints(edges []Edge, knownIDs map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range edges {
		for _, id := range []string{e.FromNode, e.ToNode} {
			if !knownIDs[id] && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}
