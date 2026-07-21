package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CandidateSimilarityFloor is the maximum cosine distance for a node to be
// considered a meaningful filing-time candidate connection. vec_distance_cosine
// returns values in [0, 2]; 0 = identical, 2 = opposite.
// Candidates whose embedding distance exceeds this floor are suppressed — the
// server returns fewer than limit (even zero) rather than handing back noise.
// Chosen to be less strict than semanticDistanceThreshold (0.3) used in search,
// because suggest_connections tolerates a looser signal.
// Exported so that audit.go (conflicts mode) can reuse the same threshold.
const CandidateSimilarityFloor = 0.5

// crossDomainAffinityBoost is the fractional improvement a cross-domain
// candidate's distance must achieve over the best same-domain candidate before
// it is included. A value of 0.20 means the cross-domain node must be at least
// 20% closer (lower distance) than the best same-domain match.
const crossDomainAffinityBoost = 0.20

// PathResult holds the shortest path between two nodes and all edges
// incident to any node on that path (spine edges + context branches).
type PathResult struct {
	Path  []Node `json:"path"`
	Edges []Edge `json:"edges"`
}

// FindPath returns the shortest path between fromID and toID using a BFS
// traversal of edges. Only live (non-archived) nodes are traversed; archived
// nodes act as walls. maxDepth caps the search at that many hops (hard limit: 6).
// Returns an empty PathResult (no error) when no path exists.
func (s *Store) FindPath(fromID, toID string, maxDepth int) (*PathResult, error) {
	if maxDepth <= 0 || maxDepth > 6 {
		maxDepth = 6
	}
	if fromID == toID {
		// Trivial: source == destination.
		n, err := s.GetNode(fromID)
		if err != nil {
			return nil, err
		}
		return &PathResult{Path: []Node{n.Node}, Edges: nil}, nil
	}

	// BFS: each entry is a path (slice of node IDs) from fromID to the frontier.
	type path struct {
		nodes []string
		edges []string // edge IDs in order
	}
	queue := []path{{nodes: []string{fromID}}}
	visited := map[string]bool{fromID: true}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		tail := cur.nodes[len(cur.nodes)-1]
		if len(cur.nodes)-1 >= maxDepth {
			continue // depth limit reached without finding target
		}

		// Fetch all edges from or to the current tail node (undirected traversal).
		rows, err := s.db.Query(`
			SELECT e.id, e.from_node, e.to_node, e.relationship, e.narrative, e.created_at
			FROM edges e
			JOIN nodes nf ON nf.id = e.from_node AND nf.archived_at IS NULL
			JOIN nodes nt ON nt.id = e.to_node   AND nt.archived_at IS NULL
			WHERE e.from_node = ? OR e.to_node = ?`, tail, tail)
		if err != nil {
			return nil, err
		}
		var neighbours []struct {
			edge      Edge
			neighbour string
		}
		for rows.Next() {
			var e Edge
			if err := rows.Scan(&e.ID, &e.FromNode, &e.ToNode, &e.Relationship, &e.Narrative, &e.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			next := e.ToNode
			if next == tail {
				next = e.FromNode
			}
			neighbours = append(neighbours, struct {
				edge      Edge
				neighbour string
			}{e, next})
		}
		rows.Close()

		for _, nb := range neighbours {
			if visited[nb.neighbour] {
				continue
			}
			newPath := path{
				nodes: append(append([]string{}, cur.nodes...), nb.neighbour),
				edges: append(append([]string{}, cur.edges...), nb.edge.ID),
			}
			if nb.neighbour == toID {
				// Found it — materialise the result.
				return s.materialisePath(newPath.nodes, newPath.edges)
			}
			visited[nb.neighbour] = true
			queue = append(queue, newPath)
		}
	}
	return &PathResult{}, nil // no path found
}

