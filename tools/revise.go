package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/corbym/memoryweb/db"
)

// reviseConnection is an edge in the revise response envelope.
type reviseConnection struct {
	Direction    string `json:"direction"`
	Relationship string `json:"relationship"`
	PeerID       string `json:"peer_id"`
	PeerLabel    string `json:"peer_label,omitempty"`
}

// buildReviseConnections builds the connections slice from a node's edges,
// annotating each peer with its label from the provided map.
func buildReviseConnections(nwe *db.NodeWithEdges, labels map[string]string) []reviseConnection {
	conns := make([]reviseConnection, 0, len(nwe.Edges))
	for _, e := range nwe.Edges {
		var dir, peerID string
		if e.FromNode == nwe.Node.ID {
			dir, peerID = "outbound", e.ToNode
		} else {
			dir, peerID = "inbound", e.FromNode
		}
		conns = append(conns, reviseConnection{
			Direction:    dir,
			Relationship: e.Relationship,
			PeerID:       peerID,
			PeerLabel:    labels[peerID],
		})
	}
	return conns
}

// peerIDsFromEdges collects the IDs of all peer nodes in an edge list.
func peerIDsFromEdges(edges []db.Edge, nodeID string) []string {
	ids := make([]string, 0, len(edges))
	for _, e := range edges {
		if e.FromNode == nodeID {
			ids = append(ids, e.ToNode)
		} else {
			ids = append(ids, e.FromNode)
		}
	}
	return ids
}

// detectLegacyNodeUpdateKeys inspects raw JSON for the retired revise_all
// "updates" wrapper key. Returns a non-empty error message if found.
func detectLegacyNodeUpdateKeys(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if _, hasUpdates := m["updates"]; hasUpdates {
		return "Unknown parameter 'updates'. Pass fields (label, description, why_matters, tags, …) directly alongside id, or use items for batch mode. Call tools/list to refresh your schema."
	}
	return ""
}

func (h *Handler) updateNode(args json.RawMessage) (*ToolResult, error) {
	return dispatchBatch(args, "revise", h.updateNodeSingle, h.updateNodesBatch)
}

