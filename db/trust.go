package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ── GetTrust ──────────────────────────────────────────────────────────────────

// intrinsicWeight maps a node_kind to its base epistemic trust level. Kinds not
// present here (reference, transient) resolve to the zero value — they are not
// epistemic claims, so they have nothing to lend or borrow trust from.
var intrinsicWeight = map[string]float64{
	"finding":    1.0,
	"decision":   1.0,
	"standing":   1.0,
	"goal":       0.6,
	"option":     0.6,
	"issue":      0.4,
	"assumption": 0.2,
}

func intrinsicWeightOf(kind string) float64 {
	return intrinsicWeight[kind]
}

// edgeWeight returns the sign a relationship contributes to trust: contradicts
// is the one negating relationship, every other relationship supports.
func edgeWeight(relationship string) float64 {
	if relationship == "contradicts" {
		return -1.0
	}
	return 1.0
}

// TrustNode is a Node decorated with a computed epistemic trust score.
type TrustNode struct {
	Node
	TrustScore float64 `json:"trust_score"`
	TrustBasis string  `json:"trust_basis"`
}

// TrustResult holds the ranked trust output.
type TrustResult struct {
	Nodes  []TrustNode `json:"nodes"`
	CallID string      `json:"call_id"`
}

type trustAccum struct {
	node            Node
	raw             float64
	inbound         float64
	highTierSupport bool
	neighbours      map[string]int // node_kind -> count, for trust_basis
}

// TrustAssessment holds a per-node trust basis and low-trust predicate result.
// Used by orient inline annotation and filing-time nudges — no normalised score.
type TrustAssessment struct {
	TrustBasis string `json:"trust_basis"`
	IsLowTrust bool   `json:"is_low_trust"`
}

const defaultTrustRecencyWindow = 90

func isHighTierKind(kind string) bool {
	return intrinsicWeightOf(kind) >= 1.0
}

func isLowTrust(a *trustAccum) bool {
	// No inbound edges yet — neighbourhood trust is undefined, not low.
	if len(a.neighbours) == 0 {
		return false
	}
	return a.inbound <= 0 || !a.highTierSupport
}

const trustSelectColumns = `n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
	       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.node_kind,
	       e.relationship, n2.node_kind, n2.id,
	       (1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS decay`

const trustJoins = `FROM nodes n
	LEFT JOIN edges e ON e.to_node = n.id
	LEFT JOIN nodes n2 ON e.from_node = n2.id
	       AND n2.archived_at IS NULL
	       AND (julianday('now') - julianday(n2.updated_at)) <= ?`

// GetTrust returns nodes in domain ranked by computed epistemic trust — derived
// from each node's own node_kind plus the kinds of nodes that connect to it
// (parallel to how GetSignificance derives structural importance from inbound
// edge count). reference and transient nodes are excluded from the ranked
// output (they are not epistemic claims) but still count as neighbours, at
// zero weight. contradicts edges subtract; every other relationship adds, all
// discounted by the same recency decay GetSignificance uses. Scores are
// normalised to [0, 1] within the result set, ordered by trust_score DESC,
// capped at limit.
func (s *Store) GetTrust(domain string, limit, recencyWindowDays int, tags, nodeKinds []string) (TrustResult, error) {
	conds := []string{
		"n.domain = ?",
		"n.archived_at IS NULL",
		"n.node_kind NOT IN ('reference', 'transient')",
	}
	whereArgs := []interface{}{domain}
	conds, whereArgs = tagFilter("n.tags", tags, conds, whereArgs)
	conds, whereArgs = nodeKindFilter("n.node_kind", nodeKinds, conds, whereArgs)

	args := append([]interface{}{recencyWindowDays}, whereArgs...)
	q := `SELECT ` + trustSelectColumns + `
	      ` + trustJoins + `
	      WHERE ` + strings.Join(conds, " AND ")

	accum, order, err := s.scanTrustRows(q, args, nil)
	if err != nil {
		return TrustResult{}, fmt.Errorf("GetTrust: %w", err)
	}
	return s.finishTrust(accum, order, domain, limit)
}

