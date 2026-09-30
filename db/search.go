package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// NodeResult is a single search result. SemanticDistance is set when the
// result was matched by vector-distance search; it is nil for LIKE results.
type NodeResult struct {
	Node
	SemanticDistance *float64 `json:"semantic_distance,omitempty"`
}

type SearchResult struct {
	Nodes             []NodeResult `json:"nodes"`
	Edges             []Edge       `json:"edges"`
	Truncated         bool         `json:"truncated,omitempty"`
	SemanticAttempted bool         `json:"semantic_attempted,omitempty"`
	SemanticBestDist  *float64     `json:"semantic_best_dist,omitempty"`
}

func (st *Store) SearchNodes(query, domain string, limit int, memoryID string, nodeKinds []string) (*SearchResult, error) {
	domain = st.ResolveAlias(domain)

	if strings.TrimSpace(query) == "" && len(nodeKinds) > 0 {
		return st.listNodesByKind(domain, nodeKinds, limit, memoryID)
	}

	var allowedIDs []string
	if memoryID != "" {
		ids, _, err := st.neighbourhoodIDs(memoryID, 2)
		if err != nil {
			return nil, err
		}
		allowedIDs = ids
	}

	// Try semantic search when sqlite-vec is loaded.
	if st.vecAvailable {
		queryText := queryPrefix() + query
		embedding, err := embed(queryText)
		if err == nil && len(embedding) > 0 {
			result, err := st.searchNodesSemantic(query, domain, limit, embedding, allowedIDs, nodeKinds)
			if err == nil {
				return result, nil
			}
			log.Printf("[memoryweb] semantic search failed: %v; falling back to text search", err)
		}
	}

	return st.searchNodesLike(query, domain, limit, allowedIDs, nodeKinds, true)
}

// listNodesByKind returns live nodes filtered by node_kind, ordered by updated_at DESC.
func (st *Store) listNodesByKind(domain string, nodeKinds []string, limit int, memoryID string) (*SearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	fetch := limit + 1

	var allowedIDs []string
	if memoryID != "" {
		ids, _, err := st.neighbourhoodIDs(memoryID, 2)
		if err != nil {
			return nil, err
		}
		allowedIDs = ids
	}

	conds := []string{"archived_at IS NULL"}
	args := []interface{}{}
	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	if len(allowedIDs) > 0 {
		placeholders, placeholderArgs := inClause(allowedIDs)
		conds = append(conds, "id IN ("+placeholders+")")
		args = append(args, placeholderArgs...)
	}
	args = append(args, fetch)

	query := `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes WHERE ` +
		strings.Join(conds, " AND ") + ` ORDER BY updated_at DESC LIMIT ?`

	rows, err := st.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	nodes, err := scanNodeRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	truncated := len(nodes) > limit
	if truncated {
		nodes = nodes[:limit]
	}
	results := wrapNodes(nodes)
	edges, err := collectEdges(st, nodes)
	if err != nil {
		return nil, fmt.Errorf("collectEdges: %w", err)
	}
	return &SearchResult{Nodes: results, Edges: edges, Truncated: truncated}, nil
}

// SearchNodesExact performs a pure substring (LIKE) search, bypassing semantic
// ranking entirely. Use this when the query contains a unique identifier, ticket
// number, or short code that is known to appear verbatim in the stored content.
// Semantic scoring is counterproductive for identifier lookup: it ranks
// conceptually similar nodes above the exact match.
func (st *Store) SearchNodesExact(query, domain string, limit int, memoryID string, nodeKinds []string) (*SearchResult, error) {
	domain = st.ResolveAlias(domain)

	if strings.TrimSpace(query) == "" && len(nodeKinds) > 0 {
		return st.listNodesByKind(domain, nodeKinds, limit, memoryID)
	}

	var allowedIDs []string
	if memoryID != "" {
		ids, _, err := st.neighbourhoodIDs(memoryID, 2)
		if err != nil {
			return nil, err
		}
		allowedIDs = ids
	}

	return st.searchNodesLike(query, domain, limit, allowedIDs, nodeKinds, false)
}

