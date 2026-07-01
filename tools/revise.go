package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/corbym/memoryweb/db"
)

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
		if !callHasWhyMatters {
			existing, err := h.store.GetNode(a.ID)
			if err != nil {
				return nil, err
			}
			if existing.Node.WhyMatters == "" {
				return nil, fmt.Errorf("occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.")
			}
		}
	}
	if a.Domain != nil && (a.Reason == nil || strings.TrimSpace(*a.Reason) == "") {
		return errorResult("reason is required when changing domain — confirm the target domain with the user before moving"), nil
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
	b, _ := json.MarshalIndent(node, "", "  ")
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
	for i, u := range updateList {
		if u.ID == "" {
			return nil, fmt.Errorf("update %d: id is required", i)
		}
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
			if !callHasWhyMatters {
				existing, err := h.store.GetNode(u.ID)
				if err != nil {
					return nil, fmt.Errorf("update %d: %w", i, err)
				}
				if existing.Node.WhyMatters == "" {
					return nil, fmt.Errorf("update %d: occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline.", i)
				}
			}
		}
		if u.Domain != nil && (u.Reason == nil || strings.TrimSpace(*u.Reason) == "") {
			return errorResult(fmt.Sprintf("update %d: reason is required when changing domain", i)), nil
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
	resp := struct {
		Updated []*db.Node `json:"updated"`
	}{Updated: nodes}
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
