package tools

import (
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

// leanSearchNode and leanEdge strip search results down to the lean-entry
// contract: id, label, why_matters (truncated), occurred_at where set, and
// semantic_distance (a score, not content — always kept). Edges drop to
// from/to IDs and relationship type; narrative text requires recall(id).
type leanSearchNode struct {
	leanEntry
	Domain           string   `json:"domain,omitempty"`
	NodeKind         string   `json:"node_kind,omitempty"`
	SemanticDistance *float64 `json:"semantic_distance,omitempty"`
}

type leanEdge struct {
	FromNode     string `json:"from_memory"`
	ToNode       string `json:"to_memory"`
	Relationship string `json:"relationship"`
}

type leanSearchResult struct {
	Nodes     []leanSearchNode `json:"nodes"`
	Edges     []leanEdge       `json:"edges"`
	Truncated bool             `json:"truncated,omitempty"`
}

func toLeanSearchResult(r *db.SearchResult) leanSearchResult {
	nodes := make([]leanSearchNode, len(r.Nodes))
	for i, nodeResult := range r.Nodes {
		nodes[i] = leanSearchNode{
			leanEntry:        toLeanEntry(nodeResult.Node),
			Domain:           nodeResult.Node.Domain,
			NodeKind:         nodeResult.Node.NodeKind,
			SemanticDistance: nodeResult.SemanticDistance,
		}
	}
	edges := make([]leanEdge, len(r.Edges))
	for i, edge := range r.Edges {
		edges[i] = leanEdge{FromNode: edge.FromNode, ToNode: edge.ToNode, Relationship: edge.Relationship}
	}
	return leanSearchResult{Nodes: nodes, Edges: edges, Truncated: r.Truncated}
}

func truncateWhy(s string) (string, bool) {
	const limit = 150
	if len(s) <= limit {
		return s, false
	}
	sub := s[:limit]
	lastBoundary := -1
	for i := 0; i < len(sub); i++ {
		if sub[i] == '.' || sub[i] == '!' || sub[i] == '?' {
			next := i + 1
			if next >= len(sub) || sub[next] == ' ' || sub[next] == '\n' || sub[next] == '\t' {
				lastBoundary = next
			}
		}
	}
	if lastBoundary > 0 {
		return strings.TrimRight(s[:lastBoundary], " \t\n"), true
	}
	return sub + "...", true
}

// leanEntry is the shared lean-node shape used across all list-shaped retrieval
// tools (orient, search, recent, significance, history): id, label, why_matters
// (truncated at 150 chars, sentence-boundary aware), and occurred_at where set.
// description and tags are always omitted — callers needing full content use
// recall(id).
type leanEntry struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	WhyMatters     string  `json:"why_matters,omitempty"`
	Truncated      bool    `json:"truncated,omitempty"`
	OccurredAt     *string `json:"occurred_at,omitempty"`
	LifecycleState string  `json:"lifecycle_state,omitempty"`
}

func toLeanEntry(node db.Node) leanEntry {
	why, truncated := truncateWhy(node.WhyMatters)
	entry := leanEntry{ID: node.ID, Label: node.Label, WhyMatters: why, Truncated: truncated}
	if node.OccurredAt != nil {
		date := node.OccurredAt.Format("2006-01-02")
		entry.OccurredAt = &date
	}
	return entry
}

func toLeanEntries(nodes []db.Node) []leanEntry {
	entries := make([]leanEntry, len(nodes))
	for i, node := range nodes {
		entries[i] = toLeanEntry(node)
	}
	return entries
}

// scoredLeanEntry pairs a lean node entry with a structural importance score.
// Used by orient's significant section and the significance tool's structural
// and uncurated sections.
type scoredLeanEntry struct {
	leanEntry
	ImportanceScore float64 `json:"importance_score"`
	Trust           string  `json:"trust,omitempty"`
}

// leanSignificanceResult mirrors db.SignificanceResult with lean node entries —
// id, label, truncated why_matters, occurred_at where set; description and
// tags omitted, consistent with the other lean retrieval tools.
type leanSignificanceResult struct {
	Declared                         []leanEntry       `json:"declared"`
	Structural                       []scoredLeanEntry `json:"structural"`
	Uncurated                        []scoredLeanEntry `json:"uncurated"`
	PotentiallyStale                 []leanEntry       `json:"potentially_stale"`
	CallID                           string            `json:"call_id"`
	DeclaredResultsTruncated         bool              `json:"declared_results_truncated"`
	StructuralResultsTruncated       bool              `json:"structural_results_truncated"`
	UncuratedResultsTruncated        bool              `json:"uncurated_results_truncated"`
	PotentiallyStaleResultsTruncated bool              `json:"potentially_stale_results_truncated"`
}

