package tools

import (
	"fmt"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) annotateLifecycle(entries []leanEntry) ([]leanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	states, err := hnd.store.LifecycleStates(ids)
	if err != nil {
		return nil, err
	}
	for i, entry := range entries {
		if state, ok := states[entry.ID]; ok {
			entries[i].LifecycleState = string(state)
		}
	}
	return entries, nil
}

func (hnd *Handler) leanEntriesFromNodes(nodes []db.Node) ([]leanEntry, error) {
	return hnd.annotateLifecycle(toLeanEntries(nodes))
}

func (hnd *Handler) annotateScoredLifecycle(entries []scoredLeanEntry) ([]scoredLeanEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	plain := make([]leanEntry, len(entries))
	for i, entry := range entries {
		plain[i] = entry.leanEntry
	}
	annotated, err := hnd.annotateLifecycle(plain)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].leanEntry = annotated[i]
	}
	return entries, nil
}

func (hnd *Handler) leanSearchResult(r *db.SearchResult) (leanSearchResult, error) {
	result := toLeanSearchResult(r)
	entries := make([]leanEntry, len(result.Nodes))
	for i, node := range result.Nodes {
		entries[i] = node.leanEntry
	}
	annotated, err := hnd.annotateLifecycle(entries)
	if err != nil {
		return leanSearchResult{}, err
	}
	for i := range result.Nodes {
		result.Nodes[i].leanEntry = annotated[i]
	}
	return result, nil
}

func (hnd *Handler) digestSearchResult(r *db.SearchResult) (digestSearchResult, error) {
	lean, err := hnd.leanSearchResult(r)
	if err != nil {
		return digestSearchResult{}, err
	}
	lines := make([]string, len(lean.Nodes))
	for i, node := range lean.Nodes {
		lines[i] = digestLineFromSearchNode(node)
	}
	edges := make([]leanEdge, len(r.Edges))
	for i, edge := range r.Edges {
		edges[i] = leanEdge{FromNode: edge.FromNode, ToNode: edge.ToNode, Relationship: edge.Relationship}
	}
	return digestSearchResult{Lines: lines, Edges: edges, Truncated: r.Truncated}, nil
}

func (hnd *Handler) leanSignificanceResult(r db.SignificanceResult) (leanSignificanceResult, error) {
	result := toLeanSignificanceResult(r)
	var err error
	result.Declared, err = hnd.annotateLifecycle(result.Declared)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.Structural, err = hnd.annotateScoredLifecycle(result.Structural)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.Uncurated, err = hnd.annotateScoredLifecycle(result.Uncurated)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	result.PotentiallyStale, err = hnd.annotateLifecycle(result.PotentiallyStale)
	if err != nil {
		return leanSignificanceResult{}, err
	}
	return result, nil
}

func (hnd *Handler) digestSignificanceResult(r db.SignificanceResult) (digestSignificanceResult, error) {
	lean, err := hnd.leanSignificanceResult(r)
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

func (hnd *Handler) digestLinesFromNodes(nodes []db.Node) ([]string, error) {
	entries, err := hnd.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return digestLinesFromEntries(entries), nil
}

func (hnd *Handler) digestNodeList(nodes []db.Node, digest bool) (interface{}, error) {
	if !digest {
		return nodes, nil
	}
	lines, err := hnd.digestLinesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func (hnd *Handler) digestSection(entries []leanEntry, digest bool) (interface{}, error) {
	if !digest {
		return entries, nil
	}
	annotated, err := hnd.annotateLifecycle(entries)
	if err != nil {
		return nil, err
	}
	return digestLinesFromEntries(annotated), nil
}

func (hnd *Handler) orientLeanSection(nodes []db.Node, digest bool) (interface{}, error) {
	entries, err := hnd.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	if !digest {
		return entries, nil
	}
	return digestLinesFromEntries(entries), nil
}

func (hnd *Handler) orientScoredSection(entries []scoredLeanEntry, digest bool) (interface{}, error) {
	annotated, err := hnd.annotateScoredLifecycle(entries)
	if err != nil {
		return nil, err
	}
	if !digest {
		return annotated, nil
	}
	return digestLines(annotated, digestLineFromScored), nil
}

func (hnd *Handler) marshalRecentFromNodes(nodes []db.Node, resultsTruncated, digest bool) (*ToolResult, error) {
	entries, err := hnd.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	return marshalRecentList(entries, resultsTruncated, digest)
}

func (hnd *Handler) digestLinesFromDrift(candidates []db.DriftCandidate) ([]string, error) {
	nodes := make([]db.Node, len(candidates))
	for i, candidate := range candidates {
		nodes[i] = candidate.Node
	}
	entries, err := hnd.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	lines := make([]string, len(candidates))
	for i, candidate := range candidates {
		reason := sanitiseDigestField(candidate.Reason)
		line := digestLineFromEntry(entries[i])
		lines[i] = fmt.Sprintf("%s (%s, edges: %d)", line, reason, candidate.EdgeCount)
	}
	return lines, nil
}
