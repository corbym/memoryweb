package tools

import (
	"encoding/json"
	"time"
)

func (h *Handler) orientCrossDomain() (*ToolResult, error) {
	// Fetch a broad slice of recent nodes across all domains then group,
	// reusing the same logic as recentChanges(group_by_domain=true).
	all, err := h.store.RecentChanges("", 1000)
	if err != nil {
		return nil, err
	}

	type recentEntry struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		UpdatedAt string `json:"updated_at"`
	}
	type domainEntry struct {
		Domain string        `json:"domain"`
		Recent []recentEntry `json:"recent"`
	}

	const perDomain = 5
	grouped := make(map[string][]recentEntry)
	domainOrder := []string{}
	for _, n := range all {
		if _, seen := grouped[n.Domain]; !seen {
			domainOrder = append(domainOrder, n.Domain)
		}
		if len(grouped[n.Domain]) < perDomain {
			grouped[n.Domain] = append(grouped[n.Domain], recentEntry{
				ID:        n.ID,
				Label:     n.Label,
				UpdatedAt: n.UpdatedAt.Format(time.RFC3339),
			})
		}
	}

	domains := make([]domainEntry, 0, len(domainOrder))
	for _, d := range domainOrder {
		domains = append(domains, domainEntry{Domain: d, Recent: grouped[d]})
	}

	resp := struct {
		Mode    string        `json:"mode"`
		Domains []domainEntry `json:"domains"`
	}{
		Mode:    "cross_domain_snapshot",
		Domains: domains,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) orientWithTopic(domain, topic string) (*ToolResult, error) {
	liveNodes, err := h.store.CountNodes(domain)
	if err != nil {
		return nil, err
	}
	archivedNodes, err := h.store.CountArchived(domain)
	if err != nil {
		return nil, err
	}
	staleCount, _ := h.store.CountStaleDrift(domain)

	result, err := h.store.SearchNodes(topic, domain, 5, "")
	if err != nil {
		return nil, err
	}

	relevant := make([]leanEntry, len(result.Nodes))
	for i, nr := range result.Nodes {
		relevant[i] = toLeanEntry(nr.Node)
	}

	spineNodes, err := h.store.Timeline(domain, true, nil, nil, nil, 20)
	if err != nil {
		return nil, err
	}
	spineEntries := toLeanEntries(spineNodes)

	recent, err := h.store.RecentChanges(domain, 5)
	if err != nil {
		return nil, err
	}
	recentEntries := toLeanEntries(recent)

	rulesNodes, err := h.store.GetStandingNodes(domain)
	if err != nil {
		return nil, err
	}
	rulesEntries := toLeanEntries(rulesNodes)
	var rulesField interface{}
	if len(rulesEntries) > 0 {
		rulesField = rulesEntries
	}

	resp := struct {
		SummaryHint   string      `json:"summary_hint"`
		ServerVersion string      `json:"server_version"`
		LiveNodes     int         `json:"live_nodes"`
		ArchivedNodes int         `json:"archived_nodes"`
		StaleCount    int         `json:"stale_count"`
		Rules         interface{} `json:"rules,omitempty"`
		DeclaredSpine interface{} `json:"declared_spine"`
		Relevant      interface{} `json:"relevant"`
		Recent        interface{} `json:"recent"`
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. relevant lists memories most similar to the supplied topic. declared_spine lists key decisions chronologically. rules lists the standing constraints and durable decisions that govern this domain. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		StaleCount:    staleCount,
		Rules:         rulesField,
		DeclaredSpine: spineEntries,
		Relevant:      relevant,
		Recent:        recentEntries,
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) summariseDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Topic  string `json:"topic"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	if a.Domain == "" {
		return h.orientCrossDomain()
	}

	if a.Topic != "" {
		return h.orientWithTopic(a.Domain, a.Topic)
	}

	// Step 1: count live and archived nodes for the domain.
	liveNodes, err := h.store.CountNodes(a.Domain)
	if err != nil {
		return nil, err
	}
	if liveNodes == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "Nothing has been filed for this domain yet."}}}, nil
	}
	archivedNodes, err := h.store.CountArchived(a.Domain)
	if err != nil {
		return nil, err
	}
	staleCount, _ := h.store.CountStaleDrift(a.Domain)

	// Step 2: fetch significant nodes (structurally load-bearing, recency-weighted inbound degree).
	sigResult, err := h.store.GetSignificance(a.Domain, 10, 90, nil)
	if err != nil {
		return nil, err
	}

	// Step 3: fetch recent changes — capped at 5.
	recent, err := h.store.RecentChanges(a.Domain, 5)
	if err != nil {
		return nil, err
	}

	// Step 4: fetch declared decision spine (nodes with occurred_at set, chronological).
	spineNodes, err := h.store.Timeline(a.Domain, true, nil, nil, nil, 20)
	if err != nil {
		return nil, err
	}

	// Step 4b: fetch standing nodes (rules)
	rulesNodes, err := h.store.GetStandingNodes(a.Domain)
	if err != nil {
		return nil, err
	}

	// Step 5: build lean response — id, label, truncated why_matters only; no description.
	recentEntries := toLeanEntries(recent)
	spineEntries := toLeanEntries(spineNodes)
	sigEntries := make([]scoredLeanEntry, len(sigResult.Structural))
	for i, sn := range sigResult.Structural {
		sigEntries[i] = scoredLeanEntry{
			leanEntry:       toLeanEntry(sn.Node),
			ImportanceScore: sn.ImportanceScore,
		}
	}
	rulesEntries := toLeanEntries(rulesNodes)
	var rulesField interface{}
	if len(rulesEntries) > 0 {
		rulesField = rulesEntries
	}

	resp := struct {
		SummaryHint   string      `json:"summary_hint"`
		ServerVersion string      `json:"server_version"`
		LiveNodes     int         `json:"live_nodes"`
		ArchivedNodes int         `json:"archived_nodes"`
		StaleCount    int         `json:"stale_count"`
		Rules         interface{} `json:"rules,omitempty"`
		DeclaredSpine interface{} `json:"declared_spine"`
		Significant   interface{} `json:"significant"`
		Recent        interface{} `json:"recent"`
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. The declared_spine lists the key decisions that shaped this domain, in chronological order — weigh these heavily when summarising. rules lists the standing constraints and durable decisions that govern this domain. significant lists structurally load-bearing memories right now. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		StaleCount:    staleCount,
		Rules:         rulesField,
		DeclaredSpine: spineEntries,
		Significant:   sigEntries,
		Recent:        recentEntries,
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
