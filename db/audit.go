package db

import (
	"database/sql"
	"strings"
	"time"
)

// DriftCandidate is a node flagged as potentially stale or conflicting.
type DriftCandidate struct {
	Node          Node   `json:"node"`
	ConflictsWith *Node  `json:"conflicts_with,omitempty"`
	Reason        string `json:"reason"`
	EdgeCount     int    `json:"edge_count"` // total edges (from + to) incident to this node
}

// ── drift detection ───────────────────────────────────────────────────────────

// FindDrift returns nodes that may be stale, contradicted, or superseded.
// Rules are applied in order; the first match per node wins:
//  1. Contradiction: connected by a "contradicts" edge.
//  2. Superseded label: contains "old", "deprecated", "replaced", "legacy", "previous".
//  3. Stale open question: contains open-question keywords and is older than 30 days.
//  4. Duplicate label: identical lowercased label in the same domain.
//  5. Transient node older than 7 days.
//  6. Standing node with fewer than 2 inbound edges and older than 30 days.
func (s *Store) FindDrift(domain string, limit int, tags, nodeKinds []string, memoryID string, depth int) ([]DriftCandidate, error) {
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

	var err error

	// ── Rule 1: contradicts edges ─────────────────────────────────────────────
	rows, err := s.db.Query(`
		SELECT a.id, a.label, a.description, a.why_matters, a.domain,
		       a.created_at, a.updated_at, a.occurred_at, a.archived_at, a.tags, a.node_kind,
		       b.id, b.label, b.description, b.why_matters, b.domain,
		       b.created_at, b.updated_at, b.occurred_at, b.archived_at, b.tags, b.node_kind
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
			aNodeKind                         string
			bID, bLabel, bDesc, bWhy, bDomain string
			bCreated, bUpdated                time.Time
			bOA, bAA                          sql.NullTime
			bTags                             string
			bNodeKind                         string
		)
		if err := rows.Scan(
			&aID, &aLabel, &aDesc, &aWhy, &aDomain, &aCreated, &aUpdated, &aOA, &aAA, &aTags, &aNodeKind,
			&bID, &bLabel, &bDesc, &bWhy, &bDomain, &bCreated, &bUpdated, &bOA, &bAA, &bTags, &bNodeKind,
		); err != nil {
			rows.Close()
			return nil, err
		}
		a := Node{ID: aID, Label: aLabel, Description: aDesc, WhyMatters: aWhy, Domain: aDomain, CreatedAt: aCreated, UpdatedAt: aUpdated, Tags: aTags, NodeKind: aNodeKind}
		a.OccurredAt = nullTimeToPtr(aOA)
		a.ArchivedAt = nullTimeToPtr(aAA)
		b := Node{ID: bID, Label: bLabel, Description: bDesc, WhyMatters: bWhy, Domain: bDomain, CreatedAt: bCreated, UpdatedAt: bUpdated, Tags: bTags, NodeKind: bNodeKind}
		b.OccurredAt = nullTimeToPtr(bOA)
		b.ArchivedAt = nullTimeToPtr(bAA)
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+supersededKW, domain)
	} else {
		rows2, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind ` +
				`FROM nodes WHERE archived_at IS NULL AND ` + supersededKW)
	}
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		n, err := scanNodeRow(rows2)
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+staleKW+` AND `+ageFilter,
			domain, cutoff30, cutoff30)
	} else {
		rows3, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND `+staleKW+` AND `+ageFilter,
			cutoff30, cutoff30)
	}
	if err != nil {
		return nil, err
	}
	for rows3.Next() {
		n, err := scanNodeRow(rows3)
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND `+dupExists, domain)
	} else {
		rows4, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind ` +
				`FROM nodes WHERE archived_at IS NULL AND ` + dupExists)
	}
	if err != nil {
		return nil, err
	}
	for rows4.Next() {
		n, err := scanNodeRow(rows4)
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
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND domain = ? AND node_kind = 'transient' AND created_at < ?`,
			domain, cutoff7)
	} else {
		rows5, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind `+
				`FROM nodes WHERE archived_at IS NULL AND node_kind = 'transient' AND created_at < ?`,
			cutoff7)
	}
	if err != nil {
		return nil, err
	}
	for rows5.Next() {
		n, err := scanNodeRow(rows5)
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

	// ── Rule 6: standing nodes with low inbound edge count older than 30 days ──
	cutoff30standing := time.Now().UTC().AddDate(0, 0, -30)
	var rows6 *sql.Rows
	if domain != "" {
		rows6, err = s.db.Query(
			`SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind,
			        COUNT(e.id) AS inbound_count
			 FROM nodes n
			 LEFT JOIN edges e ON e.to_node = n.id
			 WHERE n.archived_at IS NULL
			   AND n.node_kind = 'standing'
			   AND n.created_at < ?
			   AND n.domain = ?
			 GROUP BY n.id
			 HAVING inbound_count < 2`,
			cutoff30standing, domain)
	} else {
		rows6, err = s.db.Query(
			`SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind,
			        COUNT(e.id) AS inbound_count
			 FROM nodes n
			 LEFT JOIN edges e ON e.to_node = n.id
			 WHERE n.archived_at IS NULL
			   AND n.node_kind = 'standing'
			   AND n.created_at < ?
			 GROUP BY n.id
			 HAVING inbound_count < 2`,
			cutoff30standing)
	}
	if err != nil {
		return nil, err
	}
	nodes6, err := scanRows(rows6, func(r *sql.Rows) (Node, error) {
		var n Node
		var oa, aa sql.NullTime
		var inboundCount int
		if err := r.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind, &inboundCount); err != nil {
			return Node{}, err
		}
		n.OccurredAt = nullTimeToPtr(oa)
		n.ArchivedAt = nullTimeToPtr(aa)
		return n, nil
	})
	rows6.Close()
	if err != nil {
		return nil, err
	}
	for _, n := range nodes6 {
		add(n, nil, "standing rule with low connection count — may not be in use")
	}

	// Post-filter by neighbourhood (memory_id scoping).
	if memoryID != "" {
		allowedIDs, _, err := s.neighbourhoodIDs(memoryID, depth)
		if err != nil {
			return nil, err
		}
		allowed := make(map[string]bool, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = true
		}
		out = filter(out, func(c DriftCandidate) bool { return allowed[c.Node.ID] })
	}

	// Post-filter by tags (whole-word OR match).
	if len(tags) > 0 {
		out = filter(out, func(c DriftCandidate) bool { return nodeMatchesTags(c.Node.Tags, tags) })
	}

	// Post-filter by node_kind (OR match).
	if len(nodeKinds) > 0 {
		out = filter(out, func(c DriftCandidate) bool { return nodeMatchesNodeKind(c.Node.NodeKind, nodeKinds) })
	}

	if len(out) > limit {
		out = out[:limit]
	}

	// Enrich each candidate with its total edge count (from + to).
	if len(out) > 0 {
		ids := mapSlice(out, func(c DriftCandidate) string { return c.Node.ID })
		ph, phArgs := inClause(ids)
		args := append(phArgs, phArgs...)
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

// CountStaleDrift returns the number of live nodes that would be surfaced by
// audit(mode=stale) — i.e. the union of all FindDrift rules. Used to populate
// the stale_count field in the orient response.
func (s *Store) CountStaleDrift(domain string) (int, error) {
	candidates, err := s.FindDrift(domain, 1000, nil, nil, "", 2)
	if err != nil {
		return 0, err
	}
	return len(candidates), nil
}

// FindDisconnected returns live, non-transient nodes that have no edges// (neither as from_node nor as to_node), optionally scoped to a domain.
func (s *Store) FindDisconnected(domain string, tags, nodeKinds []string) ([]Node, error) {
	domain = s.ResolveAlias(domain)

	conds := []string{
		"archived_at IS NULL",
		"node_kind != 'transient'",
		"id NOT IN (SELECT from_node FROM edges UNION SELECT to_node FROM edges)",
	}
	args := []interface{}{}

	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	conds, args = tagFilter("tags", tags, conds, args)
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, 50)

	q := "SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE " +
		strings.Join(conds, " AND ") + " ORDER BY created_at DESC LIMIT ?"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNodeRows(rows)
}
