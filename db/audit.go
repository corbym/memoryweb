package db

import (
	"database/sql"
	"strings"
	"time"
)

// ConflictCandidate is a pair of nodes that are semantically adjacent and may
// warrant agent review for potential contradiction. The server never asserts
// these conflict — only that their embedding distance is low enough to warrant
// attention.
type ConflictCandidate struct {
	AID              string  `json:"a_id"`
	ALabel           string  `json:"a_label"`
	BID              string  `json:"b_id"`
	BLabel           string  `json:"b_label"`
	SemanticDistance float64 `json:"semantic_distance"`
	Reason           string  `json:"reason"`
}

// conflictsDomainThreshold is the cross-domain distance ceiling for the
// conflicts mode. Lower (stricter) than CandidateSimilarityFloor so cross-domain
// pairs are only surfaced when very semantically close.
const conflictsDomainThreshold = 0.3

// FindConflictCandidates returns pairs of live nodes whose embedding distance
// is below CandidateSimilarityFloor, excluding pairs that already have a
// contradicts edge between them. Same-domain pairs are preferred; cross-domain
// pairs are included only when their distance is below conflictsDomainThreshold.
// The result contains up to limit pairs. Empty slice (not nil) is returned when
// no embeddings exist.
func (s *Store) FindConflictCandidates(domain string, limit int, tags, nodeKinds []string) ([]ConflictCandidate, error) {
	if limit <= 0 {
		limit = 10
	}
	domain = s.ResolveAlias(domain)

	// Fetch live nodes with their embeddings. If no embeddings table is
	// available or empty, return an empty slice.
	if !s.vecAvailable {
		return []ConflictCandidate{}, nil
	}

	conds := []string{"n.archived_at IS NULL"}
	args := []interface{}{}
	if domain != "" {
		conds = append(conds, "n.domain = ?")
		args = append(args, domain)
	}
	conds, args = tagFilter("n.tags", tags, conds, args)
	conds, args = nodeKindFilter("n.node_kind", nodeKinds, conds, args)

	nodeQ := `SELECT n.id, n.label, n.domain, e.embedding
	FROM node_embeddings e
	JOIN nodes n ON n.id = e.node_id
	WHERE ` + strings.Join(conds, " AND ") + `
	ORDER BY n.id`

	rows, err := s.db.Query(nodeQ, args...)
	if err != nil {
		return []ConflictCandidate{}, nil
	}
	defer rows.Close()

	type embNode struct {
		id        string
		label     string
		domain    string
		embedding []byte
	}
	var nodes []embNode
	for rows.Next() {
		var n embNode
		if err := rows.Scan(&n.id, &n.label, &n.domain, &n.embedding); err != nil {
			return []ConflictCandidate{}, nil
		}
		nodes = append(nodes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return []ConflictCandidate{}, nil
	}
	if len(nodes) < 2 {
		return []ConflictCandidate{}, nil
	}

	// Build a set of pairs to exclude: already-contradicting pairs, and pairs
	// already resolved via a resolved_by/supersedes edge directly between the
	// two nodes (checked in either direction).
	type pairKey struct{ a, b string }
	contradicting := make(map[pairKey]bool)
	edgeRows, err := s.db.Query(
		`SELECT from_node, to_node FROM edges WHERE relationship IN ('contradicts', 'resolved_by', 'supersedes')`)
	if err == nil {
		for edgeRows.Next() {
			var fn, tn string
			if edgeRows.Scan(&fn, &tn) == nil {
				// Normalise: always store smaller-ID first.
				if fn > tn {
					fn, tn = tn, fn
				}
				contradicting[pairKey{fn, tn}] = true
			}
		}
		edgeRows.Close()
		if err := edgeRows.Err(); err != nil {
			return []ConflictCandidate{}, nil
		}
	}

	// Compute pairwise distances using vec_distance_cosine via SQLite.
	// For N nodes this is O(N²) queries — bounded by the domain scope and limit.
	candidates := []ConflictCandidate{}
	seen := make(map[pairKey]bool)

	for i := 0; i < len(nodes) && len(candidates) < limit*2; i++ {
		na := nodes[i]
		if len(na.embedding) == 0 {
			continue
		}
		// Query distance from na to all other nodes with embeddings, applying
		// the same tags/node_kind scoping as the outer candidate query so a
		// scoped audit(mode=conflicts) call doesn't pair a matching node with
		// an unrelated one that fails the caller's own filter.
		innerConds := []string{"n.id != ?", "n.archived_at IS NULL"}
		innerArgs := []interface{}{na.id}
		innerConds, innerArgs = tagFilter("n.tags", tags, innerConds, innerArgs)
		innerConds, innerArgs = nodeKindFilter("n.node_kind", nodeKinds, innerConds, innerArgs)

		distQ := `SELECT n.id, n.label, n.domain,
			vec_distance_cosine(e.embedding, ?) AS dist
			FROM node_embeddings e
			JOIN nodes n ON n.id = e.node_id
			WHERE ` + strings.Join(innerConds, " AND ") + `
			ORDER BY dist ASC`

		distArgs := append([]interface{}{na.embedding}, innerArgs...)
		dRows, err := s.db.Query(distQ, distArgs...)
		if err != nil {
			continue
		}
		for dRows.Next() {
			var bID, bLabel, bDomain string
			var dist float64
			if err := dRows.Scan(&bID, &bLabel, &bDomain, &dist); err != nil {
				continue
			}
			// Hard floor.
			if dist > CandidateSimilarityFloor {
				break // ordered by dist ASC
			}
			// Domain scope filter for cross-domain.
			if na.domain != bDomain && dist > conflictsDomainThreshold {
				continue
			}
			// If a domain was requested, at least one node must be in it.
			if domain != "" && na.domain != domain && bDomain != domain {
				continue
			}

			// Normalise pair key: smaller ID first.
			var pAID, pALabel, pBID, pBLabel string
			if na.id <= bID {
				pAID, pALabel, pBID, pBLabel = na.id, na.label, bID, bLabel
			} else {
				pAID, pALabel, pBID, pBLabel = bID, bLabel, na.id, na.label
			}

			pk := pairKey{pAID, pBID}
			if seen[pk] || contradicting[pk] {
				continue
			}
			seen[pk] = true
			candidates = append(candidates, ConflictCandidate{
				AID:              pAID,
				ALabel:           pALabel,
				BID:              pBID,
				BLabel:           pBLabel,
				SemanticDistance: dist,
				Reason:           "semantically adjacent — agent adjudicates whether these conflict",
			})
			if len(candidates) >= limit*2 {
				break
			}
		}
		dRows.Close()
		if err := dRows.Err(); err != nil {
			return []ConflictCandidate{}, nil
		}
	}

	// Sort by distance ascending and cap at limit.
	sortConflictCandidates(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

// sortConflictCandidates sorts candidates by SemanticDistance ascending.
func sortConflictCandidates(cs []ConflictCandidate) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].SemanticDistance < cs[j-1].SemanticDistance; j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

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

	// ── Rule 1: contradicts edges (excluding pairs with resolution edges) ────────
	// A pair is considered resolved when a resolved_by or supersedes edge
	// connects the two contradicting nodes specifically, in either direction
	// (from either node to the other — "A supersedes B" and "B resolved_by A"
	// are both valid resolution phrasings). This implements Option A from the
	// story: the resolution is expressed as a graph action (an additional
	// edge), not a new DB column. Checking both directions and requiring the
	// edge to connect this exact pair (not an unrelated third node) avoids
	// both under- and over-excluding contradicts pairs.
	rows, err := s.db.Query(`
		SELECT a.id, a.label, a.description, a.why_matters, a.domain,
		       a.created_at, a.updated_at, a.occurred_at, a.archived_at, a.tags, a.node_kind,
		       b.id, b.label, b.description, b.why_matters, b.domain,
		       b.created_at, b.updated_at, b.occurred_at, b.archived_at, b.tags, b.node_kind
		FROM edges e
		JOIN nodes a ON a.id = e.from_node AND a.archived_at IS NULL
		JOIN nodes b ON b.id = e.to_node   AND b.archived_at IS NULL
		WHERE e.relationship = 'contradicts'
		  AND NOT EXISTS (
		      SELECT 1 FROM edges r
		       WHERE r.relationship IN ('resolved_by', 'supersedes')
		         AND (
		             (r.from_node = a.id AND r.to_node = b.id) OR
		             (r.from_node = b.id AND r.to_node = a.id)
		         )
		  )`)
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

	// ── Rule 7: connected placeholder whose target appears resolved ──────────────
	// A candidate matches when ALL of:
	//   a) node_kind='goal' OR label contains "Story needed:", "TODO:", or "Placeholder:" (case-insensitive)
	//   b) Has at least one outbound edge (connects_to OR led_to) to a node whose
	//      label OR description contains a completion signal
	//   c) The placeholder node's own label/description does NOT itself contain a completion signal
	const completionSignals = `(LOWER(n2.label) LIKE '%complete%' OR LOWER(n2.label) LIKE '%shipped%' OR ` +
		`LOWER(n2.label) LIKE '%resolved%' OR LOWER(n2.label) LIKE '%done%' OR LOWER(n2.label) LIKE '%closed%' OR ` +
		`LOWER(n2.description) LIKE '%complete%' OR LOWER(n2.description) LIKE '%shipped%' OR ` +
		`LOWER(n2.description) LIKE '%resolved%' OR LOWER(n2.description) LIKE '%done%' OR LOWER(n2.description) LIKE '%closed%')`
	const phKindOrLabel = `(n.node_kind = 'goal' OR LOWER(n.label) LIKE '%story needed:%' OR ` +
		`LOWER(n.label) LIKE '%todo:%' OR LOWER(n.label) LIKE '%placeholder:%')`
	const phNoSelfSignal = `NOT (LOWER(n.label) LIKE '%complete%' OR LOWER(n.label) LIKE '%shipped%' OR ` +
		`LOWER(n.label) LIKE '%resolved%' OR LOWER(n.label) LIKE '%done%' OR LOWER(n.label) LIKE '%closed%' OR ` +
		`LOWER(n.description) LIKE '%complete%' OR LOWER(n.description) LIKE '%shipped%' OR ` +
		`LOWER(n.description) LIKE '%resolved%' OR LOWER(n.description) LIKE '%done%' OR LOWER(n.description) LIKE '%closed%')`
	const hasResolvingOutbound = `EXISTS (
		SELECT 1 FROM edges e7
		JOIN nodes n2 ON n2.id = e7.to_node AND n2.archived_at IS NULL
		WHERE e7.from_node = n.id
		  AND (e7.relationship = 'connects_to' OR e7.relationship = 'led_to' OR e7.relationship = 'leads_to')
		  AND ` + completionSignals + `)`

	var rows7 *sql.Rows
	if domain != "" {
		rows7, err = s.db.Query(
			`SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind
			 FROM nodes n
			 WHERE n.archived_at IS NULL
			   AND n.domain = ?
			   AND `+phKindOrLabel+`
			   AND `+phNoSelfSignal+`
			   AND `+hasResolvingOutbound,
			domain)
	} else {
		rows7, err = s.db.Query(
			`SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind
			 FROM nodes n
			 WHERE n.archived_at IS NULL
			   AND ` + phKindOrLabel + `
			   AND ` + phNoSelfSignal + `
			   AND ` + hasResolvingOutbound)
	}
	if err != nil {
		return nil, err
	}
	for rows7.Next() {
		n, err := scanNodeRow(rows7)
		if err != nil {
			rows7.Close()
			return nil, err
		}
		add(n, nil, "connected placeholder — target appears resolved; revise or archive placeholder")
	}
	rows7.Close()
	if err = rows7.Err(); err != nil {
		return nil, err
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