// semanticThreshold returns the configured cosine-distance cutoff for semantic
// results. When MEMORYWEB_SEMANTIC_THRESHOLD is set to a positive float it
// acts as a hard cap — rows beyond that distance are discarded and the LIKE
// fallback runs. When unset (the default) no cap is applied: all rows up to
// the query limit are returned, ranked purely by distance.
//
// vec_distance_cosine returns values in [0, 2]: 0 = identical, 2 = opposite.
func semanticThreshold() (float64, bool) {
	if v := os.Getenv("MEMORYWEB_SEMANTIC_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f, true
		}
	}
	return 0, false
}

// queryEmbeddingTable runs a KNN search against the named vec0 embedding table
// and returns NodeResults ordered by cosine distance ASC. When
// MEMORYWEB_SEMANTIC_THRESHOLD is set it stops early at that distance;
// otherwise it returns all rows up to fetch. Applies domain and node_kind
// filters via JOIN.
//
// The second return value is the first (smallest) cosine distance scanned
// before any threshold was applied — nil when the table returned no rows at
// all. This allows callers to produce meaningful diagnostic logs even when
// MEMORYWEB_SEMANTIC_THRESHOLD suppresses the entire result set.
func (st *Store) queryEmbeddingTable(tableName, domain string, fetch int, blob []byte, nodeKinds []string) ([]NodeResult, *float64, error) {
	switch tableName {
	case "node_embeddings", "node_label_embeddings", "node_whymatters_embeddings":
	default:
		return nil, nil, fmt.Errorf("queryEmbeddingTable: unknown table %q", tableName)
	}

	conds := []string{"n.archived_at IS NULL"}
	args := []any{blob}
	if domain != "" {
		conds = append(conds, "n.domain = ?")
		args = append(args, domain)
	}
	conds, args = nodeKindFilter("n.node_kind", nodeKinds, conds, args)
	args = append(args, fetch)

	q := `SELECT n.id, n.label, n.description, n.why_matters, n.domain,
		       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind,
		       vec_distance_cosine(e.embedding, ?) AS dist
		FROM ` + tableName + ` e
		JOIN nodes n ON n.id = e.node_id
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY dist ASC
		LIMIT ?`

	rows, err := st.db.Query(q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	thresh, hasThresh := semanticThreshold()
	var firstDist *float64
	var results []NodeResult
	for rows.Next() {
		var n Node
		var occurredAt, archivedAt sql.NullTime
		var dist float64
		if err := rows.Scan(
			&n.ID, &n.Label, &n.Description, &n.WhyMatters, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &occurredAt, &archivedAt, &n.Tags, &n.NodeKind,
			&dist,
		); err != nil {
			return nil, firstDist, err
		}
		if firstDist == nil {
			d := dist
			firstDist = &d
		}
		if hasThresh && dist > thresh {
			break
		}
		n.OccurredAt = nullTimeToPtr(occurredAt)
		n.ArchivedAt = nullTimeToPtr(archivedAt)
		d := dist
		results = append(results, NodeResult{Node: n, SemanticDistance: &d})
	}
	return results, firstDist, rows.Err()
}

// searchNodesSemantic ranks nodes by cosine distance between the query
// embedding and stored node embeddings, then falls back to LIKE if no
// semantic results are found within the relevance threshold.
// When per-field embedding tables are available, queries label and why_matters
// separately and uses the minimum distance per node for ranking.
func (st *Store) searchNodesSemantic(query, domain string, limit int, embedding []float32, allowedIDs, nodeKinds []string) (*SearchResult, error) {
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, err
	}

	var results []NodeResult
	var bestDist *float64

	if st.vecFieldsAvailable {
		// Per-field path: query label, why_matters, and legacy tables; merge by min dist.
		// Fetching extra rows from each table to account for overlap after merge.
		fieldFetch := (limit + 1) * 2
		labelResults, labelFirst, labelErr := st.queryEmbeddingTable("node_label_embeddings", domain, fieldFetch, blob, nodeKinds)
		if labelErr != nil {
			log.Printf("[memoryweb] per-field search: node_label_embeddings: %v", labelErr)
		}
		wmResults, wmFirst, wmErr := st.queryEmbeddingTable("node_whymatters_embeddings", domain, fieldFetch, blob, nodeKinds)
		if wmErr != nil {
			log.Printf("[memoryweb] per-field search: node_whymatters_embeddings: %v", wmErr)
		}
		// Also include legacy embeddings for nodes not yet in the field tables.
		legacyResults, legacyFirst, legacyErr := st.queryEmbeddingTable("node_embeddings", domain, fieldFetch, blob, nodeKinds)
		if legacyErr != nil {
			log.Printf("[memoryweb] per-field search: node_embeddings: %v", legacyErr)
		}

		nodeMap := make(map[string]NodeResult)
		for _, r := range append(append(labelResults, wmResults...), legacyResults...) {
			if existing, ok := nodeMap[r.ID]; !ok || *r.SemanticDistance < *existing.SemanticDistance {
				nodeMap[r.ID] = r
			}
		}
		results = make([]NodeResult, 0, len(nodeMap))
		for _, r := range nodeMap {
			results = append(results, r)
		}
		sort.Slice(results, func(i, j int) bool {
			return *results[i].SemanticDistance < *results[j].SemanticDistance
		})

		// bestDist: from merged results when non-empty, else min of all first-seen
		// distances. The fallback covers MEMORYWEB_SEMANTIC_THRESHOLD filtering all rows.
		if len(results) > 0 {
			d := *results[0].SemanticDistance
			bestDist = &d
		} else {
			for _, fd := range []*float64{labelFirst, wmFirst, legacyFirst} {
				if fd != nil && (bestDist == nil || *fd < *bestDist) {
					dd := *fd
					bestDist = &dd
				}
			}
		}
	} else {
		// Legacy path: single concatenated embedding per node.
		var legacyFirst *float64
		results, legacyFirst, err = st.queryEmbeddingTable("node_embeddings", domain, limit+1, blob, nodeKinds)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			d := *results[0].SemanticDistance
			bestDist = &d
		} else {
			bestDist = legacyFirst
		}
	}

	// Post-filter to neighbourhood if memoryID was supplied.
	if len(allowedIDs) > 0 {
		allowed := make(map[string]struct{}, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = struct{}{}
		}
		results = filter(results, func(nr NodeResult) bool {
			_, ok := allowed[nr.ID]
			return ok
		})
	}

	if len(results) == 0 {
		if bestDist != nil {
			log.Printf("[memoryweb] semantic search: no results (best dist %.3f); falling back to text search", *bestDist)
		}
		likeResult, err := st.searchNodesLike(query, domain, limit, allowedIDs, nodeKinds, true)
		if err != nil {
			return nil, err
		}
		likeResult.SemanticAttempted = true
		// bestDist reflects the closest embedding row scanned before any
		// neighbourhood filter was applied. When allowedIDs removes all
		// semantic candidates, bestDist may be non-nil even though every
		// returned node came from LIKE — it tells the caller a close semantic
		// match exists outside the requested neighbourhood.
		likeResult.SemanticBestDist = bestDist
		return likeResult, nil
	}

	truncated := len(results) > limit
	if truncated {
		results = results[:limit]
	}

	nodes := extractNodes(results)
	edges, err := collectEdges(st, nodes)
	if err != nil {
		return nil, fmt.Errorf("collectEdges: %w", err)
	}
	return &SearchResult{Nodes: results, Edges: edges, Truncated: truncated, SemanticAttempted: true, SemanticBestDist: bestDist}, nil
}

