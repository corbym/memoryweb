package tools

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) addNode(args json.RawMessage) (*ToolResult, error) {
	return dispatchBatch(args, "remember", hnd.addNodeSingle, hnd.addNodesBatch)
}

func (hnd *Handler) addNodeSingle(args json.RawMessage) (*ToolResult, error) {
	if msg := detectLegacyDecisionTypeKey(args); msg != "" {
		return errorResult(msg), nil
	}

	var params struct {
		Label       string            `json:"label"`
		Description string            `json:"description"`
		WhyMatters  string            `json:"why_matters"`
		Domain      string            `json:"domain"`
		OccurredAt  string            `json:"occurred_at"`
		Tags        string            `json:"tags"`
		RelatedTo   []json.RawMessage `json:"related_to"`
		Transient   bool              `json:"transient"`
		NodeKind    string            `json:"node_kind"`
	}
	if err := decodeParams(args, &params, "remember"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(map[string]string{
		"label":  params.Label,
		"domain": params.Domain,
	}); err != nil {
		return nil, err
	}
	var occurredAt *time.Time
	if params.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, params.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", params.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", params.OccurredAt)
			}
		}
		occurredAt = &t
	}
	if occurredAt != nil && params.WhyMatters == "" {
		return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
	}
	// backcompat: transient=true maps to node_kind=transient
	if params.Transient && params.NodeKind == "" {
		params.NodeKind = "transient"
	}
	if params.NodeKind == "" {
		params.NodeKind = "decision"
	}
	domainExists, err := hnd.store.DomainExists(params.Domain)
	if err != nil {
		return nil, err
	}
	node, err := hnd.store.AddNode(params.Label, params.Description, params.WhyMatters, params.Domain, occurredAt, params.Tags, params.NodeKind)
	if err != nil {
		return nil, err
	}

	extras := hnd.rememberFilingExtras(node, params.RelatedTo, domainExists)

	skipped := processRelatedTo(hnd, node.ID, params.RelatedTo)

	suggestions, err := hnd.store.SuggestEdges(node.ID, 5)
	if err != nil || suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}

	duplicates, err := hnd.store.FindPossibleDuplicates(node.Label, node.Domain, node.ID)
	if err != nil || duplicates == nil {
		duplicates = []db.Node{}
	}

	var misdomain *misdomainFlag
	if extras.PossibleMisdomain {
		misdomain = &misdomainFlag{
			SuggestedDomain:   extras.SuggestedDomain,
			SuggestedMemoryID: extras.SuggestedMemoryID,
		}
	}

	orphanWarning := ""
	if len(params.RelatedTo) == 0 || (len(params.RelatedTo) > 0 && len(skipped) == len(params.RelatedTo)) {
		orphanWarning = "No connections were made. Call connect with from_memory/to_memory to link these memories — connect takes no domain parameter, memory IDs are global. Suggested connections may be in other domains; that's context only, not a connect argument."
	}

	resp := struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		PossibleDuplicates   []db.Node           `json:"possible_duplicates"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
		OrphanWarning        string              `json:"orphan_warning,omitempty"`
		TrustNudge           string              `json:"trust_nudge,omitempty"`
		PossibleMisdomain    bool                `json:"possible_misdomain,omitempty"`
		SuggestedDomain      string              `json:"suggested_domain,omitempty"`
		SuggestedMemoryID    string              `json:"suggested_memory_id,omitempty"`
	}{
		Node:                 node,
		SuggestedConnections: suggestions,
		PossibleDuplicates:   duplicates,
		SkippedConnections:   skipped,
		OrphanWarning:        orphanWarning,
		TrustNudge:           extras.TrustNudge,
	}
	if misdomain != nil {
		resp.PossibleMisdomain = true
		resp.SuggestedDomain = misdomain.SuggestedDomain
		resp.SuggestedMemoryID = misdomain.SuggestedMemoryID
	}
	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

type skippedConnection struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// processRelatedTo attempts to create edges for each entry in the related_to list.
// Entries that fail (node not found, etc.) are collected in the returned slice instead of silently dropped.
func processRelatedTo(hnd *Handler, fromID string, entries []json.RawMessage) []skippedConnection {
	var skipped []skippedConnection
	for _, raw := range entries {
		relID := ""
		relationship := "connects_to"

		var strID string
		if err := json.Unmarshal(raw, &strID); err == nil {
			relID = strID
		} else {
			var entry struct {
				ID           string `json:"id"`
				Relationship string `json:"relationship"`
			}
			if err := json.Unmarshal(raw, &entry); err == nil {
				relID = entry.ID
				if entry.Relationship != "" {
					relationship = entry.Relationship
				}
			}
		}

		if relID == "" {
			continue
		}
		if _, err := hnd.store.AddEdge(fromID, relID, relationship, "auto-linked at creation"); err != nil {
			reason := err.Error()
			skipped = append(skipped, skippedConnection{ID: relID, Reason: reason})
		}
	}
	return skipped
}

// addNodesBatch handles the batch mode of remember: items is the raw JSON array of node objects.
func (hnd *Handler) addNodesBatch(items json.RawMessage) (*ToolResult, error) {
	type nodeItem struct {
		Label       string            `json:"label"`
		Description string            `json:"description"`
		WhyMatters  string            `json:"why_matters"`
		Tags        string            `json:"tags"`
		Domain      string            `json:"domain"`
		OccurredAt  string            `json:"occurred_at"`
		Transient   bool              `json:"transient"`
		NodeKind    string            `json:"node_kind"`
		RelatedTo   []json.RawMessage `json:"related_to"`
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(items, &rawItems); err != nil {
		return nil, err
	}
	for i, raw := range rawItems {
		if msg := detectLegacyDecisionTypeKey(raw); msg != "" {
			return errorResult(fmt.Sprintf("item %d: %s", i, msg)), nil
		}
	}
	nodeList, err := decodeBatchItems[nodeItem](items, "remember")
	if err != nil {
		return nil, err
	}
	for i, nodeItem := range nodeList {
		if err := requireNonEmpty(map[string]string{
			"label":  nodeItem.Label,
			"domain": nodeItem.Domain,
		}); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}
	inputs := make([]db.NodeInput, len(nodeList))
	for i, nodeItem := range nodeList {
		var occurredAt *time.Time
		if nodeItem.OccurredAt != "" {
			t, err := time.Parse(time.RFC3339, nodeItem.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", nodeItem.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("node %d: invalid occurred_at: %s", i, nodeItem.OccurredAt)
				}
			}
			occurredAt = &t
		}
		if occurredAt != nil && nodeItem.WhyMatters == "" {
			return nil, fmt.Errorf("node %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
		}
		// backcompat: transient=true maps to node_kind=transient
		nodeKind := nodeItem.NodeKind
		if nodeItem.Transient && nodeKind == "" {
			nodeKind = "transient"
		}
		if nodeKind == "" {
			nodeKind = "decision"
		}
		inputs[i] = db.NodeInput{
			Label:       nodeItem.Label,
			Description: nodeItem.Description,
			WhyMatters:  nodeItem.WhyMatters,
			Tags:        nodeItem.Tags,
			Domain:      nodeItem.Domain,
			OccurredAt:  occurredAt,
			NodeKind:    nodeKind,
		}
	}
	nodes, err := hnd.store.AddNodesBatch(inputs)
	if err != nil {
		return nil, err
	}

	domains := make([]string, len(nodeList))
	for i, nodeItem := range nodeList {
		domains[i] = nodeItem.Domain
	}
	domainSnap, err := hnd.snapshotDomainExistence(domains)
	if err != nil {
		return nil, err
	}

	type entry struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
		TrustNudge           string              `json:"trust_nudge,omitempty"`
		PossibleMisdomain    bool                `json:"possible_misdomain,omitempty"`
		SuggestedDomain      string              `json:"suggested_domain,omitempty"`
		SuggestedMemoryID    string              `json:"suggested_memory_id,omitempty"`
	}
	result := make([]entry, len(nodes))
	anyConnected := false
	for i, node := range nodes {
		suggestions, _ := hnd.store.SuggestEdges(node.ID, 5)
		if suggestions == nil {
			suggestions = []db.EdgeSuggestion{}
		}
		extras := hnd.rememberFilingExtras(node, nodeList[i].RelatedTo, domainSnap[hnd.store.ResolveAlias(nodeList[i].Domain)])
		skipped := processRelatedTo(hnd, node.ID, nodeList[i].RelatedTo)
		if len(nodeList[i].RelatedTo) > 0 && len(skipped) < len(nodeList[i].RelatedTo) {
			anyConnected = true
		}
		result[i] = entry{
			Node:                 node,
			SuggestedConnections: suggestions,
			SkippedConnections:   skipped,
			TrustNudge:           extras.TrustNudge,
			PossibleMisdomain:    extras.PossibleMisdomain,
			SuggestedDomain:      extras.SuggestedDomain,
			SuggestedMemoryID:    extras.SuggestedMemoryID,
		}
	}
	orphanWarning := ""
	if len(nodes) > 0 && !anyConnected {
		orphanWarning = "No connections were made. Call connect with from_memory/to_memory to link these memories — connect takes no domain parameter, memory IDs are global. Suggested connections may be in other domains; that's context only, not a connect argument."
	}

	type response struct {
		Nodes         []entry `json:"nodes"`
		OrphanWarning string  `json:"orphan_warning,omitempty"`
	}
	out := response{Nodes: result, OrphanWarning: orphanWarning}
	b, err := marshalResponseIndent(out)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

// addNodes retains the old remember_all wire format for backward compat during transition (not exposed in ListTools).
func (hnd *Handler) addNodes(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Nodes []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
			WhyMatters  string `json:"why_matters"`
			Tags        string `json:"tags"`
			Domain      string `json:"domain"`
			OccurredAt  string `json:"occurred_at"`
			Transient   bool   `json:"transient"`
		} `json:"nodes"`
	}
	if err := decodeParams(args, &params, "remember"); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(params.Nodes)
	if err != nil {
		return nil, err
	}
	return hnd.addNodesBatch(raw)
}

// detectLegacyDecisionTypeKey inspects raw JSON for the retired 'decision_type'
// parameter name (renamed to node_kind). Returns a non-empty error message if found.
func detectLegacyDecisionTypeKey(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if _, ok := m["decision_type"]; ok {
		return "decision_type has been renamed to node_kind — use node_kind instead. Call tools/list to refresh your schema."
	}
	return ""
}

type misdomainFlag struct {
	SuggestedDomain   string
	SuggestedMemoryID string
}

func (hnd *Handler) checkNewDomainMisdomain(node *db.Node) (*misdomainFlag, error) {
	candidate, err := hnd.store.FindMisdomainCandidate(node.ID, node.Domain)
	if err != nil || candidate == nil {
		return nil, err
	}
	reason := fmt.Sprintf("suggested domain %q (anchor %s)", candidate.SuggestedDomain, candidate.SuggestedMemoryID)
	if err := hnd.store.LogDomainCreationFlagged(node.ID, node.Label, reason); err != nil {
		return nil, err
	}
	return &misdomainFlag{
		SuggestedDomain:   candidate.SuggestedDomain,
		SuggestedMemoryID: candidate.SuggestedMemoryID,
	}, nil
}