// materialisePath fetches full Node structs for the path and all edges
// incident to any node on the path (spine edges + context branches).
func (s *Store) materialisePath(nodeIDs, edgeIDs []string) (*PathResult, error) {
	_ = edgeIDs // we now fetch all incident edges instead of just spine edges
	nodes := make([]Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nwe, err := s.GetNode(id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, nwe.Node)
	}

	// Build placeholder list for IN clause.
	ph, phArgs := inClause(nodeIDs)
	args := append(phArgs, phArgs...)

	rows, err := s.db.Query(
		`SELECT `+edgeSelectColumns+` FROM edges
		 WHERE from_node IN (`+ph+`) OR to_node IN (`+ph+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &PathResult{Path: nodes, Edges: edges}, nil
}

type ConnectionResult struct {
	From  *Node  `json:"from"`
	To    *Node  `json:"to"`
	Edges []Edge `json:"edges"`
}

// bestMatch returns the first node whose label or description best matches the term.
func (s *Store) bestMatch(term, domain string) (*Node, error) {
	domain = s.ResolveAlias(domain)
	q := "%" + term + "%"
	var row *sql.Row
	if domain != "" {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			domain, q, q, q, q, q,
		)
	} else {
		row = s.db.QueryRow(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY CASE WHEN label LIKE ? THEN 0 ELSE 1 END, updated_at DESC LIMIT 1`,
			q, q, q, q, q,
		)
	}
	var n Node
	var oa sql.NullTime
	var aa sql.NullTime
	err := row.Scan(&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain, &n.CreatedAt, &n.UpdatedAt, &oa, &aa, &n.Tags, &n.NodeKind)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.OccurredAt = nullTimeToPtr(oa)
	n.ArchivedAt = nullTimeToPtr(aa)
	return &n, nil
}

func (s *Store) FindConnections(fromTerm, toTerm, domain string) (*ConnectionResult, error) {
	return s.FindConnectionsResolved("", fromTerm, "", toTerm, domain)
}

// FindConnectionsResolved returns direct edges between two nodes. Each side is
// resolved by id (exact live node) or label (bestMatch fuzzy search).
func (s *Store) FindConnectionsResolved(fromID, fromLabel, toID, toLabel, domain string) (*ConnectionResult, error) {
	from, err := s.resolveConnectionEndpoint(fromID, fromLabel, domain)
	if err != nil {
		return nil, err
	}
	to, err := s.resolveConnectionEndpoint(toID, toLabel, domain)
	if err != nil {
		return nil, err
	}
	return s.connectionResultBetween(from, to)
}

func (s *Store) resolveConnectionEndpoint(id, label, domain string) (*Node, error) {
	if id != "" {
		nwe, err := s.GetNode(id)
		if err != nil {
			return nil, err
		}
		return &nwe.Node, nil
	}
	if label != "" {
		n, err := s.bestMatch(label, domain)
		if err != nil {
			return nil, err
		}
		if n == nil {
			return nil, fmt.Errorf("no live memory matched label %q", label)
		}
		return n, nil
	}
	return nil, fmt.Errorf("id or label is required")
}

func (s *Store) connectionResultBetween(from, to *Node) (*ConnectionResult, error) {
	rows, err := s.db.Query(
		`SELECT `+edgeSelectColumns+` FROM edges
		 WHERE (from_node = ? AND to_node = ?) OR (from_node = ? AND to_node = ?)`,
		from.ID, to.ID, to.ID, from.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ConnectionResult{From: from, To: to, Edges: edges}, nil
}

// GetDomainGraph returns the live nodes and the edges between them for a domain.
// Nodes are sorted by edge count descending so the most-connected appear first;
// the result is capped at limit (default 40, max 100). truncated is true when
// the full node set was larger than limit. nodesTotal is the full domain node
// count before any truncation; edgesTotal is the count of intra-domain edges
// across all nodes (not just the shown subset).
func (s *Store) GetDomainGraph(domain string, limit int) (nodes []Node, edges []Edge, truncated bool, nodesTotal int, edgesTotal int, err error) {
	domain = s.ResolveAlias(domain)
	if limit <= 0 {
		limit = 40
	}
	if limit > 100 {
		limit = 100
	}

	// Step 1: all live nodes in the domain.
	rows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE archived_at IS NULL AND domain = ?`, domain)
	if err != nil {
		return
	}
	var allNodes []Node
	allNodes, err = scanNodeRows(rows)
	rows.Close()
	if err != nil {
		return
	}
	if len(allNodes) == 0 {
		return
	}

	// Step 2: count edges (from + to) per node to rank by connectivity.
	ids := mapSlice(allNodes, func(n Node) string { return n.ID })
	ph, phArgs := inClause(ids)
	rankArgs := append(phArgs, phArgs...)
	ecRows, ecErr := s.db.Query(
		`SELECT id_val, COUNT(*) FROM (`+
			`SELECT from_node AS id_val FROM edges WHERE from_node IN (`+ph+`) `+
			`UNION ALL `+
			`SELECT to_node AS id_val FROM edges WHERE to_node IN (`+ph+`)`+
			`) GROUP BY id_val`,
		rankArgs...,
	)
	counts := make(map[string]int)
	if ecErr == nil {
		for ecRows.Next() {
			var id string
			var cnt int
			if ecRows.Scan(&id, &cnt) == nil {
				counts[id] = cnt
			}
		}
		ecRows.Close()
	}

	// Step 3: record totals before any truncation, then sort and truncate.
	nodesTotal = len(allNodes)
	// Count intra-domain edges (both endpoints in domain) across the full node set.
	if nodesTotal > 0 {
		_ = s.db.QueryRow(
			`SELECT COUNT(*) FROM edges WHERE from_node IN (`+ph+`) AND to_node IN (`+ph+`)`,
			rankArgs...,
		).Scan(&edgesTotal)
	}

	sort.Slice(allNodes, func(i, j int) bool {
		return counts[allNodes[i].ID] > counts[allNodes[j].ID]
	})
	if len(allNodes) > limit {
		allNodes = allNodes[:limit]
		truncated = true
	}
	nodes = allNodes

	// Step 4: fetch edges whose both endpoints are in the result set.
	ph2, nodePhArgs := inClause(mapSlice(nodes, func(n Node) string { return n.ID }))
	edgeArgs := append(nodePhArgs, nodePhArgs...)
	eRows, eErr := s.db.Query(
		`SELECT `+edgeSelectColumns+` FROM edges `+
			`WHERE from_node IN (`+ph2+`) AND to_node IN (`+ph2+`)`,
		edgeArgs...,
	)
	if eErr != nil {
		err = eErr
		return
	}
	for eRows.Next() {
		e, scanErr := scanEdge(eRows)
		if scanErr == nil {
			edges = append(edges, e)
		}
	}
	eRows.Close()
	err = eRows.Err()
	return
}

// ── edge suggestions ──────────────────────────────────────────────────────────

// EdgeSuggestion is a candidate connection returned by SuggestEdges.
type EdgeSuggestion struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
	Domain string `json:"domain"`
}

