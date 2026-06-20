package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ── GetSignificance ───────────────────────────────────────────────────────────

// ScoredNode is a Node decorated with a structural importance score.
type ScoredNode struct {
	Node
	ImportanceScore float64 `json:"importance_score"`
}

// SignificanceResult holds the four sections returned by GetSignificance.
type SignificanceResult struct {
	Declared         []Node       `json:"declared"`
	Structural       []ScoredNode `json:"structural"`
	Uncurated        []ScoredNode `json:"uncurated"`
	PotentiallyStale []Node       `json:"potentially_stale"`
	CallID           string       `json:"call_id"`
}

// GetSignificance returns a dual-signal importance analysis for a domain.
//
//   - declared:          live nodes with occurred_at set, ordered by occurred_at ASC.
//   - structural:        live nodes ranked by weighted inbound degree (decay by linker age),
//     capped at limit, ordered by importance_score DESC.
//   - uncurated:         structural top-N nodes that have no occurred_at.
//   - potentially_stale: declared nodes whose ID does not appear in structural top-N.
//
// When tags is non-empty, only nodes matching at least one tag (whole-word match)
// are included in each section. Callers that pass nil or []string{} get full domain behaviour.
//
// Every call writes rows to significance_log (one per returned node in
// structural, uncurated, potentially_stale) so the decay function can be
// validated over time.
func (s *Store) GetSignificance(domain string, limit int, recencyWindowDays int, tags []string) (SignificanceResult, error) {
	callID := shortID()
	var res SignificanceResult
	res.CallID = callID

	// ── declared ─────────────────────────────────────────────────────────────
	declaredConds := []string{"domain = ?", "occurred_at IS NOT NULL", "archived_at IS NULL"}
	declaredArgs := []interface{}{domain}
	declaredConds, declaredArgs = tagFilter("tags", tags, declaredConds, declaredArgs)
	declaredQ := `SELECT id, label, description, why_matters, tags, domain,
		       created_at, updated_at, occurred_at, archived_at, node_kind
		FROM nodes WHERE ` + strings.Join(declaredConds, " AND ") + ` ORDER BY occurred_at ASC`
	declaredRows, err := s.db.Query(declaredQ, declaredArgs...)
	if err != nil {
		return res, fmt.Errorf("GetSignificance declared: %w", err)
	}
	defer declaredRows.Close()
	for declaredRows.Next() {
		n, err := scanNode(declaredRows)
		if err != nil {
			return res, fmt.Errorf("GetSignificance scan declared: %w", err)
		}
		res.Declared = append(res.Declared, n)
	}
	if err := declaredRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance declared rows: %w", err)
	}
	if res.Declared == nil {
		res.Declared = []Node{}
	}

	// ── structural ────────────────────────────────────────────────────────────
	structConds := []string{
		"n.domain = ?",
		"n.archived_at IS NULL",
		"n2.archived_at IS NULL",
		"(julianday('now') - julianday(n2.updated_at)) <= ?",
	}
	structArgs := []interface{}{domain, recencyWindowDays}
	structConds, structArgs = tagFilter("n.tags", tags, structConds, structArgs)
	structArgs = append(structArgs, limit)
	structQ := `SELECT n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
		       n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.node_kind,
		       SUM(1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS importance_score
		FROM edges e
		JOIN nodes n  ON e.to_node   = n.id
		JOIN nodes n2 ON e.from_node = n2.id
		WHERE ` + strings.Join(structConds, " AND ") + `
		GROUP BY n.id
		ORDER BY importance_score DESC
		LIMIT ?`
	structRows, err := s.db.Query(structQ, structArgs...)
	if err != nil {
		return res, fmt.Errorf("GetSignificance structural: %w", err)
	}
	defer structRows.Close()
	structIDs := map[string]bool{}
	for structRows.Next() {
		var sn ScoredNode
		var tags, desc, why sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var nodeKind string
		if err := structRows.Scan(
			&sn.ID, &sn.Label, &desc, &why, &tags, &sn.Domain,
			&sn.CreatedAt, &sn.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
			&sn.ImportanceScore,
		); err != nil {
			return res, fmt.Errorf("GetSignificance scan structural: %w", err)
		}
		sn.Description = desc.String
		sn.WhyMatters = why.String
		sn.Tags = tags.String
		sn.OccurredAt = nullTimeToPtr(occurredAt)
		sn.ArchivedAt = nullTimeToPtr(archivedAt)
		sn.NodeKind = nodeKind
		res.Structural = append(res.Structural, sn)
		structIDs[sn.ID] = true
	}
	if err := structRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance structural rows: %w", err)
	}
	if res.Structural == nil {
		res.Structural = []ScoredNode{}
	}

	// ── uncurated: structural top-N with no occurred_at ───────────────────────
	for _, sn := range res.Structural {
		if sn.OccurredAt == nil {
			res.Uncurated = append(res.Uncurated, sn)
		}
	}
	if res.Uncurated == nil {
		res.Uncurated = []ScoredNode{}
	}

	// ── potentially_stale: declared but not in structural ─────────────────────
	for _, n := range res.Declared {
		if !structIDs[n.ID] {
			res.PotentiallyStale = append(res.PotentiallyStale, n)
		}
	}
	if res.PotentiallyStale == nil {
		res.PotentiallyStale = []Node{}
	}

	// ── log ───────────────────────────────────────────────────────────────────
	calledAt := time.Now().UTC()
	logged := map[string]bool{}
	for _, sn := range res.Structural {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, sn.ID, sn.Label, "structural", &sn.ImportanceScore); err != nil {
				return res, fmt.Errorf("GetSignificance log structural: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, sn := range res.Uncurated {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, sn.ID, sn.Label, "uncurated", nil); err != nil {
				return res, fmt.Errorf("GetSignificance log uncurated: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, n := range res.PotentiallyStale {
		if !logged[n.ID] {
			if err := s.logSignificance(callID, calledAt, domain, limit, n.ID, n.Label, "potentially_stale", nil); err != nil {
				return res, fmt.Errorf("GetSignificance log potentially_stale: %w", err)
			}
			logged[n.ID] = true
		}
	}

	return res, nil
}

// ── significance: memory_id mode ─────────────────────────────────────────────

// getSignificanceByNodeIDs runs dual-signal importance analysis scoped to a
// specific set of node IDs (e.g. a neighbourhood). domain is used only for
// logging; it does not further filter the node set.
func (s *Store) getSignificanceByNodeIDs(nodeIDs []string, domain string, recencyWindowDays int) (SignificanceResult, error) {
	callID := shortID()
	var res SignificanceResult
	res.CallID = callID

	if len(nodeIDs) == 0 {
		res.Declared = []Node{}
		res.Structural = []ScoredNode{}
		res.Uncurated = []ScoredNode{}
		res.PotentiallyStale = []Node{}
		return res, nil
	}

	ph, nodeArgs := inClause(nodeIDs)

	// ── declared ─────────────────────────────────────────────────────────────
	declaredRows, err := s.db.Query(
		`SELECT id, label, description, why_matters, tags, domain,
		        created_at, updated_at, occurred_at, archived_at, node_kind
		 FROM nodes
		 WHERE id IN (`+ph+`)
		   AND occurred_at IS NOT NULL
		   AND archived_at IS NULL
		 ORDER BY occurred_at ASC`, nodeArgs...)
	if err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs declared: %w", err)
	}
	defer declaredRows.Close()
	for declaredRows.Next() {
		n, err := scanNode(declaredRows)
		if err != nil {
			return res, fmt.Errorf("getSignificanceByNodeIDs scan declared: %w", err)
		}
		res.Declared = append(res.Declared, n)
	}
	if err := declaredRows.Err(); err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs declared rows: %w", err)
	}
	if res.Declared == nil {
		res.Declared = []Node{}
	}

	// ── structural ────────────────────────────────────────────────────────────
	structArgs := append(nodeArgs, recencyWindowDays)

	structRows, err := s.db.Query(
		`SELECT n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
		        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.node_kind,
		        SUM(1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS importance_score
		 FROM edges e
		 JOIN nodes n  ON e.to_node   = n.id
		 JOIN nodes n2 ON e.from_node = n2.id
		 WHERE n.id IN (`+ph+`)
		   AND n.archived_at IS NULL
		   AND n2.archived_at IS NULL
		   AND (julianday('now') - julianday(n2.updated_at)) <= ?
		 GROUP BY n.id
		 ORDER BY importance_score DESC`, structArgs...)
	if err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs structural: %w", err)
	}
	defer structRows.Close()
	structIDs := map[string]bool{}
	for structRows.Next() {
		var sn ScoredNode
		var tags, desc, why sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var nodeKind string
		if err := structRows.Scan(
			&sn.ID, &sn.Label, &desc, &why, &tags, &sn.Domain,
			&sn.CreatedAt, &sn.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
			&sn.ImportanceScore,
		); err != nil {
			return res, fmt.Errorf("getSignificanceByNodeIDs scan structural: %w", err)
		}
		sn.Description = desc.String
		sn.WhyMatters = why.String
		sn.Tags = tags.String
		sn.OccurredAt = nullTimeToPtr(occurredAt)
		sn.ArchivedAt = nullTimeToPtr(archivedAt)
		sn.NodeKind = nodeKind
		res.Structural = append(res.Structural, sn)
		structIDs[sn.ID] = true
	}
	if err := structRows.Err(); err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs structural rows: %w", err)
	}
	if res.Structural == nil {
		res.Structural = []ScoredNode{}
	}

	// ── uncurated ─────────────────────────────────────────────────────────────
	for _, sn := range res.Structural {
		if sn.OccurredAt == nil {
			res.Uncurated = append(res.Uncurated, sn)
		}
	}
	if res.Uncurated == nil {
		res.Uncurated = []ScoredNode{}
	}

	// ── potentially_stale ─────────────────────────────────────────────────────
	for _, n := range res.Declared {
		if !structIDs[n.ID] {
			res.PotentiallyStale = append(res.PotentiallyStale, n)
		}
	}
	if res.PotentiallyStale == nil {
		res.PotentiallyStale = []Node{}
	}

	// ── log ───────────────────────────────────────────────────────────────────
	calledAt := time.Now().UTC()
	logged := map[string]bool{}
	for _, sn := range res.Structural {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, len(nodeIDs), sn.ID, sn.Label, "structural", &sn.ImportanceScore); err != nil {
				return res, fmt.Errorf("getSignificanceByNodeIDs log structural: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, sn := range res.Uncurated {
		if !logged[sn.ID] {
			if err := s.logSignificance(callID, calledAt, domain, len(nodeIDs), sn.ID, sn.Label, "uncurated", nil); err != nil {
				return res, fmt.Errorf("getSignificanceByNodeIDs log uncurated: %w", err)
			}
			logged[sn.ID] = true
		}
	}
	for _, n := range res.PotentiallyStale {
		if !logged[n.ID] {
			if err := s.logSignificance(callID, calledAt, domain, len(nodeIDs), n.ID, n.Label, "potentially_stale", nil); err != nil {
				return res, fmt.Errorf("getSignificanceByNodeIDs log potentially_stale: %w", err)
			}
			logged[n.ID] = true
		}
	}

	return res, nil
}

// GetSignificanceForMemoryID returns dual-signal importance analysis scoped to
// the depth-hop neighbourhood of the given memory ID, clipped to the anchor's
// domain. Depth 2 is recommended; depth 1 produces near-uniform low scores.
func (s *Store) GetSignificanceForMemoryID(nodeID string, depth int, recencyWindowDays int) (SignificanceResult, error) {
	ids, anchorDomain, err := s.neighbourhoodIDs(nodeID, depth)
	if err != nil {
		return SignificanceResult{}, err
	}
	return s.getSignificanceByNodeIDs(ids, anchorDomain, recencyWindowDays)
}

// logSignificance inserts one row into significance_log.
func (s *Store) logSignificance(callID string, calledAt time.Time, domain string, limitN int, nodeID, nodeLabel, rankType string, score *float64) error {
	id := shortID()
	_, err := s.db.Exec(
		`INSERT INTO significance_log (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, callID, calledAt, domain, limitN, nodeID, nodeLabel, rankType, score,
	)
	return err
}
