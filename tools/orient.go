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

type crossDomainRecentEntry struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	UpdatedAt      string `json:"updated_at"`
	LifecycleState string `json:"lifecycle_state,omitempty"`
}

func (hnd *Handler) crossDomainRecentEntries(nodes []db.Node) ([]crossDomainRecentEntry, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}
	states, err := hnd.store.LifecycleStates(ids)
	if err != nil {
		return nil, err
	}
	entries := make([]crossDomainRecentEntry, len(nodes))
	for i, node := range nodes {
		entries[i] = crossDomainRecentEntry{
			ID:             node.ID,
			Label:          node.Label,
			UpdatedAt:      node.UpdatedAt.Format(time.RFC3339),
			LifecycleState: string(states[node.ID]),
		}
	}
	return entries, nil
}

func (hnd *Handler) orientCrossDomain(limit int, digest bool) (*ToolResult, error) {
	if limit <= 0 {
		limit = orientRecentCap
	}
	if limit > 500 {
		limit = 500
	}
	// Fetch a broad slice of recent nodes across all domains then group,
	// reusing the same logic as recentChanges(group_by_domain=true).
	all, err := hnd.store.RecentChanges("", 1000, nil)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string][]db.Node)
	domainTruncated := make(map[string]bool)
	domainOrder := []string{}
	resultsTruncated := false
	for _, node := range all {
		if _, seen := grouped[node.Domain]; !seen {
			domainOrder = append(domainOrder, node.Domain)
		}
		if len(grouped[node.Domain]) >= limit {
			domainTruncated[node.Domain] = true
			resultsTruncated = true
			continue
		}
		grouped[node.Domain] = append(grouped[node.Domain], node)
	}

	if digest {
		type digestDomainEntry struct {
			Domain                 string   `json:"domain"`
			Recent                 []string `json:"recent"`
			RecentResultsTruncated bool     `json:"recent_results_truncated"`
		}
		domains := make([]digestDomainEntry, 0, len(domainOrder))
		for _, domain := range domainOrder {
			entries, err := hnd.crossDomainRecentEntries(grouped[domain])
			if err != nil {
				return nil, err
			}
			lines := digestLines(entries, func(entry crossDomainRecentEntry) string {
				line := fmt.Sprintf("[%s] %s (%s)", entry.ID, entry.Label, entry.UpdatedAt)
				if entry.LifecycleState != "" {
					line += fmt.Sprintf(" (%s)", entry.LifecycleState)
				}
				return line
			})
			domains = append(domains, digestDomainEntry{
				Domain:                 domain,
				Recent:                 lines,
				RecentResultsTruncated: domainTruncated[domain],
			})
		}
		resp := struct {
			Mode             string              `json:"mode"`
			Domains          []digestDomainEntry `json:"domains"`
			ResultsTruncated bool                `json:"results_truncated"`
		}{
			Mode:             "cross_domain_snapshot",
			Domains:          domains,
			ResultsTruncated: resultsTruncated,
		}
		b, err := marshalResponseIndent(resp)
		if err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
	}

	type domainEntry struct {
		Domain                 string                   `json:"domain"`
		Recent                 []crossDomainRecentEntry `json:"recent"`
		RecentResultsTruncated bool                     `json:"recent_results_truncated"`
	}
	domains := make([]domainEntry, 0, len(domainOrder))
	for _, domain := range domainOrder {
		recent, err := hnd.crossDomainRecentEntries(grouped[domain])
		if err != nil {
			return nil, err
		}
		domains = append(domains, domainEntry{
			Domain:                 domain,
			Recent:                 recent,
			RecentResultsTruncated: domainTruncated[domain],
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
	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

func (hnd *Handler) orientWithTopic(domain, topic string, digest bool) (*ToolResult, error) {
	liveNodes, err := hnd.store.CountNodes(domain)
	if err != nil {
		return nil, err
	}
	archivedNodes, err := hnd.store.CountArchived(domain)
	if err != nil {
		return nil, err
	}
	staleCount, _ := hnd.store.CountStaleDrift(domain)

	result, err := hnd.store.SearchNodes(topic, domain, orientRelevantCap+1, "", nil)
	if err != nil {
		return nil, err
	}
	relevantTrunc := len(result.Nodes) > orientRelevantCap
	if relevantTrunc {
		result.Nodes = result.Nodes[:orientRelevantCap]
	}
	relevantNodes := make([]db.Node, len(result.Nodes))
	for i, nodeResult := range result.Nodes {
		relevantNodes[i] = nodeResult.Node
	}

	spineNodes, err := hnd.store.Timeline(domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return nil, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)

	recentRaw, err := hnd.store.RecentChanges(domain, orientRecentCap+1, nil)
	if err != nil {
		return nil, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)

	rulesNodes, rulesTrunc, err := hnd.store.GetStandingNodes(domain, orientRulesCap)
	if err != nil {
		return nil, err
	}

	var rulesField interface{}
	if len(rulesNodes) > 0 {
		rulesField, err = hnd.orientLeanSection(rulesNodes, digest)
		if err != nil {
			return nil, err
		}
	}
	spineField, err := hnd.orientLeanSection(spineNodes, digest)
	if err != nil {
		return nil, err
	}
	relevantField, err := hnd.orientLeanSection(relevantNodes, digest)
	if err != nil {
		return nil, err
	}
	recentField, err := hnd.orientLeanSection(recentRaw, digest)
	if err != nil {
		return nil, err
	}

	resp := struct {
		SummaryHint         string      `json:"summary_hint"`
		ServerVersion       string      `json:"server_version"`
		LiveNodes           int         `json:"live_nodes"`
		ArchivedNodes       int         `json:"archived_nodes"`
		StaleCount          int         `json:"stale_count"`
		LoadBearingLowTrust int         `json:"load_bearing_low_trust"`
		Rules               interface{} `json:"rules,omitempty"`
		DeclaredSpine       interface{} `json:"declared_spine"`
		Relevant            interface{} `json:"relevant"`
		Recent              interface{} `json:"recent"`
		orientSectionTruncation
	}{
		SummaryHint:         "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. relevant lists memories most similar to the supplied topic. declared_spine lists key decisions chronologically. rules lists the standing constraints and durable decisions that govern this domain. recent shows where work was last happening. Plain prose, no bullet points.",
		ServerVersion:       hnd.version,
		LiveNodes:           liveNodes,
		ArchivedNodes:       archivedNodes,
		StaleCount:          staleCount,
		LoadBearingLowTrust: 0,
		Rules:               rulesField,
		DeclaredSpine:       spineField,
		Relevant:            relevantField,
		Recent:              recentField,
		orientSectionTruncation: orientSectionTruncation{
			RelevantResultsTruncated:      relevantTrunc,
			RecentResultsTruncated:        recentTrunc,
			DeclaredSpineResultsTruncated: spineTrunc,
			RulesResultsTruncated:         rulesTrunc,
		},
	}

	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

// orientDomainEntry builds the full orient data for one domain. Used by the
// multi-domain (domains array) path.
type orientDomainEntry struct {
	Domain              string      `json:"domain"`
	Rules               interface{} `json:"rules,omitempty"`
	DeclaredSpine       interface{} `json:"declared_spine"`
	Significant         interface{} `json:"significant,omitempty"`
	Relevant            interface{} `json:"relevant,omitempty"`
	Recent              interface{} `json:"recent"`
	TotalNodes          int         `json:"total_nodes"`
	ArchivedNodes       int         `json:"archived_nodes"`
	StaleCount          int         `json:"stale_count"`
	LoadBearingLowTrust int         `json:"load_bearing_low_trust"`
	orientSectionTruncation
}

// buildDomainEntry builds lean orient data for a single domain (no top-level
// wrapper). topic and digest mirror the single-domain orient options. On an
// unknown/empty domain the sections are empty slices rather than errors.
func (hnd *Handler) buildDomainEntry(domain, topic string, digest bool) (orientDomainEntry, error) {
	liveNodes, err := hnd.store.CountNodes(domain)
	if err != nil {
		return orientDomainEntry{}, err
	}
	archivedNodes, err := hnd.store.CountArchived(domain)
	if err != nil {
		return orientDomainEntry{}, err
	}
	staleCount, _ := hnd.store.CountStaleDrift(domain)

	// Standing rules.
	rulesNodes, rulesTrunc, err := hnd.store.GetStandingNodes(domain, orientRulesCap)
	if err != nil {
		return orientDomainEntry{}, err
	}
	var rulesField interface{}
	if len(rulesNodes) > 0 {
		rulesField, err = hnd.orientLeanSection(rulesNodes, digest)
		if err != nil {
			return orientDomainEntry{}, err
		}
	}

	spineNodes, err := hnd.store.Timeline(domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return orientDomainEntry{}, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)

	recentRaw, err := hnd.store.RecentChanges(domain, orientRecentCap+1, nil)
	if err != nil {
		return orientDomainEntry{}, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)

	spineField, err := hnd.orientLeanSection(spineNodes, digest)
	if err != nil {
		return orientDomainEntry{}, err
	}
	recentField, err := hnd.orientLeanSection(recentRaw, digest)
	if err != nil {
		return orientDomainEntry{}, err
	}

	entry := orientDomainEntry{
		Domain:        domain,
		Rules:         rulesField,
		DeclaredSpine: spineField,
		Recent:        recentField,
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
		result, err := hnd.store.SearchNodes(topic, domain, orientRelevantCap+1, "", nil)
		if err != nil {
			return orientDomainEntry{}, err
		}
		relevantTrunc := len(result.Nodes) > orientRelevantCap
		if relevantTrunc {
			result.Nodes = result.Nodes[:orientRelevantCap]
		}
		relevantNodes := make([]db.Node, len(result.Nodes))
		for i, nodeResult := range result.Nodes {
			relevantNodes[i] = nodeResult.Node
		}
		entry.Relevant, err = hnd.orientLeanSection(relevantNodes, digest)
		if err != nil {
			return orientDomainEntry{}, err
		}
		entry.RelevantResultsTruncated = relevantTrunc
	} else {
		sigResult, err := hnd.store.GetSignificance(domain, orientSignificantCap, 90, nil, nil, 0)
		if err != nil {
			return orientDomainEntry{}, err
		}
		sigEntries := make([]scoredLeanEntry, len(sigResult.Structural))
		for i, scoredNode := range sigResult.Structural {
			sigEntries[i] = scoredLeanEntry{
				leanEntry:       toLeanEntry(scoredNode.Node),
				ImportanceScore: scoredNode.ImportanceScore,
			}
		}
		sigEntries, err = hnd.annotateSignificantTrust(sigEntries)
		if err != nil {
			return orientDomainEntry{}, err
		}
		if digest {
			sigEntries = hnd.annotateTrustDeltas(sigEntries, 3)
		}
		for _, sigEntry := range sigEntries {
			if sigEntry.Trust != "" {
				entry.LoadBearingLowTrust++
			}
		}
		entry.Significant, err = hnd.orientScoredSection(sigEntries, digest)
		if err != nil {
			return orientDomainEntry{}, err
		}
		entry.SignificantResultsTruncated = sigResult.StructuralResultsTruncated
	}

	return entry, nil
}

func (hnd *Handler) summariseDomain(args json.RawMessage) (*ToolResult, error) {
	if argsEmpty(args) {
		return hnd.orientCrossDomain(0, false)
	}
	var params struct {
		Domain  string   `json:"domain"`
		Domains []string `json:"domains"`
		Topic   string   `json:"topic"`
		Digest  bool     `json:"digest"`
		Limit   int      `json:"limit"`
	}
	if err := decodeParams(args, &params, "orient"); err != nil {
		return nil, err
	}

	// domains field present with empty array → validation error.
	if params.Domains != nil && len(params.Domains) == 0 {
		return nil, fmt.Errorf("domains must not be empty — provide 1–5 domain names")
	}

	// No domain and no domains → cross-domain bootstrap.
	if params.Domain == "" && len(params.Domains) == 0 {
		return hnd.orientCrossDomain(params.Limit, params.Digest)
	}

	// Mutual exclusion: domain + domains together is an error.
	if params.Domain != "" && len(params.Domains) > 0 {
		return nil, fmt.Errorf("domain and domains are mutually exclusive — provide one or the other, not both")
	}

	// domains array validation.
	if len(params.Domains) > 0 {
		if len(params.Domains) > 5 {
			return nil, fmt.Errorf("domains accepts at most 5 items (got %d) — maximum is 5", len(params.Domains))
		}
		// Length 1: behave identically to orient(domain="X").
		if len(params.Domains) == 1 {
			params.Domain = params.Domains[0]
			params.Domains = nil
			// Fall through to single-domain path below.
		} else {
			// Multi-domain path: build each entry in input order.
			return hnd.orientMultiDomain(params.Domains, params.Topic, params.Digest)
		}
	}

	// Single-domain path (domain is set, domains is empty).
	if params.Topic != "" {
		return hnd.orientWithTopic(params.Domain, params.Topic, params.Digest)
	}

	// Step 1: count live and archived nodes for the domain.
	liveNodes, err := hnd.store.CountNodes(params.Domain)
	if err != nil {
		return nil, err
	}
	if liveNodes == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "Nothing has been filed for this domain yet."}}}, nil
	}
	archivedNodes, err := hnd.store.CountArchived(params.Domain)
	if err != nil {
		return nil, err
	}
	staleCount, _ := hnd.store.CountStaleDrift(params.Domain)

	// Step 2: fetch significant nodes (structurally load-bearing, recency-weighted inbound degree).
	sigResult, err := hnd.store.GetSignificance(params.Domain, orientSignificantCap, 90, nil, nil, 0)
	if err != nil {
		return nil, err
	}

	// Step 3: fetch recent changes — capped at orientRecentCap.
	recentRaw, err := hnd.store.RecentChanges(params.Domain, orientRecentCap+1, nil)
	if err != nil {
		return nil, err
	}
	recentRaw, recentTrunc := cappedNodes(recentRaw, orientRecentCap)

	// Step 4: fetch declared decision spine (nodes with occurred_at set, chronological).
	spineNodes, err := hnd.store.Timeline(params.Domain, true, nil, nil, nil, nil, orientSpineCap+1)
	if err != nil {
		return nil, err
	}
	spineNodes, spineTrunc := cappedNodes(spineNodes, orientSpineCap)

	// Step 4b: fetch standing nodes (rules)
	rulesNodes, rulesTrunc, err := hnd.store.GetStandingNodes(params.Domain, orientRulesCap)
	if err != nil {
		return nil, err
	}

	sigEntries := make([]scoredLeanEntry, len(sigResult.Structural))
	for i, scoredNode := range sigResult.Structural {
		sigEntries[i] = scoredLeanEntry{
			leanEntry:       toLeanEntry(scoredNode.Node),
			ImportanceScore: scoredNode.ImportanceScore,
		}
	}
	sigEntries, err = hnd.annotateSignificantTrust(sigEntries)
	if err != nil {
		return nil, err
	}
	if params.Digest {
		sigEntries = hnd.annotateTrustDeltas(sigEntries, 3)
	}

	lowTrustCount := 0
	for _, sigEntry := range sigEntries {
		if sigEntry.Trust != "" {
			lowTrustCount++
		}
	}

	var rulesField interface{}
	if len(rulesNodes) > 0 {
		rulesField, err = hnd.orientLeanSection(rulesNodes, params.Digest)
		if err != nil {
			return nil, err
		}
	}
	spineField, err := hnd.orientLeanSection(spineNodes, params.Digest)
	if err != nil {
		return nil, err
	}
	significantField, err := hnd.orientScoredSection(sigEntries, params.Digest)
	if err != nil {
		return nil, err
	}
	recentField, err := hnd.orientLeanSection(recentRaw, params.Digest)
	if err != nil {
		return nil, err
	}

	resp := struct {
		SummaryHint         string      `json:"summary_hint"`
		ServerVersion       string      `json:"server_version"`
		LiveNodes           int         `json:"live_nodes"`
		ArchivedNodes       int         `json:"archived_nodes"`
		StaleCount          int         `json:"stale_count"`
		LoadBearingLowTrust int         `json:"load_bearing_low_trust"`
		Rules               interface{} `json:"rules,omitempty"`
		DeclaredSpine       interface{} `json:"declared_spine"`
		Significant         interface{} `json:"significant"`
		Recent              interface{} `json:"recent"`
		orientSectionTruncation
	}{
		SummaryHint:         "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. The declared_spine lists the key decisions that shaped this domain, in chronological order — weigh these heavily when summarising. rules lists the standing constraints and durable decisions that govern this domain. significant lists structurally load-bearing memories right now. recent shows where work was last happening. When load_bearing_low_trust > 0, inspect significant entries with a trust annotation and consider whether they warrant review. Plain prose, no bullet points.",
		ServerVersion:       hnd.version,
		LiveNodes:           liveNodes,
		ArchivedNodes:       archivedNodes,
		StaleCount:          staleCount,
		LoadBearingLowTrust: lowTrustCount,
		Rules:               rulesField,
		DeclaredSpine:       spineField,
		Significant:         significantField,
		Recent:              recentField,
		orientSectionTruncation: orientSectionTruncation{
			SignificantResultsTruncated:   sigResult.StructuralResultsTruncated,
			RecentResultsTruncated:        recentTrunc,
			DeclaredSpineResultsTruncated: spineTrunc,
			RulesResultsTruncated:         rulesTrunc,
		},
	}

	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

// orientMultiDomain handles orient(domains=[2..5 items], topic?, digest?).
func (hnd *Handler) orientMultiDomain(domains []string, topic string, digest bool) (*ToolResult, error) {
	entries := make([]orientDomainEntry, len(domains))
	for i, domain := range domains {
		entry, err := hnd.buildDomainEntry(domain, topic, digest)
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
		ServerVersion: hnd.version,
	}
	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}
