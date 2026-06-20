package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (h *Handler) forgetNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.ArchiveNode(a.ID, a.Reason); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q archived. It can be restored at any time with restore_node.", a.ID),
	}}}, nil
}

func (h *Handler) restoreNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.RestoreNode(a.ID); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q restored and is now visible in search and retrieval.", a.ID),
	}}}, nil
}

func (h *Handler) listArchived(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Tags   string `json:"tags"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	tags := splitTags(a.Tags)
	nodes, err := h.store.ListArchived(a.Domain, tags)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// forgetAll archives multiple nodes in a single atomic transaction.
// If any ID is not found, the transaction is rolled back and no nodes are archived.
func (h *Handler) forgetAll(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Items []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if len(a.Items) == 0 {
		return errorResult("items is required and must not be empty"), nil
	}
	batch := make([]struct{ ID, Reason string }, len(a.Items))
	for i, item := range a.Items {
		if item.ID == "" {
			return errorResult(fmt.Sprintf("item %d is missing id", i)), nil
		}
		batch[i] = struct{ ID, Reason string }{ID: item.ID, Reason: item.Reason}
	}
	if err := h.store.ArchiveNodesBatch(batch); err != nil {
		return errorResult(err.Error()), nil
	}
	ids := make([]string, len(a.Items))
	for i, item := range a.Items {
		ids[i] = item.ID
	}
	msg := fmt.Sprintf("archived %d memories: %s\nAll nodes can be restored at any time with restore.", len(ids), strings.Join(ids, ", "))
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

// auditTool dispatches mode=stale to drift, mode=orphans to findDisconnected,
// and mode=archived to listArchived.
func (h *Handler) auditTool(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	switch a.Mode {
	case "stale":
		return h.drift(args)
	case "orphans":
		return h.findDisconnected(args)
	case "archived":
		return h.listArchived(args)
	default:
		return errorResult(fmt.Sprintf("unknown audit mode %q — use stale, orphans, or archived", a.Mode)), nil
	}
}

func (h *Handler) drift(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain   string `json:"domain"`
		Limit    int    `json:"limit"`
		Tags     string `json:"tags"`
		MemoryID string `json:"memory_id"`
		Depth    int    `json:"depth"`
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
	if a.Depth <= 0 {
		a.Depth = 2
	}
	tags := splitTags(a.Tags)
	candidates, err := h.store.FindDrift(a.Domain, a.Limit, tags, a.MemoryID, a.Depth)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(candidates, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) findDisconnected(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Tags   string `json:"tags"`
		// MemoryID is parsed but intentionally ignored for orphans:
		// orphans have no connections by definition, so BFS from an anchor
		// would never reach them.
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	tags := splitTags(a.Tags)
	nodes, err := h.store.FindDisconnected(a.Domain, tags)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "No disconnected memories found."}}}, nil
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