// leanTrustNode mirrors db.TrustNode with a lean entry — id, label, truncated
// why_matters, occurred_at where set; description and tags omitted, consistent
// with the other lean retrieval tools. node_kind, trust_score, and trust_basis
// are content the trust signal exists to surface, so they're always kept.
type leanTrustNode struct {
	leanEntry
	NodeKind   string  `json:"node_kind,omitempty"`
	TrustScore float64 `json:"trust_score"`
	TrustBasis string  `json:"trust_basis"`
}

type leanTrustResult struct {
	Nodes  []leanTrustNode `json:"nodes"`
	CallID string          `json:"call_id"`
}

func toLeanTrustResult(r db.TrustResult) leanTrustResult {
	nodes := make([]leanTrustNode, len(r.Nodes))
	for i, node := range r.Nodes {
		nodes[i] = leanTrustNode{
			leanEntry:  toLeanEntry(node.Node),
			NodeKind:   node.NodeKind,
			TrustScore: node.TrustScore,
			TrustBasis: node.TrustBasis,
		}
	}
	return leanTrustResult{Nodes: nodes, CallID: r.CallID}
}

func toLeanSignificanceResult(r db.SignificanceResult) leanSignificanceResult {
	structural := make([]scoredLeanEntry, len(r.Structural))
	for i, scoredNode := range r.Structural {
		structural[i] = scoredLeanEntry{leanEntry: toLeanEntry(scoredNode.Node), ImportanceScore: scoredNode.ImportanceScore}
	}
	uncurated := make([]scoredLeanEntry, len(r.Uncurated))
	for i, scoredNode := range r.Uncurated {
		uncurated[i] = scoredLeanEntry{leanEntry: toLeanEntry(scoredNode.Node), ImportanceScore: scoredNode.ImportanceScore}
	}
	return leanSignificanceResult{
		Declared:                         toLeanEntries(r.Declared),
		Structural:                       structural,
		Uncurated:                        uncurated,
		PotentiallyStale:                 toLeanEntries(r.PotentiallyStale),
		CallID:                           r.CallID,
		DeclaredResultsTruncated:         r.DeclaredResultsTruncated,
		StructuralResultsTruncated:       r.StructuralResultsTruncated,
		UncuratedResultsTruncated:        r.UncuratedResultsTruncated,
		PotentiallyStaleResultsTruncated: r.PotentiallyStaleResultsTruncated,
	}
}

// ── digest mode — single-line text per node (opt-in, default off) ─────────────