// SuggestEdges returns up to limit candidate connections for the given node.
// When sqlite-vec embeddings are available for the node, it uses semantic
// nearest-neighbour search with a similarity floor (candidateSimilarityFloor)
// and domain affinity (same-domain preferred; cross-domain candidates only when
// their distance is at least crossDomainAffinityBoost better than the best
// same-domain match). Falls back to keyword matching (tag overlap + label words)
// when embeddings are unavailable.
// It never creates edges — the caller must use AddEdge to act on suggestions.
func (s *Store) SuggestEdges(id string, limit int) ([]EdgeSuggestion, error) {
	if limit <= 0 {
		limit = 5
	}

	// Fetch the target node.
	var targetLabel, targetDomain, targetTags, targetDesc, targetWhy string
	if err := s.db.QueryRow(
		`SELECT label, domain, tags, description, why_matters FROM nodes WHERE id = ? AND archived_at IS NULL`, id,
	).Scan(&targetLabel, &targetDomain, &targetTags, &targetDesc, &targetWhy); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("node not found: %s", id)
		}
		return nil, err
	}

	// Try semantic path when the node has an embedding stored.
	// Use the enriched variant so the reason field surfaces keyword overlap.
	if s.vecAvailable {
		if results, ok, err := s.suggestEdgesSemanticEnriched(id, targetLabel, targetDomain, targetDesc, targetWhy, targetTags, limit); err == nil && ok {
			return results, nil
		}
	}

	// Keyword fallback: extract meaningful keywords from label + tags.
	return s.suggestEdgesKeyword(id, targetLabel, targetDomain, targetTags, limit)
}

