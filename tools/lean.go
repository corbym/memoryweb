package tools

import (
	"strings"

	"github.com/corbym/memoryweb/db"
)

// leanSearchNode and leanEdge strip search results down to the lean-entry
// contract: id, label, why_matters (truncated), occurred_at where set, and
// semantic_distance (a score, not content — always kept). Edges drop to
// from/to IDs and relationship type; narrative text requires recall(id).
type leanSearchNode struct {
	leanEntry
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
	for i, nr := range r.Nodes {
		nodes[i] = leanSearchNode{leanEntry: toLeanEntry(nr.Node), SemanticDistance: nr.SemanticDistance}
	}
	edges := make([]leanEdge, len(r.Edges))
	for i, e := range r.Edges {
		edges[i] = leanEdge{FromNode: e.FromNode, ToNode: e.ToNode, Relationship: e.Relationship}
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
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	WhyMatters string  `json:"why_matters,omitempty"`
	Truncated  bool    `json:"truncated,omitempty"`
	OccurredAt *string `json:"occurred_at,omitempty"`
}

func toLeanEntry(n db.Node) leanEntry {
	why, truncated := truncateWhy(n.WhyMatters)
	e := leanEntry{ID: n.ID, Label: n.Label, WhyMatters: why, Truncated: truncated}
	if n.OccurredAt != nil {
		s := n.OccurredAt.Format("2006-01-02")
		e.OccurredAt = &s
	}
	return e
}

func toLeanEntries(nodes []db.Node) []leanEntry {
	entries := make([]leanEntry, len(nodes))
	for i, n := range nodes {
		entries[i] = toLeanEntry(n)
	}
	return entries
}

// scoredLeanEntry pairs a lean node entry with a structural importance score.
// Used by orient's significant section and the significance tool's structural
// and uncurated sections.
type scoredLeanEntry struct {
	leanEntry
	ImportanceScore float64 `json:"importance_score"`
}

// leanSignificanceResult mirrors db.SignificanceResult with lean node entries —
// id, label, truncated why_matters, occurred_at where set; description and
// tags omitted, consistent with the other lean retrieval tools.
type leanSignificanceResult struct {
	Declared         []leanEntry       `json:"declared"`
	Structural       []scoredLeanEntry `json:"structural"`
	Uncurated        []scoredLeanEntry `json:"uncurated"`
	PotentiallyStale []leanEntry       `json:"potentially_stale"`
	CallID           string            `json:"call_id"`
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
	for i, n := range r.Nodes {
		nodes[i] = leanTrustNode{
			leanEntry:  toLeanEntry(n.Node),
			NodeKind:   n.NodeKind,
			TrustScore: n.TrustScore,
			TrustBasis: n.TrustBasis,
		}
	}
	return leanTrustResult{Nodes: nodes, CallID: r.CallID}
}

func toLeanSignificanceResult(r db.SignificanceResult) leanSignificanceResult {
	structural := make([]scoredLeanEntry, len(r.Structural))
	for i, sn := range r.Structural {
		structural[i] = scoredLeanEntry{leanEntry: toLeanEntry(sn.Node), ImportanceScore: sn.ImportanceScore}
	}
	uncurated := make([]scoredLeanEntry, len(r.Uncurated))
	for i, sn := range r.Uncurated {
		uncurated[i] = scoredLeanEntry{leanEntry: toLeanEntry(sn.Node), ImportanceScore: sn.ImportanceScore}
	}
	return leanSignificanceResult{
		Declared:         toLeanEntries(r.Declared),
		Structural:       structural,
		Uncurated:        uncurated,
		PotentiallyStale: toLeanEntries(r.PotentiallyStale),
		CallID:           r.CallID,
	}
}
