package tools

import (
	"fmt"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) annotateLifecycle(entries []leanEntry) ([]leanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	states, err := h.store.LifecycleStates(ids)
	if err != nil {
		return nil, err
	}
	for i, e := range entries {
		if s, ok := states[e.ID]; ok {
			entries[i].LifecycleState = string(s)
		}
	}
	return entries, nil
}

func (h *Handler) leanEntriesFromNodes(nodes []db.Node) ([]leanEntry, error) {
	return h.annotateLifecycle(toLeanEntries(nodes))
}

func (h *Handler) annotateScoredLifecycle(entries []scoredLeanEntry) ([]scoredLeanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	plain := make([]leanEntry, len(entries))
	for i, e := range entries {
		plain[i] = e.leanEntry
	}
	annotated, err := h.annotateLifecycle(plain)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].leanEntry = annotated[i]
	}
	return entries, nil
}

func (h *Handler) leanSearchResult(r *db.SearchResult) (leanSearchResult, error) {
	result := toLeanSearchResult(r)
	entries := make([]leanEntry, len(result.Nodes))
	for i, n := range result.Nodes {
		entries[i] = n.leanEntry
	}
	annotated, err := h.annotateLifecycle(entries)
	if err != nil {
		return leanSearchResult{}, err
	}
	for i := range result.Nodes {
		result.Nodes[i].leanEntry = annotated[i]
	}
	return result, nil
}

func (h *Handler) digestSearchResult(r *db.SearchResult) (digestSearchResult, error) {
	lean, err := h.leanSearchResult(r)
	if err != nil {
		return digestSearchResult{}, err
	}
	lines := make([]string, len(lean.Nodes))
	for i, n := range lean.Nodes {
		lines[i] = digestLineFromSearchNode(n)
	}
	edges := make([]leanEdge, len(r.Edges))
	for i, e := range r.Edges {
		edges[i] = leanEdge{FromNode: e.FromNode, ToNode: e.ToNode, Relationship: e.Relationship}
	}
	return digestSearchResult{Lines: lines, Edges: edges, Truncated: r.Truncated}, nil
}

func (h *Handler) leanSignificanceResult(r db.SignificanceResult) (leanSignificanceResult, error) {
	result := toLeanSignificanceResult(r)
	var err error
	result.Declared, err = h.annotateLifecycle(result.Declared)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.Structural, err = h.annotateScoredLifecycle(result.Structural)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.Uncurated, err = h.annotateScoredLifecycle(result.Uncurated)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.PotentiallyStale, err = h.annotateLifecycle(result.PotentiallyStale)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	return result, nil
}

func (h *Handler) digestSignificanceResult(r db.SignificanceResult) (digestSignificanceResult, error) {
	lean, err := h.leanSignificanceResult(r)
	if err != nil {
		return digestSignificanceResult{}, err
	}
	return digestSignificanceResult{
		Declared:                         digestLinesFromEntries(lean.Declared),
		Structural:                       digestLines(lean.Structural, digestLineFromScored),
		Uncurated:                        digestLines(lean.Uncurated, digestLineFromScored),
		PotentiallyStale:                 digestLinesFromEntries(lean.PotentiallyStale),
		CallID:                           lean.CallID,
		DeclaredResultsTruncated:         lean.DeclaredResultsTruncated,
		StructuralResultsTruncated:       lean.StructuralResultsTruncated,
		UncuratedResultsTruncated:        lean.UncuratedResultsTruncated,
		PotentiallyStaleResultsTruncated: lean.PotentiallyStaleResultsTruncated,
	}, nil
}

func (h *Handler) digestLinesFromNodes(nodes []db.Node) ([]string, error) {
	entries, err := h.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return digestLinesFromEntries(entries), nil
}

func (h *Handler) digestNodeList(nodes []db.Node, digest bool) (interface{}, error) {
	if !digest {
		return nodes, nil
	}
	lines, err := h.digestLinesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func (h *Handler) digestSection(entries []leanEntry, digest bool) (interface{}, error) {
	if !digest {
		return entries, nil
	}
	annotated, err := h.annotateLifecycle(entries)
	if err != nil {
		return nil, err
	}
	return digestLinesFromEntries(annotated), nil
}

func (h *Handler) orientLeanSection(nodes []db.Node, digest bool) (interface{}, error) {
	entries, err := h.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	if !digest {
		return entries, nil
	}
	return digestLinesFromEntries(entries), nil
}

func (h *Handler) orientScoredSection(entries []scoredLeanEntry, digest bool) (interface{}, error) {
	annotated, err := h.annotateScoredLifecycle(entries)
	if err != nil {
		return nil, err
	}
	if !digest {
		return annotated, nil
	}
	return digestLines(annotated, digestLineFromScored), nil
}

func (h *Handler) marshalRecentFromNodes(nodes []db.Node, resultsTruncated, digest bool) (*ToolResult, error) {
	entries, err := h.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return marshalRecentList(entries, resultsTruncated, digest)
}

func (h *Handler) digestLinesFromDrift(candidates []db.DriftCandidate) ([]string, error) {
	nodes := make([]db.Node, len(candidates))
	for i, c := range candidates {
		nodes[i] = c.Node
	}
	entries, err := h.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(candidates))
	for i, c := range candidates {
		reason := sanitiseDigestField(c.Reason)
		line := digestLineFromEntry(entries[i])
		lines[i] = fmt.Sprintf("%s (%s, edges: %d)", line, reason, c.EdgeCount)
	}
	return lines, nil
}