// getTrustByNodeIDs runs trust analysis scoped to a specific set of node IDs
// (e.g. a neighbourhood). domain is used only for logging; it does not further
// filter the node set. No limit truncation, matching getSignificanceByNodeIDs.
func (s *Store) getTrustByNodeIDs(nodeIDs []string, domain string, recencyWindowDays int, nodeKinds []string) (TrustResult, error) {
	if len(nodeIDs) == 0 {
		return TrustResult{Nodes: []TrustNode{}, CallID: shortID()}, nil
	}
	ph, idArgs := inClause(nodeIDs)
	conds := []string{"n.id IN (" + ph + ")", "n.archived_at IS NULL", "n.node_kind NOT IN ('reference', 'transient')"}
	conds, kindArgs := nodeKindFilter("n.node_kind", nodeKinds, conds, nil)
	args := append([]interface{}{recencyWindowDays}, idArgs...)
	args = append(args, kindArgs...)

	q := `SELECT ` + trustSelectColumns + `
	      ` + trustJoins + `
	      WHERE ` + strings.Join(conds, " AND ")

	accum, order, err := s.scanTrustRows(q, args, nil)
	if err != nil {
		return TrustResult{}, fmt.Errorf("getTrustByNodeIDs: %w", err)
	}
	return s.finishTrust(accum, order, domain, 0)
}

// GetTrustForMemoryID returns trust analysis scoped to the depth-hop
// neighbourhood of the given memory ID, clipped to the anchor's domain.
func (s *Store) GetTrustForMemoryID(nodeID string, depth int, recencyWindowDays int, nodeKinds []string) (TrustResult, error) {
	ids, anchorDomain, err := s.neighbourhoodIDs(nodeID, depth)
	if err != nil {
		return TrustResult{}, err
	}
	return s.getTrustByNodeIDs(ids, anchorDomain, recencyWindowDays, nodeKinds)
}

// scanTrustRows runs query and accumulates one trustAccum per target node,
// summing the weighted contribution of every qualifying inbound edge.
// excludeInboundFrom skips inbound edges whose from-node is in the set (used when
// assessing dependency trust so the filing/revising node does not inflate its deps).
func (s *Store) scanTrustRows(query string, args []interface{}, excludeInboundFrom map[string]struct{}) (map[string]*trustAccum, []string, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	accum := map[string]*trustAccum{}
	var order []string
	for rows.Next() {
		var n Node
		var desc, why, tagsCol sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var nodeKind string
		var relationship, neighbourKind, neighbourID sql.NullString
		var decay sql.NullFloat64
		if err := rows.Scan(
			&n.ID, &n.Label, &desc, &why, &tagsCol, &n.Domain,
			&n.CreatedAt, &n.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
			&relationship, &neighbourKind, &neighbourID, &decay,
		); err != nil {
			return nil, nil, fmt.Errorf("scan: %w", err)
		}
		n.Description = desc.String
		n.WhyMatters = why.String
		n.Tags = tagsCol.String
		n.OccurredAt = nullTimeToPtr(occurredAt)
		n.ArchivedAt = nullTimeToPtr(archivedAt)
		n.NodeKind = nodeKind

		a, ok := accum[n.ID]
		if !ok {
			a = &trustAccum{
				node:       n,
				raw:        intrinsicWeightOf(nodeKind),
				neighbours: map[string]int{},
			}
			accum[n.ID] = a
			order = append(order, n.ID)
		}
		if relationship.Valid && neighbourKind.Valid && decay.Valid {
			if excludeInboundFrom != nil && neighbourID.Valid {
				if _, skip := excludeInboundFrom[neighbourID.String]; skip {
					continue
				}
			}
			contrib := edgeWeight(relationship.String) * intrinsicWeightOf(neighbourKind.String) * decay.Float64
			a.raw += contrib
			a.inbound += contrib
			if contrib > 0 && isHighTierKind(neighbourKind.String) {
				a.highTierSupport = true
			}
			a.neighbours[neighbourKind.String]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("rows: %w", err)
	}
	return accum, order, nil
}

// finishTrust normalises raw scores to [0,1], sorts descending, truncates to
// limit (when > 0), and logs each returned node to significance_log with
// rank_type='trust'.
func (s *Store) finishTrust(accum map[string]*trustAccum, order []string, domain string, limit int) (TrustResult, error) {
	if len(order) == 0 {
		return TrustResult{Nodes: []TrustNode{}, CallID: shortID()}, nil
	}

	minRaw, maxRaw := accum[order[0]].raw, accum[order[0]].raw
	for _, id := range order {
		r := accum[id].raw
		if r < minRaw {
			minRaw = r
		}
		if r > maxRaw {
			maxRaw = r
		}
	}

	nodes := make([]TrustNode, 0, len(order))
	for _, id := range order {
		a := accum[id]
		score := 1.0
		if maxRaw != minRaw {
			score = (a.raw - minRaw) / (maxRaw - minRaw)
		}
		nodes = append(nodes, TrustNode{
			Node:       a.node,
			TrustScore: score,
			TrustBasis: formatTrustBasis(a.node.NodeKind, a.neighbours),
		})
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].TrustScore > nodes[j].TrustScore })
	if limit > 0 && len(nodes) > limit {
		nodes = nodes[:limit]
	}

	callID := shortID()
	calledAt := time.Now().UTC()
	for _, n := range nodes {
		score := n.TrustScore
		if err := s.logSignificance(callID, calledAt, domain, limit, n.ID, n.Label, "trust", &score); err != nil {
			return TrustResult{}, fmt.Errorf("log trust: %w", err)
		}
	}

	return TrustResult{Nodes: nodes, CallID: callID}, nil
}