func sanitiseDigestField(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

func digestLineFromEntry(entry leanEntry) string {
	label := sanitiseDigestField(entry.Label)
	line := fmt.Sprintf("[%s] %s", entry.ID, label)
	if entry.WhyMatters != "" {
		line += " — " + sanitiseDigestField(entry.WhyMatters)
	}
	if entry.OccurredAt != nil {
		line += fmt.Sprintf(" (%s)", *entry.OccurredAt)
	}
	if entry.LifecycleState != "" {
		line += fmt.Sprintf(" (%s)", entry.LifecycleState)
	}
	return line
}

func digestLinesFromEntries(entries []leanEntry) []string {
	return digestLines(entries, digestLineFromEntry)
}

func digestLineFromScored(entry scoredLeanEntry) string {
	line := fmt.Sprintf("%s (score: %.2f)", digestLineFromEntry(entry.leanEntry), entry.ImportanceScore)
	if entry.Trust != "" {
		line += fmt.Sprintf(" (trust: %s)", sanitiseDigestField(entry.Trust))
	}
	return line
}

func digestLineFromSearchNode(node leanSearchNode) string {
	label := sanitiseDigestField(node.Label)
	var line string
	if node.WhyMatters != "" {
		excerpt := sanitiseDigestField(node.WhyMatters)
		line = fmt.Sprintf("[%s] %s — %s (%s, %s)", node.ID, label, excerpt, node.Domain, node.NodeKind)
	} else {
		line = fmt.Sprintf("[%s] %s (%s, %s)", node.ID, label, node.Domain, node.NodeKind)
	}
	if node.LifecycleState != "" {
		line += fmt.Sprintf(" (%s)", node.LifecycleState)
	}
	if node.SemanticDistance != nil && *node.SemanticDistance > 0 {
		line += fmt.Sprintf("  %.2f", *node.SemanticDistance)
	}
	return line
}

func digestLineFromTrust(node leanTrustNode) string {
	basis := sanitiseDigestField(node.TrustBasis)
	return fmt.Sprintf("%s (trust: %.2f) %s", digestLineFromEntry(node.leanEntry), node.TrustScore, basis)
}

type digestSearchResult struct {
	Lines     []string   `json:"lines"`
	Edges     []leanEdge `json:"edges,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

func toDigestSearchResult(r *db.SearchResult) digestSearchResult {
	lines := make([]string, len(r.Nodes))
	for i, nodeResult := range r.Nodes {
		lines[i] = digestLineFromSearchNode(leanSearchNode{leanEntry: toLeanEntry(nodeResult.Node), SemanticDistance: nodeResult.SemanticDistance})
	}
	edges := make([]leanEdge, len(r.Edges))
	for i, edge := range r.Edges {
		edges[i] = leanEdge{FromNode: edge.FromNode, ToNode: edge.ToNode, Relationship: edge.Relationship}
	}
	return digestSearchResult{Lines: lines, Edges: edges, Truncated: r.Truncated}
}

type digestSignificanceResult struct {
	Declared                         []string `json:"declared"`
	Structural                       []string `json:"structural"`
	Uncurated                        []string `json:"uncurated"`
	PotentiallyStale                 []string `json:"potentially_stale"`
	CallID                           string   `json:"call_id"`
	DeclaredResultsTruncated         bool     `json:"declared_results_truncated"`
	StructuralResultsTruncated       bool     `json:"structural_results_truncated"`
	UncuratedResultsTruncated        bool     `json:"uncurated_results_truncated"`
	PotentiallyStaleResultsTruncated bool     `json:"potentially_stale_results_truncated"`
}

func toDigestSignificanceResult(r db.SignificanceResult) digestSignificanceResult {
	structural := make([]string, len(r.Structural))
	for i, scoredNode := range r.Structural {
		structural[i] = digestLineFromScored(scoredLeanEntry{leanEntry: toLeanEntry(scoredNode.Node), ImportanceScore: scoredNode.ImportanceScore})
	}
	uncurated := make([]string, len(r.Uncurated))
	for i, scoredNode := range r.Uncurated {
		uncurated[i] = digestLineFromScored(scoredLeanEntry{leanEntry: toLeanEntry(scoredNode.Node), ImportanceScore: scoredNode.ImportanceScore})
	}
	return digestSignificanceResult{
		Declared:                         digestLinesFromEntries(toLeanEntries(r.Declared)),
		Structural:                       structural,
		Uncurated:                        uncurated,
		PotentiallyStale:                 digestLinesFromEntries(toLeanEntries(r.PotentiallyStale)),
		CallID:                           r.CallID,
		DeclaredResultsTruncated:         r.DeclaredResultsTruncated,
		StructuralResultsTruncated:       r.StructuralResultsTruncated,
		UncuratedResultsTruncated:        r.UncuratedResultsTruncated,
		PotentiallyStaleResultsTruncated: r.PotentiallyStaleResultsTruncated,
	}
}

type digestTrustResult struct {
	Lines  []string `json:"lines"`
	CallID string   `json:"call_id"`
}

func toDigestTrustResult(r db.TrustResult) digestTrustResult {
	lines := make([]string, len(r.Nodes))
	for i, node := range r.Nodes {
		lines[i] = digestLineFromTrust(leanTrustNode{
			leanEntry:  toLeanEntry(node.Node),
			NodeKind:   node.NodeKind,
			TrustScore: node.TrustScore,
			TrustBasis: node.TrustBasis,
		})
	}
	return digestTrustResult{Lines: lines, CallID: r.CallID}
}

type digestGroupedRecent struct {
	Domain string   `json:"domain"`
	Lines  []string `json:"lines"`
}

// digestLines maps items to compact single-line strings for digest mode.
func digestLines[T any](items []T, render func(T) string) []string {
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = render(item)
	}
	return lines
}

func digestLineFromDrift(c db.DriftCandidate) string {
	reason := sanitiseDigestField(c.Reason)
	line := digestLineFromEntry(toLeanEntry(c.Node))
	return fmt.Sprintf("%s (%s, edges: %d)", line, reason, c.EdgeCount)
}

func digestLineFromPlaceholder(c db.PlaceholderCandidate) string {
	reason := sanitiseDigestField(c.Reason)
	line := digestLineFromEntry(toLeanEntry(c.Node))
	return fmt.Sprintf("%s (%s)", line, reason)
}

func digestLinesFromPlaceholders(cs []db.PlaceholderCandidate) []string {
	return digestLines(cs, digestLineFromPlaceholder)
}