// searchNodesLike performs a full-phrase LIKE search. When wordFallback is true
// and the full-phrase LIKE returns no results, it retries with individual words
// OR'd together. Pass wordFallback=false (the exact path) to skip the fallback.
// When allowedIDs is non-empty, results are restricted to nodes in that set.
func (st *Store) searchNodesLike(query, domain string, limit int, allowedIDs, nodeKinds []string, wordFallback bool) (*SearchResult, error) {
	pattern := "%" + escapeLike(query) + "%"
	fetch := limit + 1

	likeClause := "(label LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\' OR why_matters LIKE ? ESCAPE '\\' OR tags LIKE ? ESCAPE '\\')"
	conds := []string{"archived_at IS NULL", likeClause}
	args := []interface{}{pattern, pattern, pattern, pattern}
	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	if len(allowedIDs) > 0 {
		placeholders, idArgs := inClause(allowedIDs)
		conds = append(conds, "id IN ("+placeholders+")")
		args = append(args, idArgs...)
	}
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, fetch)

	qStr := `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
	 WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY updated_at DESC LIMIT ?`
	rows, err := st.db.Query(qStr, args...)
	if err != nil {
		return nil, err
	}

	nodes, err := scanNodeRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	truncated := len(nodes) > limit
	if truncated {
		nodes = nodes[:limit]
	}

	// If the full-phrase LIKE returned nothing and the query contains multiple
	// words, fall back to an OR of individual-word LIKE terms so that nodes
	// whose fields collectively cover the query words are still surfaced.
	// Neighbourhood scoping is not applied to the word fallback — it already
	// returned nothing within the neighbourhood.
	// The fallback is disabled for the exact path (wordFallback=false) because
	// OR semantics would return false positives for identifier lookups.
	if wordFallback && len(nodes) == 0 && !truncated && len(allowedIDs) == 0 {
		words := strings.Fields(query)
		if len(words) > 1 {
			log.Printf("[memoryweb] search: no results for %q (domain=%q), falling back to individual-word search", query, domain)
			var wordTruncated bool
			nodes, wordTruncated, err = st.searchByWords(words, domain, limit, nodeKinds)
			if err != nil {
				return nil, err
			}
			truncated = wordTruncated
		}
	}

	results := wrapNodes(nodes)
	edges, err := collectEdges(st, nodes)
	if err != nil {
		return nil, fmt.Errorf("collectEdges: %w", err)
	}
	return &SearchResult{Nodes: results, Edges: edges, Truncated: truncated}, nil
}

