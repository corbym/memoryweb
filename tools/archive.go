package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
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

const auditArchivedDefaultLimit = 25

func (h *Handler) listArchived(a auditArgs) (*ToolResult, error) {
	if a.Limit <= 0 {
		a.Limit = auditArchivedDefaultLimit
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)
	nodes, err := h.store.ListArchived(a.Domain, tags, nodeKinds, a.Limit)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		var nodesField interface{} = []db.Node{}
		if a.Digest {
			nodesField = []string{}
		}
		out := auditArchivedResult{Nodes: nodesField, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	resultsTruncated := len(nodes) > a.Limit
	if resultsTruncated {
		nodes = nodes[:a.Limit]
	}
	out := auditArchivedResult{
		Nodes:            digestNodeList(nodes, a.Digest),
		ResultsTruncated: resultsTruncated,
	}
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

// auditTool dispatches mode=stale/orphans/archived/conflicts.
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
	case "conflicts":
		return h.findConflictCandidates(a)
	case "kind_coverage":
		return h.findKindCoverage(a)
	default:
		return errorResult(fmt.Sprintf("unknown audit mode %q — use stale, orphans, archived, conflicts, or kind_coverage", a.Mode)), nil
	}
}

// ConflictCandidatesResult is the response shape for audit(mode=conflicts).
type ConflictCandidatesResult struct {
	Candidates       []db.ConflictCandidate `json:"candidates"`
	ResultsTruncated bool                   `json:"results_truncated"`
	Truncated        bool                   `json:"truncated,omitempty"` // deprecated alias
}

type auditStaleResult struct {
	Candidates       []db.DriftCandidate `json:"candidates"`
	ResultsTruncated bool                `json:"results_truncated"`
}

type auditOrphansResult struct {
	Nodes            []db.Node `json:"nodes"`
	ResultsTruncated bool      `json:"results_truncated"`
}

type auditArchivedResult struct {
	Nodes            interface{} `json:"nodes"`
	ResultsTruncated bool        `json:"results_truncated"`
}

type auditStaleDigestResult struct {
	Lines            []string `json:"lines"`
	ResultsTruncated bool     `json:"results_truncated"`
}

// findConflictCandidates handles mode=conflicts: returns semantically adjacent
// node pairs that do not already have a contradicts edge. The server never
// asserts these conflict — only that they are close enough to warrant review.
func (h *Handler) findConflictCandidates(a auditArgs) (*ToolResult, error) {
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 100 {
		a.Limit = 100
	}
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)

	// Fetch one extra to detect truncation.
	candidates, err := h.store.FindConflictCandidates(a.Domain, a.Limit+1, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		out := ConflictCandidatesResult{Candidates: []db.ConflictCandidate{}, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	truncated := len(candidates) > a.Limit
	if truncated {
		candidates = candidates[:a.Limit]
	}

	out := ConflictCandidatesResult{
		Candidates:       candidates,
		ResultsTruncated: truncated,
		Truncated:        truncated,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
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
	candidates, err := h.store.FindDrift(a.Domain, a.Limit+1, tags, nodeKinds, a.MemoryID, a.Depth)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		if a.Digest {
			out := auditStaleDigestResult{Lines: []string{}, ResultsTruncated: false}
			b, _ := json.MarshalIndent(out, "", "  ")
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
		}
		out := auditStaleResult{Candidates: []db.DriftCandidate{}, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	resultsTruncated := len(candidates) > a.Limit
	if resultsTruncated {
		candidates = candidates[:a.Limit]
	}
	if a.Digest {
		out := auditStaleDigestResult{
			Lines:            digestLines(candidates, digestLineFromDrift),
			ResultsTruncated: resultsTruncated,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	out := auditStaleResult{Candidates: candidates, ResultsTruncated: resultsTruncated}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) findDisconnected(a auditArgs) (*ToolResult, error) {
	if a.Limit <= 0 {
		a.Limit = 50
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)
	nodes, err := h.store.FindDisconnected(a.Domain, tags, nodeKinds, a.Limit)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		out := auditOrphansResult{Nodes: []db.Node{}, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	resultsTruncated := len(nodes) > a.Limit
	if resultsTruncated {
		nodes = nodes[:a.Limit]
	}
	if a.Digest {
		out := auditStaleDigestResult{
			Lines:            digestLinesFromEntries(toLeanEntries(nodes)),
			ResultsTruncated: resultsTruncated,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	out := auditOrphansResult{Nodes: nodes, ResultsTruncated: resultsTruncated}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) findKindCoverage(a auditArgs) (*ToolResult, error) {
	if a.Limit <= 0 {
		a.Limit = 50
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	tags := splitTags(a.Tags)
	nodeKinds := splitNodeKinds(a.NodeKind)
	result, err := h.store.FindKindCoverage(a.Domain, a.Limit, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
