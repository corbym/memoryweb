package db

import (
	"database/sql"
	"log"
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
	Nodes     []NodeResult `json:"nodes"`
	Edges     []Edge       `json:"edges"`
	Truncated bool         `json:"truncated,omitempty"`
}

func (s *Store) SearchNodes(query, domain string, limit int, memoryID string) (*SearchResult, error) {
	domain = s.ResolveAlias(domain)

	var allowedIDs []string
	if memoryID != "" {
		ids, _, err := s.neighbourhoodIDs(memoryID, 2)
		if err != nil {
			return nil, err
		}
		allowedIDs = ids
	}

	// Try semantic search when sqlite-vec is loaded.
	if s.vecAvailable {
		embedding, err := embed(query)
		if err == nil && len(embedding) > 0 {
			result, err := s.searchNodesSemantic(query, domain, limit, embedding, allowedIDs)
			if err == nil {
				return result, nil
			}
			log.Printf("[memoryweb] semantic search failed: %v; falling back to text search", err)
		}
	}

	return s.searchNodesLike(query, domain, limit, allowedIDs)
}

// SearchNodesExact performs a pure substring (LIKE) search, bypassing semantic
// ranking entirely. Use this when the query contains a unique identifier, ticket
// number, or short code that is known to appear verbatim in the stored content.
// Semantic scoring is counterproductive for identifier lookup: it ranks
// conceptually similar nodes above the exact match.
func (s *Store) SearchNodesExact(query, domain string, limit int, memoryID string) (*SearchResult, error) {
	domain = s.ResolveAlias(domain)

	var allowedIDs []string
	if memoryID != "" {
		ids, _, err := s.neighbourhoodIDs(memoryID, 2)
		if err != nil {
			return nil, err
		}
		allowedIDs = ids
	}

	return s.searchNodesLike(query, domain, limit, allowedIDs)
}

// semanticDistanceThreshold is the maximum cosine distance for a node to be
// considered a semantic match. vec_distance_cosine returns values in [0, 2];
// 0 = identical, 2 = opposite. Results beyond this threshold are discarded
// and the LIKE fallback runs instead.
const semanticDistanceThreshold = 0.3

// searchNodesSemantic ranks nodes by cosine distance between the query
// embedding and stored node embeddings, then falls back to LIKE if no
// semantic results are found within the relevance threshold.
func (s *Store) searchNodesSemantic(query, domain string, limit int, embedding []float32, allowedIDs []string) (*SearchResult, error) {
	blob, err := vec.SerializeFloat32(embedding)
	if err != nil {
		return nil, err
	}

	// Fetch one extra row so we can detect truncation without a separate COUNT
	// query. The threshold check still cuts off results beyond semanticDistanceThreshold.
	fetch := limit + 1

	var rows *sql.Rows
	if domain != "" {
		rows, err = s.db.Query(`
			SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind,
			       vec_distance_cosine(e.embedding, ?) AS dist
			FROM node_embeddings e
			JOIN nodes n ON n.id = e.node_id
			WHERE n.archived_at IS NULL AND n.domain = ?
			ORDER BY dist ASC
			LIMIT ?`,
			blob, domain, fetch)
	} else {
		rows, err = s.db.Query(`
			SELECT n.id, n.label, n.description, n.why_matters, n.domain,
			       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.tags, n.node_kind,
			       vec_distance_cosine(e.embedding, ?) AS dist
			FROM node_embeddings e
			JOIN nodes n ON n.id = e.node_id
			WHERE n.archived_at IS NULL
			ORDER BY dist ASC
			LIMIT ?`,
			blob, fetch)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
			return nil, err
		}
		// Results are ordered by distance ASC; stop as soon as we exceed the threshold.
		if dist > semanticDistanceThreshold {
			break
		}
		n.OccurredAt = nullTimeToPtr(occurredAt)
		n.ArchivedAt = nullTimeToPtr(archivedAt)
		d := dist // copy for pointer stability
		results = append(results, NodeResult{Node: n, SemanticDistance: &d})
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
		// No embeddings within threshold (or all filtered out); fall back to literal search.
		return s.searchNodesLike(query, domain, limit, allowedIDs)
	}

	truncated := len(results) > limit
	if truncated {
		results = results[:limit]
	}

	nodes := extractNodes(results)
	return &SearchResult{Nodes: results, Edges: collectEdges(s.db, nodes), Truncated: truncated}, nil
}

