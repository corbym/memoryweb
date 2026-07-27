package tools

import (
	"encoding/json"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) recentChanges(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var a struct {
		Domain        string `json:"domain"`
		Limit         int    `json:"limit"`
		GroupByDomain bool   `json:"group_by_domain"`
		Tags          string `json:"tags"`
		NodeKind      string `json:"node_kind"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		Digest        bool   `json:"digest"`
	}
	if err := decodeParams(args, &a, "recent"); err != nil {
		return nil, err
	}

	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	if a.Depth <= 0 {
		a.Depth = 2
	}

	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)

	if a.GroupByDomain && len(nodeKinds) > 0 {
		return errorResult("group_by_domain and node_kind cannot be used together"), nil
	}

	if a.MemoryID != "" {
		nodes, err := h.store.RecentChangesScoped(a.MemoryID, a.Depth, "", tags, nodeKinds, a.Limit+1)
		if err != nil {
			return nil, err
		}
		nodes, truncated := trimWithTruncation(nodes, a.Limit)
		return h.marshalRecentFromNodes(nodes, truncated, a.Digest)
	}

	if len(tags) > 0 || len(nodeKinds) > 0 {
		nodes, err := h.store.RecentChangesScoped("", a.Depth, a.Domain, tags, nodeKinds, a.Limit+1)
		if err != nil {
			return nil, err
		}
		nodes, truncated := trimWithTruncation(nodes, a.Limit)
		return h.marshalRecentFromNodes(nodes, truncated, a.Digest)
	}

	if a.GroupByDomain && a.Domain == "" {
		perDomain := a.Limit
		all, err := h.store.RecentChanges("", 1000, nil)
		if err != nil {
			return nil, err
		}
		grouped := make(map[string][]db.Node)
		domainOrder := []string{}
		resultsTruncated := false
		for _, n := range all {
			if _, seen := grouped[n.Domain]; !seen {
				domainOrder = append(domainOrder, n.Domain)
			}
			if len(grouped[n.Domain]) >= perDomain {
				resultsTruncated = true
				continue
			}
			grouped[n.Domain] = append(grouped[n.Domain], n)
		}
		if a.Digest {
			groups := make([]digestGroupedRecent, 0, len(domainOrder))
			for _, d := range domainOrder {
				lines, err := h.digestLinesFromNodes(grouped[d])
				if err != nil {
					return nil, err
				}
				groups = append(groups, digestGroupedRecent{Domain: d, Lines: lines})
			}
			out := struct {
				Groups           []digestGroupedRecent `json:"groups"`
				ResultsTruncated bool                  `json:"results_truncated"`
			}{Groups: groups, ResultsTruncated: resultsTruncated}
			b, _ := json.MarshalIndent(out, "", "  ")
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
		}
		type groupedResult struct {
			Domain string      `json:"domain"`
			Nodes  []leanEntry `json:"nodes"`
		}
		groups := make([]groupedResult, 0, len(domainOrder))
		for _, d := range domainOrder {
			entries, err := h.leanEntriesFromNodes(grouped[d])
			if err != nil {
				return nil, err
			}
			groups = append(groups, groupedResult{Domain: d, Nodes: entries})
		}
		out := struct {
			Groups           []groupedResult `json:"groups"`
			ResultsTruncated bool            `json:"results_truncated"`
		}{Groups: groups, ResultsTruncated: resultsTruncated}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	nodes, err := h.store.RecentChanges(a.Domain, a.Limit+1, nodeKinds)
	if err != nil {
		return nil, err
	}
	nodes, truncated := trimWithTruncation(nodes, a.Limit)
	return h.marshalRecentFromNodes(nodes, truncated, a.Digest)
}

func marshalRecentList(entries []leanEntry, resultsTruncated bool, digest bool) (*ToolResult, error) {
	if digest {
		out := struct {
			Lines            []string `json:"lines"`
			ResultsTruncated bool     `json:"results_truncated"`
		}{Lines: digestLinesFromEntries(entries), ResultsTruncated: resultsTruncated}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	out := struct {
		Nodes            []leanEntry `json:"nodes"`
		ResultsTruncated bool        `json:"results_truncated"`
	}{Nodes: entries, ResultsTruncated: resultsTruncated}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