// suggestEdgesSemantic finds candidate connections using embedding cosine
// distance. Returns (results, true, nil) on success, (nil, false, nil) when
// no embedding exists for the node (caller falls back to keyword path).
func (s *Store) suggestEdgesSemantic(id, label, domain, description, whyMatters string, limit int) ([]EdgeSuggestion, bool, error) {
	// Look up the stored embedding blob for this node and pass it directly
	// to the sqlite-vec distance function.
	var blob []byte
	if err := s.db.QueryRow(
		`SELECT embedding FROM node_embeddings WHERE node_id = ?`, id,
	).Scan(&blob); err != nil {
		// No embedding yet — fall back to keyword path.
		return nil, false, nil
	}
	if len(blob) == 0 {
		return nil, false, nil
	}

	// Fetch up to limit*4 nearest neighbours across all domains (excluding self
	// and already-connected nodes). We over-fetch to have room to apply domain
	// affinity filtering before capping at limit.
	fetch := limit * 4
	rows, err := s.db.Query(`
		SELECT n.id, n.label, n.domain,
		       vec_distance_cosine(e.embedding, ?) AS dist
		FROM node_embeddings e
		JOIN nodes n ON n.id = e.node_id
		WHERE n.id != ?
		  AND n.archived_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM edges
		       WHERE (from_node = ? AND to_node = n.id)
		          OR (from_node = n.id AND to_node = ?)
		  )
		ORDER BY dist ASC
		LIMIT ?`, blob, id, id, id, fetch)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	type candidate struct {
		id     string
		label  string
		domain string
		dist   float64
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.label, &c.domain, &c.dist); err != nil {
			return nil, false, err
		}
		// Hard floor: suppress anything beyond the similarity floor.
		if c.dist > CandidateSimilarityFloor {
			break // results are ordered by dist ASC; all following are worse
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	if len(all) == 0 {
		// No candidates within floor — return empty slice rather than noise.
		return []EdgeSuggestion{}, true, nil
	}

	// Domain affinity: find best same-domain distance.
	bestSameDomainDist := float64(2.0) // worst possible cosine distance
	haveSameDomain := false
	for _, c := range all {
		if c.domain == domain {
			haveSameDomain = true
			if c.dist < bestSameDomainDist {
				bestSameDomainDist = c.dist
			}
		}
	}

	// Filter: include same-domain candidates; include cross-domain candidates
	// only when their distance is crossDomainAffinityBoost better than the
	// best same-domain match. With no same-domain candidate to calibrate
	// against, there is no baseline "normal" to compare a cross-domain match
	// to, so cross-domain candidates are excluded entirely rather than
	// admitted by a relaxed-to-uselessness threshold (2.0*(1-boost), which
	// every candidate already inside the outer floor would trivially pass).
	threshold := bestSameDomainDist * (1.0 - crossDomainAffinityBoost)
	var kept []candidate
	for _, c := range all {
		if c.domain == domain {
			kept = append(kept, c)
		} else if haveSameDomain && c.dist <= threshold {
			kept = append(kept, c)
		}
	}

	// Cap at limit.
	if len(kept) > limit {
		kept = kept[:limit]
	}

	// Enrich reasons: run keyword matching to surface shared tags/label words
	// so callers can see the semantic signal alongside the embedding signal.
	// Compute the keywords from the filing node's label and tags for comparison.
	// (targetTags and targetLabel are not in scope here; we pass them in the
	// caller's embedded text. A lightweight re-derive from label is sufficient.)
	result := make([]EdgeSuggestion, len(kept))
	for i, c := range kept {
		var reason string
		if c.domain != domain {
			reason = "semantically similar (cross-domain)"
		} else {
			reason = "semantically similar"
		}
		result[i] = EdgeSuggestion{ID: c.id, Label: c.label, Reason: reason, Domain: c.domain}
	}
	return result, true, nil
}

// suggestEdgesSemanticWithKeywords is like suggestEdgesSemantic but also runs
// keyword matching to enrich the reason field. The reason will mention shared
// tags or label words when they exist.
func (s *Store) suggestEdgesSemanticEnriched(id, label, domain, description, whyMatters, tags string, limit int) ([]EdgeSuggestion, bool, error) {
	results, ok, err := s.suggestEdgesSemantic(id, label, domain, description, whyMatters, limit)
	if err != nil || !ok || len(results) == 0 {
		return results, ok, err
	}
	// Enrich each result's reason with keyword overlap information.
	keywords := suggestKeywords(label, tags)
	for i := range results {
		var matchedTags, matchedLabels []string
		cLabelLower := strings.ToLower(results[i].Label)
		seen := map[string]bool{}
		for _, kw := range keywords {
			if seen[kw] {
				continue
			}
			if strings.Contains(cLabelLower, kw) {
				matchedLabels = append(matchedLabels, kw)
				seen[kw] = true
			}
		}
		// Fetch candidate tags from DB for tag overlap check.
		var cTags string
		s.db.QueryRow(`SELECT tags FROM nodes WHERE id = ?`, results[i].ID).Scan(&cTags)
		cTagsLower := strings.ToLower(cTags)
		for _, kw := range keywords {
			if seen[kw] {
				continue
			}
			if strings.Contains(cTagsLower, kw) {
				matchedTags = append(matchedTags, kw)
				seen[kw] = true
			}
		}
		if len(matchedTags) > 0 || len(matchedLabels) > 0 {
			var parts []string
			if strings.Contains(results[i].Reason, "cross-domain") {
				parts = append(parts, "semantically similar (cross-domain)")
			} else {
				parts = append(parts, "semantically similar")
			}
			if len(matchedTags) > 0 {
				parts = append(parts, "shares tags: "+strings.Join(matchedTags, " "))
			}
			if len(matchedLabels) > 0 {
				parts = append(parts, "similar label words: "+strings.Join(matchedLabels, " "))
			}
			results[i].Reason = strings.Join(parts, "; ")
		}
	}
	return results, true, nil
}

// suggestEdgesKeyword finds candidate connections using tag overlap and label
// word matching. This is the fallback path when no embedding is available.
func (s *Store) suggestEdgesKeyword(id, targetLabel, targetDomain, targetTags string, limit int) ([]EdgeSuggestion, error) {
	// Extract meaningful keywords from label + tags (lowercased, deduplicated,
	// stop-words and very short words removed).
	keywords := suggestKeywords(targetLabel, targetTags)
	if len(keywords) == 0 {
		return []EdgeSuggestion{}, nil
	}

	// Fetch all other live nodes in the same domain (cap at 200 to bound work).
	rows, err := s.db.Query(
		`SELECT id, label, tags FROM nodes
		 WHERE id != ? AND domain = ? AND archived_at IS NULL
		 ORDER BY updated_at DESC LIMIT 200`,
		id, targetDomain,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		id     string
		label  string
		score  int
		reason string
	}

	var candidates []scored
	for rows.Next() {
		var cid, clabel, ctags string
		rows.Scan(&cid, &clabel, &ctags)

		cLabelLower := strings.ToLower(clabel)
		cTagsLower := strings.ToLower(ctags)

		var matchedTags, matchedLabels []string
		seen := map[string]bool{}
		for _, kw := range keywords {
			if seen[kw] {
				continue
			}
			if strings.Contains(cTagsLower, kw) {
				matchedTags = append(matchedTags, kw)
				seen[kw] = true
			} else if strings.Contains(cLabelLower, kw) {
				matchedLabels = append(matchedLabels, kw)
				seen[kw] = true
			}
		}

		score := len(matchedTags)*2 + len(matchedLabels)
		if score == 0 {
			continue
		}

		var reasons []string
		if len(matchedTags) > 0 {
			reasons = append(reasons, "shares tags: "+strings.Join(matchedTags, " "))
		}
		if len(matchedLabels) > 0 {
			reasons = append(reasons, "similar label words: "+strings.Join(matchedLabels, " "))
		}
		candidates = append(candidates, scored{
			id:     cid,
			label:  clabel,
			score:  score,
			reason: strings.Join(reasons, "; "),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result := make([]EdgeSuggestion, len(candidates))
	for i, c := range candidates {
		result[i] = EdgeSuggestion{ID: c.id, Label: c.label, Reason: c.reason, Domain: targetDomain}
	}
	return result, nil
}

// suggestKeywords extracts lowercase, deduplicated, meaningful words from label
// and tags, skipping common stop words and words shorter than 3 characters.
func suggestKeywords(label, tags string) []string {
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true,
		"of": true, "in": true, "to": true, "is": true, "it": true,
		"be": true, "for": true, "on": true, "at": true, "by": true,
		"we": true, "as": true, "so": true, "do": true, "not": true,
		"are": true, "was": true, "has": true, "had": true, "its": true,
	}
	seen := map[string]bool{}
	var keywords []string
	addWords := func(text string) {
		for _, w := range strings.Fields(strings.ToLower(text)) {
			// Strip any leading/trailing punctuation or symbol, not just the
			// ASCII set — em-dash, en-dash, curly quotes, ellipsis, etc. would
			// otherwise survive as a standalone "word" of 3+ UTF-8 bytes.
			w = strings.TrimFunc(w, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsNumber(r)
			})
			if utf8.RuneCountInString(w) < 3 || stopWords[w] || seen[w] {
				continue
			}
			seen[w] = true
			keywords = append(keywords, w)
		}
	}
	addWords(tags) // tags first — higher signal
	addWords(label)
	return keywords
}

// GetNodeNeighbourhood returns the target node, all live nodes directly
// connected to it (depth 1), and all edges between those nodes.
// Returns an error if the node does not exist or is archived.
func (s *Store) GetNodeNeighbourhood(nodeID string) (nodes []Node, edges []Edge, err error) {
	var target Node
	var oa, aa sql.NullTime
	err = s.db.QueryRow(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE id = ? AND archived_at IS NULL`, nodeID,
	).Scan(&target.ID, &target.Label, &target.Description, &target.WhyMatters, &target.Domain,
		&target.CreatedAt, &target.UpdatedAt, &oa, &aa, &target.Tags, &target.NodeKind)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return nil, nil, err
	}
	target.OccurredAt = nullTimeToPtr(oa)
	target.ArchivedAt = nullTimeToPtr(aa)

	// Collect IDs of all direct neighbours via edges.
	eRows, err := s.db.Query(
		`SELECT CASE WHEN from_node = ? THEN to_node ELSE from_node END AS neighbour_id
		 FROM edges WHERE from_node = ? OR to_node = ?`, nodeID, nodeID, nodeID)
	if err != nil {
		return nil, nil, err
	}
	neighbourIDs := map[string]bool{}
	for eRows.Next() {
		var id string
		if scanErr := eRows.Scan(&id); scanErr != nil {
			eRows.Close()
			return nil, nil, scanErr
		}
		neighbourIDs[id] = true
	}
	eRows.Close()
	if err = eRows.Err(); err != nil {
		return nil, nil, err
	}

	// Build the full neighbourhood ID list (target + neighbours).
	allIDs := make([]string, 0, len(neighbourIDs)+1)
	allIDs = append(allIDs, nodeID)
	for id := range neighbourIDs {
		allIDs = append(allIDs, id)
	}

	// Fetch all live nodes in the neighbourhood.
	ph, nArgs := inClause(allIDs)
	nRows, err := s.db.Query(
		`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind
		 FROM nodes WHERE archived_at IS NULL AND id IN (`+ph+`)`, nArgs...)
	if err != nil {
		return nil, nil, err
	}
	var neighbourhood []Node
	neighbourhood, err = scanNodeRows(nRows)
	nRows.Close()
	if err != nil {
		return nil, nil, err
	}
	nodes = append(nodes, neighbourhood...)

	// Fetch all edges where both endpoints are in the live neighbourhood set.
	eArgs := append(nArgs, nArgs...)
	edgeRows, err := s.db.Query(
		`SELECT `+edgeSelectColumns+`
		 FROM edges WHERE from_node IN (`+ph+`) AND to_node IN (`+ph+`)`, eArgs...)
	if err != nil {
		return nil, nil, err
	}
	for edgeRows.Next() {
		e, scanErr := scanEdge(edgeRows)
		if scanErr != nil {
			edgeRows.Close()
			return nil, nil, scanErr
		}
		edges = append(edges, e)
	}
	edgeRows.Close()
	err = edgeRows.Err()
	return
}

// neighbourhoodIDs performs a BFS from nodeID for depth hops, clipping at the
// anchor node's domain boundary (cross-domain edges are not followed). Returns
// all visited IDs (including the anchor) and the anchor's domain.
func (s *Store) neighbourhoodIDs(nodeID string, depth int) ([]string, string, error) {
	var anchorDomain string
	err := s.db.QueryRow(
		`SELECT domain FROM nodes WHERE id = ? AND archived_at IS NULL`, nodeID,
	).Scan(&anchorDomain)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("memory not found: %s", nodeID)
	}
	if err != nil {
		return nil, "", err
	}

	visited := map[string]bool{nodeID: true}
	frontier := []string{nodeID}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		ph := strings.Repeat("?,", len(frontier))
		ph = ph[:len(ph)-1]
		// args: frontier IDs (for from_node IN), frontier IDs again (for to_node IN), domain
		args := make([]interface{}, len(frontier)*2+1)
		for i, id := range frontier {
			args[i] = id
			args[len(frontier)+i] = id
		}
		args[len(frontier)*2] = anchorDomain

		rows, err := s.db.Query(
			`SELECT DISTINCT n.id
			 FROM edges e
			 JOIN nodes n ON (
			     (e.from_node IN (`+ph+`) AND n.id = e.to_node)
			     OR
			     (e.to_node IN (`+ph+`) AND n.id = e.from_node)
			 )
			 WHERE n.domain = ?
			   AND n.archived_at IS NULL`, args...)
		if err != nil {
			return nil, "", err
		}
		var next []string
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				rows.Close()
				return nil, "", scanErr
			}
			if !visited[id] {
				visited[id] = true
				next = append(next, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, "", err
		}
		frontier = next
	}

	ids := make([]string, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	return ids, anchorDomain, nil
}

// MisdomainCandidate is the top workspace KNN match suggesting a different domain
// for a newly-created domain's first memory.
type MisdomainCandidate struct {
	SuggestedDomain   string
	SuggestedMemoryID string
}

// FindMisdomainCandidate runs workspace-wide KNN from nodeID's embedding with
// only the similarity floor applied (no domain-affinity reranking). Returns nil
// when no embedding exists, vec is unavailable, or no cross-domain match clears
// the floor.
func (s *Store) FindMisdomainCandidate(nodeID, requestedDomain string) (*MisdomainCandidate, error) {
	if !s.vecAvailable {
		return nil, nil
	}
	requestedDomain = s.ResolveAlias(requestedDomain)
	var blob []byte
	if err := s.db.QueryRow(`SELECT embedding FROM node_embeddings WHERE node_id = ?`, nodeID).Scan(&blob); err != nil {
		return nil, nil
	}
	if len(blob) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT n.id, n.domain, vec_distance_cosine(e.embedding, ?) AS dist
		FROM node_embeddings e
		JOIN nodes n ON n.id = e.node_id
		WHERE n.id != ?
		  AND n.archived_at IS NULL
		ORDER BY dist ASC
		LIMIT 20`, blob, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, domain string
		var dist float64
		if err := rows.Scan(&id, &domain, &dist); err != nil {
			return nil, err
		}
		if dist > CandidateSimilarityFloor {
			break
		}
		if domain != requestedDomain {
			return &MisdomainCandidate{SuggestedDomain: domain, SuggestedMemoryID: id}, nil
		}
	}
	return nil, rows.Err()
}