// searchNodesLike performs a full-phrase LIKE search with a multi-word fallback.
// When allowedIDs is non-empty, results are restricted to nodes in that set.
func (s *Store) searchNodesLike(query, domain string, limit int, allowedIDs []string) (*SearchResult, error) {
	q := "%" + query + "%"
	var rows *sql.Rows
	var err error

	// Fetch one extra row to detect truncation without a separate COUNT query.
	fetch := limit + 1

	if len(allowedIDs) > 0 {
		ph, idArgs := inClause(allowedIDs)
		args := []interface{}{}
		if domain != "" {
			args = append(args, domain)
		}
		args = append(args, q, q, q, q)
		args = append(args, idArgs...)
		args = append(args, fetch)
		var qStr string
		if domain != "" {
			qStr = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?) AND id IN (` + ph + `) ORDER BY updated_at DESC LIMIT ?`
		} else {
			qStr = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?) AND id IN (` + ph + `) ORDER BY updated_at DESC LIMIT ?`
		}
		rows, err = s.db.Query(qStr, args...)
	} else if domain != "" {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE domain = ? AND archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			domain, q, q, q, q, fetch,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
			 WHERE archived_at IS NULL AND (label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)
			 ORDER BY updated_at DESC LIMIT ?`,
			q, q, q, q, fetch,
		)
	}
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
	if len(nodes) == 0 && !truncated && len(allowedIDs) == 0 {
		words := strings.Fields(query)
		if len(words) > 1 {
			log.Printf("[memoryweb] search: no results for %q (domain=%q), falling back to individual-word search", query, domain)
			var wordTruncated bool
			nodes, wordTruncated, err = s.searchByWords(words, domain, limit)
			if err != nil {
				return nil, err
			}
			truncated = wordTruncated
		}
	}

	results := wrapNodes(nodes)
	return &SearchResult{Nodes: results, Edges: collectEdges(s.db, nodes), Truncated: truncated}, nil
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
func (s *Store) searchByWords(words []string, domain string, limit int) ([]Node, bool, error) {
	// Build: (label LIKE ? OR desc LIKE ? OR why LIKE ? OR tags LIKE ?)
	//        OR (label LIKE ? OR ...)   ... one group per word.
	const fields = 4 // label, description, why_matters, tags
	wordClause := "(label LIKE ? OR description LIKE ? OR why_matters LIKE ? OR tags LIKE ?)"
	clauses := make([]string, len(words))
	for i := range words {
		clauses[i] = wordClause
	}
	combined := strings.Join(clauses, " OR ")

	args := []interface{}{}
	for _, w := range words {
		wq := "%" + w + "%"
		for j := 0; j < fields; j++ {
			args = append(args, wq)
		}
	}

	// Fetch one extra row to detect truncation.
	fetch := limit + 1

	var q string
	if domain != "" {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
		     WHERE domain = ? AND archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		// domain goes first, limit last
		finalArgs := make([]interface{}, 0, 1+len(args)+1)
		finalArgs = append(finalArgs, domain)
		finalArgs = append(finalArgs, args...)
		finalArgs = append(finalArgs, fetch)
		args = finalArgs
	} else {
		q = `SELECT id, label, description, why_matters, domain, created_at, updated_at, occurred_at, archived_at, tags, node_kind FROM nodes
		     WHERE archived_at IS NULL AND (` + combined + `) ORDER BY updated_at DESC LIMIT ?`
		args = append(args, fetch)
	}

	rows, err := s.db.Query(q, args...)
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