// AssessTrustForNodeIDs computes trust basis and the low-trust predicate for each
// node ID without normalising scores or writing significance_log rows.
// excludeInboundFrom omits inbound edges from those node IDs (typically the node
// being filed or revised, so its depends_on link does not mask low-trust deps).
func (s *Store) AssessTrustForNodeIDs(nodeIDs []string, recencyWindowDays int, excludeInboundFrom ...string) (map[string]TrustAssessment, error) {
	if recencyWindowDays <= 0 {
		recencyWindowDays = defaultTrustRecencyWindow
	}
	result := map[string]TrustAssessment{}
	if len(nodeIDs) == 0 {
		return result, nil
	}
	var exclude map[string]struct{}
	if len(excludeInboundFrom) > 0 {
		exclude = make(map[string]struct{}, len(excludeInboundFrom))
		for _, id := range excludeInboundFrom {
			if id != "" {
				exclude[id] = struct{}{}
			}
		}
	}
	ph, idArgs := inClause(nodeIDs)
	conds := []string{"n.id IN (" + ph + ")", "n.archived_at IS NULL", "n.node_kind NOT IN ('reference', 'transient')"}
	args := append([]interface{}{recencyWindowDays}, idArgs...)
	q := `SELECT ` + trustSelectColumns + `
	      ` + trustJoins + `
	      WHERE ` + strings.Join(conds, " AND ")
	accum, _, err := s.scanTrustRows(q, args, exclude)
	if err != nil {
		return nil, fmt.Errorf("AssessTrustForNodeIDs: %w", err)
	}
	for _, id := range nodeIDs {
		a, ok := accum[id]
		if !ok {
			continue
		}
		result[id] = TrustAssessment{
			TrustBasis: formatTrustBasis(a.node.NodeKind, a.neighbours),
			IsLowTrust: isLowTrust(a),
		}
	}
	return result, nil
}

// formatTrustBasis builds a human-readable audit trail: the node's own kind,
// then each distinct neighbour kind present, sorted by count desc then kind
// alphabetically. The "self:" component guarantees a non-empty result even
// when a node has no neighbours.
func formatTrustBasis(selfKind string, neighbours map[string]int) string {
	parts := []string{"self:" + selfKind}
	kinds := make([]string, 0, len(neighbours))
	for k := range neighbours {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if neighbours[kinds[i]] != neighbours[kinds[j]] {
			return neighbours[kinds[i]] > neighbours[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s×%d", k, neighbours[k]))
	}
	return strings.Join(parts, ", ")
}
