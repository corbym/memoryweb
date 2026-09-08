package tools

import (
	"encoding/json"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) recentChanges(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var params struct {
		Domain        string `json:"domain"`
		Limit         int    `json:"limit"`
		GroupByDomain bool   `json:"group_by_domain"`
		Tags          string `json:"tags"`
		NodeKind      string `json:"node_kind"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		Digest        bool   `json:"digest"`
	}
	if err := decodeParams(args, &params, "recent"); err != nil {
		return nil, err
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	if params.Depth <= 0 {
		params.Depth = 2
	}

	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)

	if params.GroupByDomain && len(nodeKinds) > 0 {
		return errorResult("group_by_domain and node_kind cannot be used together"), nil
	}

	if params.MemoryID != "" {
		nodes, err := hnd.store.RecentChangesScoped(params.MemoryID, params.Depth, "", tags, nodeKinds, params.Limit+1)
		if err != nil {
			return nil, err
		}
		nodes, truncated := trimWithTruncation(nodes, params.Limit)
		return hnd.marshalRecentFromNodes(nodes, truncated, params.Digest)
	}

	if len(tags) > 0 || len(nodeKinds) > 0 {
		nodes, err := hnd.store.RecentChangesScoped("", params.Depth, params.Domain, tags, nodeKinds, params.Limit+1)
		if err != nil {
			return nil, err
		}
		nodes, truncated := trimWithTruncation(nodes, params.Limit)
		return hnd.marshalRecentFromNodes(nodes, truncated, params.Digest)
	}

	if params.GroupByDomain && params.Domain == "" {
		perDomain := params.Limit
		all, err := hnd.store.RecentChanges("", 1000, nil)
		if err != nil {
			return nil, err
		}
		grouped := make(map[string][]db.Node)
		domainOrder := []string{}
		resultsTruncated := false
		for _, node := range all {
			if _, seen := grouped[node.Domain]; !seen {
				domainOrder = append(domainOrder, node.Domain)
			}
			if len(grouped[node.Domain]) >= perDomain {
				resultsTruncated = true
				continue
			}
			grouped[node.Domain] = append(grouped[node.Domain], node)
		}
		if params.Digest {
			groups := make([]digestGroupedRecent, 0, len(domainOrder))
			for _, domain := range domainOrder {
				lines, err := hnd.digestLinesFromNodes(grouped[domain])
				if err != nil {
					return nil, err
				}
				groups = append(groups, digestGroupedRecent{Domain: domain, Lines: lines})
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
		for _, domain := range domainOrder {
			entries, err := hnd.leanEntriesFromNodes(grouped[domain])
			if err != nil {
				return nil, err
			}
			groups = append(groups, groupedResult{Domain: domain, Nodes: entries})
		}
		out := struct {
			Groups           []groupedResult `json:"groups"`
			ResultsTruncated bool            `json:"results_truncated"`
		}{Groups: groups, ResultsTruncated: resultsTruncated}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	nodes, err := hnd.store.RecentChanges(params.Domain, params.Limit+1, nodeKinds)
	if err != nil {
		return nil, err
	}
	nodes, truncated := trimWithTruncation(nodes, params.Limit)
	return hnd.marshalRecentFromNodes(nodes, truncated, params.Digest)
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
