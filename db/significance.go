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
	Declared                         []Node       `json:"declared"`
	Structural                       []ScoredNode `json:"structural"`
	Uncurated                        []ScoredNode `json:"uncurated"`
	PotentiallyStale                 []Node       `json:"potentially_stale"`
	CallID                           string       `json:"call_id"`
	DeclaredResultsTruncated         bool         `json:"declared_results_truncated"`
	StructuralResultsTruncated       bool         `json:"structural_results_truncated"`
	UncuratedResultsTruncated        bool         `json:"uncurated_results_truncated"`
	PotentiallyStaleResultsTruncated bool         `json:"potentially_stale_results_truncated"`
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
func (st *Store) GetSignificance(domain string, limit int, recencyWindowDays int, tags, nodeKinds []string, declaredLimit int) (SignificanceResult, error) {
	callID := shortID()
	var res SignificanceResult
	res.CallID = callID

	// ── declared ─────────────────────────────────────────────────────────────
	declaredConds := []string{"domain = ?", "occurred_at IS NOT NULL", "archived_at IS NULL"}
	declaredArgs := []interface{}{domain}
	declaredConds, declaredArgs = tagFilter("tags", tags, declaredConds, declaredArgs)
	declaredConds, declaredArgs = nodeKindFilter("node_kind", nodeKinds, declaredConds, declaredArgs)
	declaredQ := `SELECT id, label, description, why_matters, tags, domain,
		       created_at, updated_at, occurred_at, archived_at, node_kind
		FROM nodes WHERE ` + strings.Join(declaredConds, " AND ") + ` ORDER BY occurred_at ASC`
	if declaredLimit > 0 {
		declaredQ += ` LIMIT ?`
		declaredArgs = append(declaredArgs, declaredLimit+1)
	}
	declaredRows, err := st.db.Query(declaredQ, declaredArgs...)
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
	if declaredLimit > 0 {
		res.DeclaredResultsTruncated = len(res.Declared) > declaredLimit
		if res.DeclaredResultsTruncated {
			res.Declared = res.Declared[:declaredLimit]
		}
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
	structConds, structArgs = nodeKindFilter("n.node_kind", nodeKinds, structConds, structArgs)
	structArgs = append(structArgs, limit+1)
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
	structRows, err := st.db.Query(structQ, structArgs...)
	if err != nil {
		return res, fmt.Errorf("GetSignificance structural: %w", err)
	}
	defer structRows.Close()
	structIDs := map[string]bool{}
	for structRows.Next() {
		var scoredNode ScoredNode
		var tagsNull, description, whyMatters sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var nodeKind string
		if err := structRows.Scan(
			&scoredNode.ID, &scoredNode.Label, &description, &whyMatters, &tagsNull, &scoredNode.Domain,
			&scoredNode.CreatedAt, &scoredNode.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
			&scoredNode.ImportanceScore,
		); err != nil {
			return res, fmt.Errorf("GetSignificance scan structural: %w", err)
		}
		scoredNode.Description = description.String
		scoredNode.WhyMatters = whyMatters.String
		scoredNode.Tags = tagsNull.String
		scoredNode.OccurredAt = nullTimeToPtr(occurredAt)
		scoredNode.ArchivedAt = nullTimeToPtr(archivedAt)
		scoredNode.NodeKind = nodeKind
		res.Structural = append(res.Structural, scoredNode)
		structIDs[scoredNode.ID] = true
	}
	if err := structRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance structural rows: %w", err)
	}
	if res.Structural == nil {
		res.Structural = []ScoredNode{}
	}
	res.StructuralResultsTruncated = len(res.Structural) > limit
	if res.StructuralResultsTruncated {
		res.Structural = res.Structural[:limit]
		structIDs = map[string]bool{}
		for _, scoredNode := range res.Structural {
			structIDs[scoredNode.ID] = true
		}
	}

	// ── uncurated: structural top-N with no occurred_at ───────────────────────
	for _, scoredNode := range res.Structural {
		if scoredNode.OccurredAt == nil {
			res.Uncurated = append(res.Uncurated, scoredNode)
		}
	}
	if res.Uncurated == nil {
		res.Uncurated = []ScoredNode{}
	}
	res.UncuratedResultsTruncated = res.StructuralResultsTruncated

	// ── potentially_stale: declared but not in structural top-N ─────────────────
	psConds := []string{"domain = ?", "occurred_at IS NOT NULL", "archived_at IS NULL"}
	psArgs := []interface{}{domain}
	psConds, psArgs = tagFilter("tags", tags, psConds, psArgs)
	psConds, psArgs = nodeKindFilter("node_kind", nodeKinds, psConds, psArgs)
	if len(structIDs) > 0 {
		structIDList := make([]string, 0, len(structIDs))
		for id := range structIDs {
			structIDList = append(structIDList, id)
		}
		placeholders, placeholderArgs := inClause(structIDList)
		psConds = append(psConds, "id NOT IN ("+placeholders+")")
		psArgs = append(psArgs, placeholderArgs...)
	}
	psQ := `SELECT id, label, description, why_matters, tags, domain,
	       created_at, updated_at, occurred_at, archived_at, node_kind
	FROM nodes WHERE ` + strings.Join(psConds, " AND ") + ` ORDER BY occurred_at ASC`
	if declaredLimit > 0 {
		psQ += ` LIMIT ?`
		psArgs = append(psArgs, declaredLimit+1)
	}
	psRows, err := st.db.Query(psQ, psArgs...)
	if err != nil {
		return res, fmt.Errorf("GetSignificance potentially_stale: %w", err)
	}
	defer psRows.Close()
	for psRows.Next() {
		n, err := scanNode(psRows)
		if err != nil {
			return res, fmt.Errorf("GetSignificance scan potentially_stale: %w", err)
		}
		res.PotentiallyStale = append(res.PotentiallyStale, n)
	}
	if err := psRows.Err(); err != nil {
		return res, fmt.Errorf("GetSignificance potentially_stale rows: %w", err)
	}
	if res.PotentiallyStale == nil {
		res.PotentiallyStale = []Node{}
	}
	if declaredLimit > 0 {
		res.PotentiallyStaleResultsTruncated = len(res.PotentiallyStale) > declaredLimit
		if res.PotentiallyStaleResultsTruncated {
			res.PotentiallyStale = res.PotentiallyStale[:declaredLimit]
		}
	}

	// ── log ───────────────────────────────────────────────────────────────────
	calledAt := time.Now().UTC()
	logged := map[string]bool{}
	var logEntries []significanceLogEntry
	for _, scoredNode := range res.Structural {
		if !logged[scoredNode.ID] {
			s := scoredNode.ImportanceScore
			logEntries = append(logEntries, significanceLogEntry{scoredNode.ID, scoredNode.Label, "structural", &s})
			logged[scoredNode.ID] = true
		}
	}
	for _, scoredNode := range res.Uncurated {
		if !logged[scoredNode.ID] {
			logEntries = append(logEntries, significanceLogEntry{scoredNode.ID, scoredNode.Label, "uncurated", nil})
			logged[scoredNode.ID] = true
		}
	}
	for _, n := range res.PotentiallyStale {
		if !logged[n.ID] {
			logEntries = append(logEntries, significanceLogEntry{n.ID, n.Label, "potentially_stale", nil})
			logged[n.ID] = true
		}
	}
	if err := st.logSignificanceBatch(callID, calledAt, domain, limit, logEntries); err != nil {
		return res, fmt.Errorf("GetSignificance log batch: %w", err)
	}

	return res, nil
}

