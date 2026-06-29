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
		MemoryID      string `json:"memory_id"`
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

	tags := splitTags(a.Tags)

	if a.MemoryID != "" {
		nodes, err := h.store.RecentChangesScoped(a.MemoryID, 2, "", tags, a.Limit)
		if err != nil {
			return nil, err
		}
		return marshalRecentList(toLeanEntries(nodes), a.Digest)
	}

	if len(tags) > 0 {
		nodes, err := h.store.RecentChangesScoped("", 2, a.Domain, tags, a.Limit)
		if err != nil {
			return nil, err
		}
		return marshalRecentList(toLeanEntries(nodes), a.Digest)
	}

	if a.GroupByDomain && a.Domain == "" {
		perDomain := a.Limit
		all, err := h.store.RecentChanges("", 1000)
		if err != nil {
			return nil, err
		}
		grouped := make(map[string][]db.Node)
		domainOrder := []string{}
		for _, n := range all {
			if _, seen := grouped[n.Domain]; !seen {
				domainOrder = append(domainOrder, n.Domain)
			}
			if len(grouped[n.Domain]) < perDomain {
				grouped[n.Domain] = append(grouped[n.Domain], n)
			}
		}
		if a.Digest {
			out := make([]digestGroupedRecent, 0, len(domainOrder))
			for _, d := range domainOrder {
				out = append(out, digestGroupedRecent{
					Domain: d,
					Lines:  digestLinesFromEntries(toLeanEntries(grouped[d])),
				})
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
		}
		type groupedResult struct {
			Domain string      `json:"domain"`
			Nodes  []leanEntry `json:"nodes"`
		}
		out := make([]groupedResult, 0, len(domainOrder))
		for _, d := range domainOrder {
			out = append(out, groupedResult{Domain: d, Nodes: toLeanEntries(grouped[d])})
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	nodes, err := h.store.RecentChanges(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	return marshalRecentList(toLeanEntries(nodes), a.Digest)
}

func marshalRecentList(entries []leanEntry, digest bool) (*ToolResult, error) {
	if digest {
		out := struct {
			Lines []string `json:"lines"`
		}{Lines: digestLinesFromEntries(entries)}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	b, _ := json.MarshalIndent(entries, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
