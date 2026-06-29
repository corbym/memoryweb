package tools

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) addNode(args json.RawMessage) (*ToolResult, error) {
	return dispatchBatch(args, "remember", h.addNodeSingle, h.addNodesBatch)
}

func (h *Handler) addNodeSingle(args json.RawMessage) (*ToolResult, error) {
	if msg := detectLegacyDecisionTypeKey(args); msg != "" {
		return errorResult(msg), nil
	}

	var a struct {
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
	if err := decodeParams(args, &a, "remember"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(map[string]string{
		"label":  a.Label,
		"domain": a.Domain,
	}); err != nil {
		return nil, err
	}
	var occurredAt *time.Time
	if a.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, a.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", a.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", a.OccurredAt)
			}
		}
		occurredAt = &t
	}
	if occurredAt != nil && a.WhyMatters == "" {
		return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
	}
	// backcompat: transient=true maps to node_kind=transient
	if a.Transient && a.NodeKind == "" {
		a.NodeKind = "transient"
	}
	if a.NodeKind == "" {
		a.NodeKind = "decision"
	}
	node, err := h.store.AddNode(a.Label, a.Description, a.WhyMatters, a.Domain, occurredAt, a.Tags, a.NodeKind)
	if err != nil {
		return nil, err
	}

	skipped := processRelatedTo(h, node.ID, a.RelatedTo)

	suggestions, err := h.store.SuggestEdges(node.ID, 5)
	if err != nil || suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}

	duplicates, err := h.store.FindPossibleDuplicates(node.Label, node.Domain, node.ID)
	if err != nil || duplicates == nil {
		duplicates = []db.Node{}
	}

	orphanWarning := ""
	if len(a.RelatedTo) == 0 || (len(a.RelatedTo) > 0 && len(skipped) == len(a.RelatedTo)) {
		orphanWarning = fmt.Sprintf("No connections were made. Call connect with domain=%s to link these memories. Some suggested connections are in other domains — pass their domain explicitly when calling connect, not the current domain.", node.Domain)
	}

	resp := struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		PossibleDuplicates   []db.Node           `json:"possible_duplicates"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
		OrphanWarning        string              `json:"orphan_warning,omitempty"`
	}{
		Node:                 node,
		SuggestedConnections: suggestions,
		PossibleDuplicates:   duplicates,
		SkippedConnections:   skipped,
		OrphanWarning:        orphanWarning,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

type skippedConnection struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// processRelatedTo attempts to create edges for each entry in the related_to list.
// Entries that fail (node not found, etc.) are collected in the returned slice instead of silently dropped.
func processRelatedTo(h *Handler, fromID string, entries []json.RawMessage) []skippedConnection {
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
		if _, err := h.store.AddEdge(fromID, relID, relationship, "auto-linked at creation"); err != nil {
			reason := err.Error()
			skipped = append(skipped, skippedConnection{ID: relID, Reason: reason})
		}
	}
	return skipped
}

// addNodesBatch handles the batch mode of remember: items is the raw JSON array of node objects.
func (h *Handler) addNodesBatch(items json.RawMessage) (*ToolResult, error) {
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
	for i, n := range nodeList {
		if err := requireNonEmpty(map[string]string{
			"label":  n.Label,
			"domain": n.Domain,
		}); err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
	}
	inputs := make([]db.NodeInput, len(nodeList))
	for i, n := range nodeList {
		var occurredAt *time.Time
		if n.OccurredAt != "" {
			t, err := time.Parse(time.RFC3339, n.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", n.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("node %d: invalid occurred_at: %s", i, n.OccurredAt)
				}
			}
			occurredAt = &t
		}
		if occurredAt != nil && n.WhyMatters == "" {
			return nil, fmt.Errorf("node %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
		}
		// backcompat: transient=true maps to node_kind=transient
		nodeKind := n.NodeKind
		if n.Transient && nodeKind == "" {
			nodeKind = "transient"
		}
		if nodeKind == "" {
			nodeKind = "decision"
		}
		inputs[i] = db.NodeInput{
			Label:       n.Label,
			Description: n.Description,
			WhyMatters:  n.WhyMatters,
			Tags:        n.Tags,
			Domain:      n.Domain,
			OccurredAt:  occurredAt,
			NodeKind:    nodeKind,
		}
	}
	nodes, err := h.store.AddNodesBatch(inputs)
	if err != nil {
		return nil, err
	}

	type entry struct {
		Node                 *db.Node            `json:"node"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		SkippedConnections   []skippedConnection `json:"skipped_connections,omitempty"`
	}
	result := make([]entry, len(nodes))
	anyConnected := false
	for i, n := range nodes {
		suggestions, _ := h.store.SuggestEdges(n.ID, 5)
		if suggestions == nil {
			suggestions = []db.EdgeSuggestion{}
		}
		skipped := processRelatedTo(h, n.ID, nodeList[i].RelatedTo)
		if len(nodeList[i].RelatedTo) > 0 && len(skipped) < len(nodeList[i].RelatedTo) {
			anyConnected = true
		}
		result[i] = entry{Node: n, SuggestedConnections: suggestions, SkippedConnections: skipped}
	}
	orphanWarning := ""
	if len(nodes) > 0 && !anyConnected {
		orphanWarning = "No connections were made. Call connect with domain=<domain> to link these memories. Some suggested connections are in other domains — pass their domain explicitly when calling connect, not the current domain."
	}

	type response struct {
		Nodes         []entry `json:"nodes"`
		OrphanWarning string  `json:"orphan_warning,omitempty"`
	}
	out := response{Nodes: result, OrphanWarning: orphanWarning}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// addNodes retains the old remember_all wire format for backward compat during transition (not exposed in ListTools).
func (h *Handler) addNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
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
	if err := decodeParams(args, &a, "remember"); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a.Nodes)
	if err != nil {
		return nil, err
	}
	return h.addNodesBatch(raw)
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
