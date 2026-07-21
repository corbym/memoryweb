package tools

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/corbym/memoryweb/db"
)

const (
	orientSignificantCap = 10
	orientRecentCap      = 5
	orientSpineCap       = 20
	orientRulesCap       = 20
	orientRelevantCap    = 5
)

type orientSectionTruncation struct {
	SignificantResultsTruncated   bool `json:"significant_results_truncated"`
	RecentResultsTruncated        bool `json:"recent_results_truncated"`
	DeclaredSpineResultsTruncated bool `json:"declared_spine_results_truncated"`
	RulesResultsTruncated         bool `json:"rules_results_truncated"`
	RelevantResultsTruncated      bool `json:"relevant_results_truncated"`
}

func cappedNodes(nodes []db.Node, cap int) ([]db.Node, bool) {
	return trimWithTruncation(nodes, cap)
}

func (h *Handler) orientCrossDomain(limit int) (*ToolResult, error) {
	if limit <= 0 {
		limit = orientRecentCap
	}
	if limit > 500 {
		limit = 500
	}
	// Fetch a broad slice of recent nodes across all domains then group,
	// reusing the same logic as recentChanges(group_by_domain=true).
	all, err := h.store.RecentChanges("", 1000, nil)
	if err != nil {
		return nil, err
	}

	type recentEntry struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		UpdatedAt string `json:"updated_at"`
	}
	type domainEntry struct {
		Domain                 string        `json:"domain"`
		Recent                 []recentEntry `json:"recent"`
		RecentResultsTruncated bool          `json:"recent_results_truncated"`
	}

	grouped := make(map[string][]recentEntry)
	domainTruncated := make(map[string]bool)
	domainOrder := []string{}
	resultsTruncated := false
	for _, n := range all {
		if _, seen := grouped[n.Domain]; !seen {
			domainOrder = append(domainOrder, n.Domain)
		}
		if len(grouped[n.Domain]) >= limit {
			domainTruncated[n.Domain] = true
			resultsTruncated = true
			continue
		}
		grouped[n.Domain] = append(grouped[n.Domain], recentEntry{
			ID:        n.ID,
			Label:     n.Label,
			UpdatedAt: n.UpdatedAt.Format(time.RFC3339),
		})
	}

	domains := make([]domainEntry, 0, len(domainOrder))
	for _, d := range domainOrder {
		domains = append(domains, domainEntry{
			Domain:                 d,
			Recent:                 grouped[d],
			RecentResultsTruncated: domainTruncated[d],
		})
	}

	resp := struct {
		Mode             string        `json:"mode"`
		Domains          []domainEntry `json:"domains"`
		ResultsTruncated bool          `json:"results_truncated"`
	}{
		Mode:             "cross_domain_snapshot",
		Domains:          domains,
		ResultsTruncated: resultsTruncated,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) orientWithTopic(domain, topic string, digest bool) (*ToolResult, error) {
	liveNodes, err := h.store.CountNodes(domain)
	if err != nil {
		return nil, err
	}
	archivedNodes, err := h.store.CountArchived(domain)
	if err != nil {
		return nil, err
	}
	staleCount, _ := h.store.CountStaleDrift(domain)

	result, err := h.store.SearchNodes(topic, domain, orientRelevantCap+1, "", nil)
	if err != nil {
		return nil, err
	}
	relevantTrunc := len(result.Nodes) > orientRelevantCap
	if relevantTrunc {
		result.Nodes = result.Nodes[:orientRelevantCap]
	}
	relevant := make([]leanEntry, len(result.Nodes))
	for i, nr := range result.Nodes {
		relevant[i] = toLeanEntry(nr.Node)
	}

	spineNodes, err := h.store.Timeline(domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return nil, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)
	spineEntries := toLeanEntries(spineNodes)

	recentRaw, err := h.store.RecentChanges(domain, orientRecentCap+1, nil)
	if err != nil {
		return nil, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)
	recentEntries := toLeanEntries(recentRaw)

	rulesNodes, rulesTrunc, err := h.store.GetStandingNodes(domain, orientRulesCap)
	if err != nil {
		return nil, err
	}
	rulesEntries := toLeanEntries(rulesNodes)

	var rulesField interface{}
	if len(rulesEntries) > 0 {
		rulesField = digestSection(rulesEntries, digest)
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
		orientSectionTruncation
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. relevant lists memories most similar to the supplied topic. declared_spine lists key decisions chronologically. rules lists the standing constraints and durable decisions that govern this domain. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		StaleCount:    staleCount,
		Rules:         rulesField,
		DeclaredSpine: digestSection(spineEntries, digest),
		Relevant:      digestSection(relevant, digest),
		Recent:        digestSection(recentEntries, digest),
		orientSectionTruncation: orientSectionTruncation{
			RelevantResultsTruncated:      relevantTrunc,
			RecentResultsTruncated:        recentTrunc,
			DeclaredSpineResultsTruncated: spineTrunc,
			RulesResultsTruncated:         rulesTrunc,
		},
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// orientDomainEntry builds the full orient data for one domain. Used by the
// multi-domain (domains array) path.
type orientDomainEntry struct {
	Domain        string      `json:"domain"`
	Rules         interface{} `json:"rules,omitempty"`
	DeclaredSpine interface{} `json:"declared_spine"`
	Significant   interface{} `json:"significant,omitempty"`
	Relevant      interface{} `json:"relevant,omitempty"`
	Recent        interface{} `json:"recent"`
	TotalNodes    int         `json:"total_nodes"`
	ArchivedNodes int         `json:"archived_nodes"`
	StaleCount    int         `json:"stale_count"`
	orientSectionTruncation
}

// buildDomainEntry builds lean orient data for a single domain (no top-level
// wrapper). topic and digest mirror the single-domain orient options. On an
// unknown/empty domain the sections are empty slices rather than errors.
func (h *Handler) buildDomainEntry(domain, topic string, digest bool) (orientDomainEntry, error) {
	liveNodes, err := h.store.CountNodes(domain)
	if err != nil {
		return orientDomainEntry{}, err
	}
	archivedNodes, err := h.store.CountArchived(domain)
	if err != nil {
		return orientDomainEntry{}, err
	}
	staleCount, _ := h.store.CountStaleDrift(domain)

	// Standing rules.
	rulesNodes, rulesTrunc, err := h.store.GetStandingNodes(domain, orientRulesCap)
	if err != nil {
		return orientDomainEntry{}, err
	}
	rulesEntries := toLeanEntries(rulesNodes)
	var rulesField interface{}
	if len(rulesEntries) > 0 {
		rulesField = digestSection(rulesEntries, digest)
	}

	// Declared spine.
	spineNodes, err := h.store.Timeline(domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return orientDomainEntry{}, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)
	spineEntries := toLeanEntries(spineNodes)

	// Recent.
	recentRaw, err := h.store.RecentChanges(domain, orientRecentCap+1, nil)
	if err != nil {
		return orientDomainEntry{}, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)
	recentEntries := toLeanEntries(recentRaw)

	entry := orientDomainEntry{
		Domain:        domain,
		Rules:         rulesField,
		DeclaredSpine: digestSection(spineEntries, digest),
		Recent:        digestSection(recentEntries, digest),
		TotalNodes:    liveNodes,
		ArchivedNodes: archivedNodes,
		StaleCount:    staleCount,
		orientSectionTruncation: orientSectionTruncation{
			RecentResultsTruncated:        recentTrunc,
			DeclaredSpineResultsTruncated: spineTrunc,
			RulesResultsTruncated:         rulesTrunc,
		},
	}

	if topic != "" {
		result, err := h.store.SearchNodes(topic, domain, orientRelevantCap+1, "", nil)
		if err != nil {
			return orientDomainEntry{}, err
		}
		relevantTrunc := len(result.Nodes) > orientRelevantCap
		if relevantTrunc {
			result.Nodes = result.Nodes[:orientRelevantCap]
		}
		relevant := make([]leanEntry, len(result.Nodes))
		for i, nr := range result.Nodes {
			relevant[i] = toLeanEntry(nr.Node)
		}
		entry.Relevant = digestSection(relevant, digest)
		entry.RelevantResultsTruncated = relevantTrunc
	} else {
		sigResult, err := h.store.GetSignificance(domain, orientSignificantCap, 90, nil, nil, 0)
		if err != nil {
			return orientDomainEntry{}, err
		}
		sigEntries := make([]scoredLeanEntry, len(sigResult.Structural))
		for i, sn := range sigResult.Structural {
			sigEntries[i] = scoredLeanEntry{
				leanEntry:       toLeanEntry(sn.Node),
				ImportanceScore: sn.ImportanceScore,
			}
		}
		sigEntries, err = h.annotateSignificantTrust(sigEntries)
		if err != nil {
			return orientDomainEntry{}, err
		}
		entry.Significant = digestScoredSection(sigEntries, digest)
		entry.SignificantResultsTruncated = sigResult.StructuralResultsTruncated
	}

	return entry, nil
}

func (h *Handler) summariseDomain(args json.RawMessage) (*ToolResult, error) {
	if argsEmpty(args) {
		return h.orientCrossDomain(0)
	}
	var a struct {
		Domain  string   `json:"domain"`
		Domains []string `json:"domains"`
		Topic   string   `json:"topic"`
		Digest  bool     `json:"digest"`
		Limit   int      `json:"limit"`
	}
	if err := decodeParams(args, &a, "orient"); err != nil {
		return nil, err
	}

	// domains field present with empty array → validation error.
	if a.Domains != nil && len(a.Domains) == 0 {
		return nil, fmt.Errorf("domains must not be empty — provide 1–5 domain names")
	}

	// No domain and no domains → cross-domain bootstrap.
	if a.Domain == "" && len(a.Domains) == 0 {
		return h.orientCrossDomain(a.Limit)
	}

	// Mutual exclusion: domain + domains together is an error.
	if a.Domain != "" && len(a.Domains) > 0 {
		return nil, fmt.Errorf("domain and domains are mutually exclusive — provide one or the other, not both")
	}

	// domains array validation.
	if len(a.Domains) > 0 {
		if len(a.Domains) > 5 {
			return nil, fmt.Errorf("domains accepts at most 5 items (got %d) — maximum is 5", len(a.Domains))
		}
		// Length 1: behave identically to orient(domain="X").
		if len(a.Domains) == 1 {
			a.Domain = a.Domains[0]
			a.Domains = nil
			// Fall through to single-domain path below.
		} else {
			// Multi-domain path: build each entry in input order.
			return h.orientMultiDomain(a.Domains, a.Topic, a.Digest)
		}
	}

	// Single-domain path (domain is set, domains is empty).
	if a.Topic != "" {
		return h.orientWithTopic(a.Domain, a.Topic, a.Digest)
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
	sigResult, err := h.store.GetSignificance(a.Domain, orientSignificantCap, 90, nil, nil, 0)
	if err != nil {
		return nil, err
	}

	// Step 3: fetch recent changes — capped at orientRecentCap.
	recentRaw, err := h.store.RecentChanges(a.Domain, orientRecentCap+1, nil)
	if err != nil {
		return nil, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)

	// Step 4: fetch declared decision spine (nodes with occurred_at set, chronological).
	spineNodes, err := h.store.Timeline(a.Domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return nil, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)

	// Step 4b: fetch standing nodes (rules)
	rulesNodes, rulesTrunc, err := h.store.GetStandingNodes(a.Domain, orientRulesCap)
	if err != nil {
		return nil, err
	}

	// Step 5: build lean response — id, label, truncated why_matters only; no description.
	recentEntries := toLeanEntries(recentRaw)
	spineEntries := toLeanEntries(spineNodes)
	sigEntries := make([]scoredLeanEntry, len(sigResult.Structural))
	for i, sn := range sigResult.Structural {
		sigEntries[i] = scoredLeanEntry{
			leanEntry:       toLeanEntry(sn.Node),
			ImportanceScore: sn.ImportanceScore,
		}
	}
	sigEntries, err = h.annotateSignificantTrust(sigEntries)
	if err != nil {
		return nil, err
	}
	rulesEntries := toLeanEntries(rulesNodes)

	var rulesField interface{}
	if len(rulesEntries) > 0 {
		rulesField = digestSection(rulesEntries, a.Digest)
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
		orientSectionTruncation
	}{
		SummaryHint:   "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. The declared_spine lists the key decisions that shaped this domain, in chronological order — weigh these heavily when summarising. rules lists the standing constraints and durable decisions that govern this domain. significant lists structurally load-bearing memories right now. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion: h.version,
		LiveNodes:     liveNodes,
		ArchivedNodes: archivedNodes,
		StaleCount:    staleCount,
		Rules:         rulesField,
		DeclaredSpine: digestSection(spineEntries, a.Digest),
		Significant:   digestScoredSection(sigEntries, a.Digest),
		Recent:        digestSection(recentEntries, a.Digest),
		orientSectionTruncation: orientSectionTruncation{
			SignificantResultsTruncated:   sigResult.StructuralResultsTruncated,
			RecentResultsTruncated:        recentTrunc,
			DeclaredSpineResultsTruncated: spineTrunc,
			RulesResultsTruncated:         rulesTrunc,
		},
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// orientMultiDomain handles orient(domains=[2..5 items], topic?, digest?).
func (h *Handler) orientMultiDomain(domains []string, topic string, digest bool) (*ToolResult, error) {
	entries := make([]orientDomainEntry, len(domains))
	for i, d := range domains {
		entry, err := h.buildDomainEntry(d, topic, digest)
		if err != nil {
			return nil, err
		}
		entries[i] = entry
	}

	resp := struct {
		SummaryHint   string              `json:"summary_hint"`
		Orientations  []orientDomainEntry `json:"orientations"`
		ServerVersion string              `json:"server_version"`
	}{
		SummaryHint:   "Synthesise each domain's section into its own narrative paragraph (max 300 words), covering: current state, known blockers, recent decisions, and open questions. declared_spine lists key decisions chronologically. rules lists standing constraints. significant/relevant lists load-bearing or topic-matched memories. recent shows where work was last happening. Plain prose per domain, no bullet points.",
		Orientations:  entries,
		ServerVersion: h.version,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