// ── significance: memory_id mode ─────────────────────────────────────────────

// getSignificanceByNodeIDs runs dual-signal importance analysis scoped to a
// specific set of node IDs (e.g. a neighbourhood). domain is used only for
// logging; it does not further filter the node set.
func (st *Store) getSignificanceByNodeIDs(nodeIDs []string, domain string, recencyWindowDays int, nodeKinds []string) (SignificanceResult, error) {
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

	placeholders, nodeArgs := inClause(nodeIDs)

	declConds := []string{"id IN (" + placeholders + ")", "occurred_at IS NOT NULL", "archived_at IS NULL"}
	declConds, declArgs := nodeKindFilter("node_kind", nodeKinds, declConds, append([]interface{}{}, nodeArgs...))

	// ── declared ─────────────────────────────────────────────────────────────
	declaredRows, err := st.db.Query(
		`SELECT id, label, description, why_matters, tags, domain,
		        created_at, updated_at, occurred_at, archived_at, node_kind
		 FROM nodes
		 WHERE `+strings.Join(declConds, " AND ")+`
		 ORDER BY occurred_at ASC`, declArgs...)
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
	structConds := []string{"n.id IN (" + placeholders + ")", "n.archived_at IS NULL", "n2.archived_at IS NULL", "(julianday('now') - julianday(n2.updated_at)) <= ?"}
	structConds, structKindArgs := nodeKindFilter("n.node_kind", nodeKinds, structConds, nil)
	structArgs := append(nodeArgs, recencyWindowDays)
	structArgs = append(structArgs, structKindArgs...)

	structRows, err := st.db.Query(
		`SELECT n.id, n.label, n.description, n.why_matters, n.tags, n.domain,
		        n.created_at, n.updated_at, n.occurred_at, n.archived_at, n.node_kind,
		        SUM(1.0 / (1.0 + (julianday('now') - julianday(n2.updated_at)))) AS importance_score
		 FROM edges e
		 JOIN nodes n  ON e.to_node   = n.id
		 JOIN nodes n2 ON e.from_node = n2.id
		 WHERE `+strings.Join(structConds, " AND ")+`
		 GROUP BY n.id
		 ORDER BY importance_score DESC`, structArgs...)
	if err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs structural: %w", err)
	}
	defer structRows.Close()
	structIDs := map[string]bool{}
	for structRows.Next() {
		var scoredNode ScoredNode
		var tagsNull, description, whyMatters sql.NullString
		var occurredAt, archivedAt sql.NullTime
		var nodeKind string
		if err := structRows.Scan(
			&scoredNode.ID, &scoredNode.Label, &description, &whyMatters, &tagsNull, &scoredNode.Domain,
			&scoredNode.CreatedAt, &scoredNode.UpdatedAt, &occurredAt, &archivedAt, &nodeKind,
			&scoredNode.ImportanceScore,
		); err != nil {
			return res, fmt.Errorf("getSignificanceByNodeIDs scan structural: %w", err)
		}
		scoredNode.Description = description.String
		scoredNode.WhyMatters = whyMatters.String
		scoredNode.Tags = tagsNull.String
		scoredNode.OccurredAt = nullTimeToPtr(occurredAt)
		scoredNode.ArchivedAt = nullTimeToPtr(archivedAt)
		scoredNode.NodeKind = nodeKind
		res.Structural = append(res.Structural, scoredNode)
		structIDs[scoredNode.ID] = true
	}
	if err := structRows.Err(); err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs structural rows: %w", err)
	}
	if res.Structural == nil {
		res.Structural = []ScoredNode{}
	}

	// ── uncurated ─────────────────────────────────────────────────────────────
	for _, scoredNode := range res.Structural {
		if scoredNode.OccurredAt == nil {
			res.Uncurated = append(res.Uncurated, scoredNode)
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
	var logEntries []significanceLogEntry
	for _, scoredNode := range res.Structural {
		if !logged[scoredNode.ID] {
			s := scoredNode.ImportanceScore
			logEntries = append(logEntries, significanceLogEntry{scoredNode.ID, scoredNode.Label, "structural", &s})
			logged[scoredNode.ID] = true
		}
	}
	for _, scoredNode := range res.Uncurated {
		if !logged[scoredNode.ID] {
			logEntries = append(logEntries, significanceLogEntry{scoredNode.ID, scoredNode.Label, "uncurated", nil})
			logged[scoredNode.ID] = true
		}
	}
	for _, n := range res.PotentiallyStale {
		if !logged[n.ID] {
			logEntries = append(logEntries, significanceLogEntry{n.ID, n.Label, "potentially_stale", nil})
			logged[n.ID] = true
		}
	}
	if err := st.logSignificanceBatch(callID, calledAt, domain, len(nodeIDs), logEntries); err != nil {
		return res, fmt.Errorf("getSignificanceByNodeIDs log batch: %w", err)
	}

	return res, nil
}

// GetSignificanceForMemoryID returns dual-signal importance analysis scoped to
// the depth-hop neighbourhood of the given memory ID, clipped to the anchor's
// domain. Depth 2 is recommended; depth 1 produces near-uniform low scores.
func (st *Store) GetSignificanceForMemoryID(nodeID string, depth int, recencyWindowDays int, nodeKinds []string) (SignificanceResult, error) {
	ids, anchorDomain, err := st.neighbourhoodIDs(nodeID, depth)
	if err != nil {
		return SignificanceResult{}, err
	}
	return st.getSignificanceByNodeIDs(ids, anchorDomain, recencyWindowDays, nodeKinds)
}

type significanceLogEntry struct {
	nodeID, nodeLabel, rankType string
	score                       *float64
}

// logSignificanceBatch inserts multiple rows into significance_log in a single
// multi-row INSERT, replacing the O(N) per-node loop used previously.
func (st *Store) logSignificanceBatch(callID string, calledAt time.Time, domain string, limitN int, entries []significanceLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	const cols = 9
	placeholderRow := "(?, ?, ?, ?, ?, ?, ?, ?, ?)"
	rows := make([]string, len(entries))
	args := make([]interface{}, 0, len(entries)*cols)
	for i, e := range entries {
		rows[i] = placeholderRow
		args = append(args, shortID(), callID, calledAt, domain, limitN, e.nodeID, e.nodeLabel, e.rankType, e.score)
	}
	q := `INSERT INTO significance_log (id, call_id, called_at, domain, limit_n, node_id, node_label, rank_type, score) VALUES ` +
		strings.Join(rows, ", ")
	_, err := st.db.Exec(q, args...)
	return err
}

// LastTrustScores returns the most recently logged trust score from significance_log
// for each supplied node ID, keyed by node ID. Nodes with no prior trust log entry
// are absent from the map. Used by orient digest mode to detect trust worsening.
func (st *Store) LastTrustScores(nodeIDs []string) (map[string]float64, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	placeholders, args := inClause(nodeIDs)
	// ROW_NUMBER() partitioned by node_id ensures exactly one row per node even
	// when two log entries share an identical called_at timestamp.
	query := `SELECT node_id, score FROM (
	        SELECT node_id, score,
	               ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY called_at DESC) AS rn
	        FROM significance_log
	        WHERE node_id IN (` + placeholders + `)
	          AND rank_type = 'trust'
	          AND score IS NOT NULL
	      ) WHERE rn = 1`
	rows, err := st.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("LastTrustScores: %w", err)
	}
	defer rows.Close()
	result := make(map[string]float64)
	for rows.Next() {
		var nodeID string
		var score float64
		if err := rows.Scan(&nodeID, &score); err != nil {
			return nil, fmt.Errorf("LastTrustScores scan: %w", err)
		}
		result[nodeID] = score
	}
	return result, rows.Err()
}