// extractNodes extracts the embedded Node from each NodeResult.
func extractNodes(nrs []NodeResult) []Node {
	return mapSlice(nrs, func(nr NodeResult) Node { return nr.Node })
}

// wrapNodes wraps []Node into []NodeResult with nil SemanticDistance (LIKE results).
func wrapNodes(nodes []Node) []NodeResult {
	return mapSlice(nodes, func(n Node) NodeResult { return NodeResult{Node: n} })
}

// searchByWords executes a fallback query that matches nodes containing ANY of
// the provided words in ANY of the searchable fields (label, description,
// why_matters, tags). Results are ordered by updated_at DESC.
// Returns the matching nodes and a truncated flag (true when the result set
// was capped at limit).
func (st *Store) searchByWords(words []string, domain string, limit int, nodeKinds []string) ([]Node, bool, error) {
	// Build: (label LIKE ? OR desc LIKE ? OR why LIKE ? OR tags LIKE ?)
	//        OR (label LIKE ? OR ...)   ... one group per word.
	const fields = 4 // label, description, why_matters, tags
	wordClause := "(label LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\' OR why_matters LIKE ? ESCAPE '\\' OR tags LIKE ? ESCAPE '\\')"
	clauses := make([]string, len(words))
	for i := range words {
		clauses[i] = wordClause
	}
	combined := strings.Join(clauses, " OR ")

	// Fetch one extra row to detect truncation.
	fetch := limit + 1

	conds := []string{"archived_at IS NULL"}
	args := []interface{}{}
	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	conds = append(conds, "("+combined+")")
	for _, word := range words {
		wordPattern := "%" + escapeLike(word) + "%"
		for j := 0; j < fields; j++ {
			args = append(args, wordPattern)
		}
	}
	conds, args = nodeKindFilter("node_kind", nodeKinds, conds, args)
	args = append(args, fetch)

	query := `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
	     WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY updated_at DESC LIMIT ?`

	rows, err := st.db.Query(query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	nodes, err := scanNodeRows(rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(nodes) > limit
	if truncated {
		nodes = nodes[:limit]
	}
	return nodes, truncated, nil
}