func (h *Handler) updateNodeSingle(args json.RawMessage) (*ToolResult, error) {
	// Detect the retired revise_all/updates wrapper format.
	if msg := detectLegacyNodeUpdateKeys(args); msg != "" {
		return errorResult(msg), nil
	}
	if msg := detectLegacyDecisionTypeKey(args); msg != "" {
		return errorResult(msg), nil
	}

	var a struct {
		ID          string  `json:"id"`
		Label       *string `json:"label"`
		Description *string `json:"description"`
		WhyMatters  *string `json:"why_matters"`
		Tags        *string `json:"tags"`
		OccurredAt  *string `json:"occurred_at"`
		Transient   *bool   `json:"transient"`
		NodeKind    *string `json:"node_kind"`
		Domain      *string `json:"domain"`
		Reason      *string `json:"reason"`
	}
	if err := decodeParams(args, &a, "revise"); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	existingNwe, err := h.store.GetNode(a.ID)
	if err != nil {
		return nil, err
	}
	contentTouched := reviseContentTouched(existingNwe.Node, a.Label, a.Description, a.WhyMatters, a.NodeKind)
	var occurredAt *time.Time
	if a.OccurredAt != nil {
		t, err := time.Parse(time.RFC3339, *a.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", *a.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", *a.OccurredAt)
			}
		}
		occurredAt = &t
	}
	if occurredAt != nil {
		callHasWhyMatters := a.WhyMatters != nil && *a.WhyMatters != ""
		if !callHasWhyMatters && existingNwe.Node.WhyMatters == "" {
			return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
		}
	}
	if a.Domain != nil {
		resolved := h.store.ResolveAlias(*a.Domain)
		if resolved == existingNwe.Node.Domain {
			a.Domain = nil
		} else if a.Reason == nil || strings.TrimSpace(*a.Reason) == "" {
			return errorResult("reason is required when changing domain — confirm the target domain with the user before moving"), nil
		}
	}
	// backcompat: transient=true maps to node_kind=transient
	if a.Transient != nil && a.NodeKind == nil {
		if *a.Transient {
			s := "transient"
			a.NodeKind = &s
		} else {
			s := "decision"
			a.NodeKind = &s
		}
	}
	node, err := h.store.UpdateNode(a.ID, a.Label, a.Description, a.WhyMatters, a.Tags, occurredAt, a.NodeKind, a.Domain, a.Reason)
	if err != nil {
		return nil, err
	}

	nwe, err := h.store.GetNode(a.ID)
	if err != nil {
		return nil, err
	}

	var trustNudge string
	if contentTouched {
		nudge, nudgeErr := h.trustNudgeForDependencies(outboundDependencyIDs(nwe.Edges, a.ID), a.ID)
		if nudgeErr != nil {
			log.Printf("[memoryweb] trust nudge for %s: %v", a.ID, nudgeErr)
		} else {
			trustNudge = nudge
		}
	}

	peerIDs := peerIDsFromEdges(nwe.Edges, a.ID)
	labels := h.store.GetNodeLabels(peerIDs)
	connections := buildReviseConnections(nwe, labels)

	suggestions, err := h.store.SuggestEdges(a.ID, 5)
	if err != nil || suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}

	duplicates, err := h.store.FindPossibleDuplicates(node.Label, node.Domain, node.ID)
	if err != nil || duplicates == nil {
		duplicates = []db.Node{}
	}

	resp := struct {
		Node                 *db.Node            `json:"node"`
		Connections          []reviseConnection  `json:"connections"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		PossibleDuplicates   []db.Node           `json:"possible_duplicates,omitempty"`
		TrustNudge           string              `json:"trust_nudge,omitempty"`
	}{
		Node:                 node,
		Connections:          connections,
		SuggestedConnections: suggestions,
		PossibleDuplicates:   duplicates,
		TrustNudge:           trustNudge,
	}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// updateNodesBatch handles the batch mode of revise: items is the raw JSON array of update objects.
func (h *Handler) updateNodesBatch(items json.RawMessage) (*ToolResult, error) {
	type updateItem struct {
		ID          string  `json:"id"`
		Label       *string `json:"label"`
		Description *string `json:"description"`
		WhyMatters  *string `json:"why_matters"`
		Tags        *string `json:"tags"`
		OccurredAt  *string `json:"occurred_at"`
		Transient   *bool   `json:"transient"`
		NodeKind    *string `json:"node_kind"`
		Domain      *string `json:"domain"`
		Reason      *string `json:"reason"`
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
	updateList, err := decodeBatchItems[updateItem](items, "revise")
	if err != nil {
		return nil, err
	}
	inputs := make([]db.NodeUpdateInput, len(updateList))
	contentTouched := make([]bool, len(updateList))
	for i, u := range updateList {
		if u.ID == "" {
			return nil, fmt.Errorf("update %d: id is required", i)
		}
		existingNwe, err := h.store.GetNode(u.ID)
		if err != nil {
			return nil, fmt.Errorf("update %d: %w", i, err)
		}
		contentTouched[i] = reviseContentTouched(existingNwe.Node, u.Label, u.Description, u.WhyMatters, u.NodeKind)
		var occurredAt *time.Time
		if u.OccurredAt != nil {
			t, err := time.Parse(time.RFC3339, *u.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", *u.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("update %d: invalid occurred_at format: %s", i, *u.OccurredAt)
				}
			}
			occurredAt = &t
		}
		if occurredAt != nil {
			callHasWhyMatters := u.WhyMatters != nil && *u.WhyMatters != ""
			if !callHasWhyMatters && existingNwe.Node.WhyMatters == "" {
				return nil, fmt.Errorf("update %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
			}
		}
		if u.Domain != nil {
			resolved := h.store.ResolveAlias(*u.Domain)
			if resolved == existingNwe.Node.Domain {
				u.Domain = nil
			} else if u.Reason == nil || strings.TrimSpace(*u.Reason) == "" {
				return errorResult(fmt.Sprintf("update %d: reason is required when changing domain", i)), nil
			}
		}
		// backcompat: transient bool maps to node_kind
		nodeKind := u.NodeKind
		if u.Transient != nil && nodeKind == nil {
			if *u.Transient {
				s := "transient"
				nodeKind = &s
			} else {
				s := "decision"
				nodeKind = &s
			}
		}
		inputs[i] = db.NodeUpdateInput{
			ID:          u.ID,
			Label:       u.Label,
			Description: u.Description,
			WhyMatters:  u.WhyMatters,
			Tags:        u.Tags,
			OccurredAt:  occurredAt,
			NodeKind:    nodeKind,
			Domain:      u.Domain,
			Reason:      u.Reason,
		}
	}
	nodes, err := h.store.UpdateNodesBatch(inputs)
	if err != nil {
		return nil, err
	}

	type updatedEntry struct {
		Node                 *db.Node            `json:"node"`
		Connections          []reviseConnection  `json:"connections"`
		SuggestedConnections []db.EdgeSuggestion `json:"suggested_connections"`
		TrustNudge           string              `json:"trust_nudge,omitempty"`
	}
	updated := make([]updatedEntry, len(nodes))
	for i, node := range nodes {
		nwe, err := h.store.GetNode(node.ID)
		if err != nil {
			return nil, err
		}

		var trustNudge string
		if contentTouched[i] {
			nudge, nudgeErr := h.trustNudgeForDependencies(outboundDependencyIDs(nwe.Edges, node.ID), node.ID)
			if nudgeErr != nil {
				log.Printf("[memoryweb] trust nudge for %s: %v", node.ID, nudgeErr)
			} else {
				trustNudge = nudge
			}
		}

		peerIDs := peerIDsFromEdges(nwe.Edges, node.ID)
		labels := h.store.GetNodeLabels(peerIDs)
		connections := buildReviseConnections(nwe, labels)

		suggestions, _ := h.store.SuggestEdges(node.ID, 5)
		if suggestions == nil {
			suggestions = []db.EdgeSuggestion{}
		}

		updated[i] = updatedEntry{
			Node:                 node,
			Connections:          connections,
			SuggestedConnections: suggestions,
			TrustNudge:           trustNudge,
		}
	}
	resp := struct {
		Updated []updatedEntry `json:"updated"`
	}{Updated: updated}
	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// updateNodes retains the old revise_all wire format for backward compat during transition (not exposed in ListTools).
func (h *Handler) updateNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Updates []struct {
			ID          string  `json:"id"`
			Label       *string `json:"label"`
			Description *string `json:"description"`
			WhyMatters  *string `json:"why_matters"`
			Tags        *string `json:"tags"`
			OccurredAt  *string `json:"occurred_at"`
		} `json:"updates"`
	}
	if err := decodeParams(args, &a, "revise"); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a.Updates)
	if err != nil {
		return nil, err
	}
	return h.updateNodesBatch(raw)
}
