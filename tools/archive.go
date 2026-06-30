package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

type auditArgs struct {
	Mode     string `json:"mode"`
	Domain   string `json:"domain"`
	Limit    int    `json:"limit"`
	Tags     string `json:"tags"`
	NodeKind string `json:"node_kind"`
	MemoryID string `json:"memory_id"`
	Depth    int    `json:"depth"`
	Digest   bool   `json:"digest"`
}

func (h *Handler) forgetNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := decodeParams(args, &a, "forget"); err != nil {
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
	if err := decodeParams(args, &a, "restore"); err != nil {
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

func (h *Handler) listArchived(a auditArgs) (*ToolResult, error) {
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)
	nodes, err := h.store.ListArchived(a.Domain, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	out := digestNodeList(nodes, a.Digest)
	b, _ := json.MarshalIndent(out, "", "  ")
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
	if err := decodeParams(args, &a, "forget_all"); err != nil {
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
	var a auditArgs
	if err := decodeParams(args, &a, "audit"); err != nil {
		return nil, err
	}
	switch a.Mode {
	case "stale":
		return h.drift(a)
	case "orphans":
		return h.findDisconnected(a)
	case "archived":
		return h.listArchived(a)
	default:
		return errorResult(fmt.Sprintf("unknown audit mode %q — use stale, orphans, or archived", a.Mode)), nil
	}
}

func (h *Handler) drift(a auditArgs) (*ToolResult, error) {
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
	nodeKinds := splitNodeKinds(a.NodeKind)
	candidates, err := h.store.FindDrift(a.Domain, a.Limit, tags, nodeKinds, a.MemoryID, a.Depth)
	if err != nil {
		return nil, err
	}
	out := digestAuditResults(candidates, a.Digest)
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) findDisconnected(a auditArgs) (*ToolResult, error) {
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)
	nodes, err := h.store.FindDisconnected(a.Domain, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "No disconnected memories found."}}}, nil
	}
	out := digestNodeList(nodes, a.Digest)
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
