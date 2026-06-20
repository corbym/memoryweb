package tools

import (
	"encoding/json"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) recentChanges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain        string `json:"domain"`
		Limit         int    `json:"limit"`
		GroupByDomain bool   `json:"group_by_domain"`
		Tags          string `json:"tags"`
		MemoryID      string `json:"memory_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}

	tags := splitTags(a.Tags)

	// memory_id scoping: neighbourhood-restricted, group_by_domain ignored.
	if a.MemoryID != "" {
		nodes, err := h.store.RecentChangesScoped(a.MemoryID, 2, "", tags, a.Limit)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(toLeanEntries(nodes), "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	// Tags only (no memory_id): domain-scoped with tag filter.
	if len(tags) > 0 {
		nodes, err := h.store.RecentChangesScoped("", 2, a.Domain, tags, a.Limit)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(toLeanEntries(nodes), "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	// group_by_domain only makes sense when no domain is specified.
	if a.GroupByDomain && a.Domain == "" {
		perDomain := a.Limit
		// Fetch a broad slice of recent nodes across all domains, then group.
		// 1000 is a generous upper bound; real deployments are unlikely to exceed it.
		all, err := h.store.RecentChanges("", 1000)
		if err != nil {
			return nil, err
		}
		// Group by domain, preserving updated_at DESC order within each group.
		grouped := make(map[string][]db.Node)
		domainOrder := []string{} // track insertion order for stable output
		for _, n := range all {
			if _, seen := grouped[n.Domain]; !seen {
				domainOrder = append(domainOrder, n.Domain)
			}
			if len(grouped[n.Domain]) < perDomain {
				grouped[n.Domain] = append(grouped[n.Domain], n)
			}
		}
		// Build ordered result.
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

	// Normal (non-grouped) behaviour.
	nodes, err := h.store.RecentChanges(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(toLeanEntries(nodes), "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
