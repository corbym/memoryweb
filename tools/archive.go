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

func (hnd *Handler) forgetNode(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID      string `json:"id"`
		Reason  string `json:"reason"`
		Restore bool   `json:"restore"`
	}
	if err := decodeParams(args, &params, "forget"); err != nil {
		return nil, err
	}
	if params.ID == "" {
		return errorResult("id is required"), nil
	}
	if params.Restore {
		if err := hnd.store.RestoreNode(params.ID); err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("Node %q restored and is now visible in search and retrieval.", params.ID),
		}}}, nil
	}
	if strings.TrimSpace(params.Reason) == "" {
		return errorResult("reason is required when archiving"), nil
	}
	if err := hnd.store.ArchiveNode(params.ID, params.Reason); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q archived. It can be restored at any time with forget(restore=true).", params.ID),
	}}}, nil
}

const auditArchivedDefaultLimit = 25

func (hnd *Handler) listArchived(params auditArgs) (*ToolResult, error) {
	if params.Limit <= 0 {
		params.Limit = auditArchivedDefaultLimit
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)
	nodes, err := hnd.store.ListArchived(params.Domain, tags, nodeKinds, params.Limit)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		var nodesField interface{} = []db.Node{}
		if params.Digest {
			nodesField = []string{}
		}
		out := auditArchivedResult{Nodes: nodesField, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	resultsTruncated := len(nodes) > params.Limit
	if resultsTruncated {
		nodes = nodes[:params.Limit]
	}
	out := auditArchivedResult{
		ResultsTruncated: resultsTruncated,
	}
	var err2 error
	out.Nodes, err2 = hnd.digestNodeList(nodes, params.Digest)
	if err2 != nil {
		return nil, err2
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// forgetAll archives multiple nodes in a single atomic transaction.
// If any ID is not found, the transaction is rolled back and no nodes are archived.
func (hnd *Handler) forgetAll(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Items []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	if err := decodeParams(args, &params, "forget_all"); err != nil {
		return nil, err
	}
	if len(params.Items) == 0 {
		return errorResult("items is required and must not be empty"), nil
	}
	batch := make([]struct{ ID, Reason string }, len(params.Items))
	for i, item := range params.Items {
		if item.ID == "" {
			return errorResult(fmt.Sprintf("item %d is missing id", i)), nil
		}
		batch[i] = struct{ ID, Reason string }{ID: item.ID, Reason: item.Reason}
	}
	if err := hnd.store.ArchiveNodesBatch(batch); err != nil {
		return errorResult(err.Error()), nil
	}
	ids := make([]string, len(params.Items))
	for i, item := range params.Items {
		ids[i] = item.ID
	}
	msg := fmt.Sprintf("archived %d memories: %s\nAll nodes can be restored at any time with forget(restore=true).", len(ids), strings.Join(ids, ", "))
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

// restoreAll un-archives multiple nodes in a single atomic transaction.
func (hnd *Handler) restoreAll(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := decodeParams(args, &params, "restore_all"); err != nil {
		return nil, err
	}
	if len(params.Items) == 0 {
		return errorResult("items is required and must not be empty"), nil
	}
	ids := make([]string, len(params.Items))
	for i, item := range params.Items {
		if item.ID == "" {
			return errorResult(fmt.Sprintf("item %d is missing id", i)), nil
		}
		ids[i] = item.ID
	}
	if err := hnd.store.RestoreNodesBatch(ids); err != nil {
		return errorResult(err.Error()), nil
	}
	b, _ := json.Marshal(map[string]any{"restored": len(ids), "ids": ids})
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// auditTool dispatches mode=stale/orphans/archived/conflicts.
func (hnd *Handler) auditTool(args json.RawMessage) (*ToolResult, error) {
	var params auditArgs
	if err := decodeParams(args, &params, "audit"); err != nil {
		return nil, err
	}
	switch params.Mode {
	case "stale":
		return hnd.drift(params)
	case "orphans":
		return hnd.findDisconnected(params)
	case "archived":
		return hnd.listArchived(params)
	case "conflicts":
		return hnd.findConflictCandidates(params)
	case "kind_coverage":
		return hnd.findKindCoverage(params)
	default:
		return errorResult(fmt.Sprintf("unknown audit mode %q — use stale, orphans, archived, conflicts, or kind_coverage", params.Mode)), nil
	}
}

// ConflictCandidatesResult is the response shape for audit(mode=conflicts).
type ConflictCandidatesResult struct {
	Candidates       []db.ConflictCandidate `json:"candidates"`
	ResultsTruncated bool                   `json:"results_truncated"`
	Truncated        bool                   `json:"truncated,omitempty"` // deprecated alias
}

type auditStaleResult struct {
	Candidates            []db.DriftCandidate       `json:"candidates"`
	ResultsTruncated      bool                      `json:"results_truncated"`
	Placeholders          []db.PlaceholderCandidate `json:"placeholders"`
	PlaceholdersTruncated bool                      `json:"placeholders_truncated"`
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
	Lines                 []string `json:"lines"`
	ResultsTruncated      bool     `json:"results_truncated"`
	PlaceholderLines      []string `json:"placeholder_lines"`
	PlaceholdersTruncated bool     `json:"placeholders_truncated"`
}

// findConflictCandidates handles mode=conflicts: returns semantically adjacent
// node pairs that do not already have a contradicts edge. The server never
// asserts these conflict — only that they are close enough to warrant review.
func (hnd *Handler) findConflictCandidates(params auditArgs) (*ToolResult, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)

	// Fetch one extra to detect truncation.
	candidates, err := hnd.store.FindConflictCandidates(params.Domain, params.Limit+1, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		out := ConflictCandidatesResult{Candidates: []db.ConflictCandidate{}, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	truncated := len(candidates) > params.Limit
	if truncated {
		candidates = candidates[:params.Limit]
	}

	out := ConflictCandidatesResult{
		Candidates:       candidates,
		ResultsTruncated: truncated,
		Truncated:        truncated,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (hnd *Handler) drift(params auditArgs) (*ToolResult, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	if params.Depth <= 0 {
		params.Depth = 2
	}
	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)
	candidates, err := hnd.store.FindDrift(params.Domain, params.Limit+1, tags, nodeKinds, params.MemoryID, params.Depth)
	if err != nil {
		return nil, err
	}
	resultsTruncated := len(candidates) > params.Limit
	if resultsTruncated {
		candidates = candidates[:params.Limit]
	}
	if candidates == nil {
		candidates = []db.DriftCandidate{}
	}

	placeholders, err := hnd.store.FindPlaceholders(params.Domain, params.Limit+1, 30, 60, tags)
	if err != nil {
		return nil, err
	}
	plTruncated := len(placeholders) > params.Limit
	if plTruncated {
		placeholders = placeholders[:params.Limit]
	}
	if placeholders == nil {
		placeholders = []db.PlaceholderCandidate{}
	}

	if params.Digest {
		lines, err := hnd.digestLinesFromDrift(candidates)
		if err != nil {
			return nil, err
		}
		if lines == nil {
			lines = []string{}
		}
		out := auditStaleDigestResult{
			Lines:                 lines,
			ResultsTruncated:      resultsTruncated,
			PlaceholderLines:      digestLinesFromPlaceholders(placeholders),
			PlaceholdersTruncated: plTruncated,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	out := auditStaleResult{
		Candidates:            candidates,
		ResultsTruncated:      resultsTruncated,
		Placeholders:          placeholders,
		PlaceholdersTruncated: plTruncated,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (hnd *Handler) findDisconnected(params auditArgs) (*ToolResult, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)
	nodes, err := hnd.store.FindDisconnected(params.Domain, tags, nodeKinds, params.Limit)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		out := auditOrphansResult{Nodes: []db.Node{}, ResultsTruncated: false}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	resultsTruncated := len(nodes) > params.Limit
	if resultsTruncated {
		nodes = nodes[:params.Limit]
	}
	if params.Digest {
		lines, err := hnd.digestLinesFromNodes(nodes)
		if err != nil {
			return nil, err
		}
		out := auditStaleDigestResult{
			Lines:            lines,
			ResultsTruncated: resultsTruncated,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	out := auditOrphansResult{Nodes: nodes, ResultsTruncated: resultsTruncated}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

type auditKindCoverageResult struct {
	TotalNodes          int            `json:"total_nodes"`
	ByKind              map[string]int `json:"by_kind"`
	LegacyDominantPct   float64        `json:"legacy_dominant_pct"`
	MigrationCandidates []leanEntry    `json:"migration_candidates"`
	ResultsTruncated    bool           `json:"results_truncated"`
}

func (hnd *Handler) findKindCoverage(params auditArgs) (*ToolResult, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	tags := splitTags(params.Tags)
	nodeKinds := splitNodeKinds(params.NodeKind)
	result, err := hnd.store.FindKindCoverage(params.Domain, params.Limit, tags, nodeKinds)
	if err != nil {
		return nil, err
	}
	migrationCandidates, err := hnd.leanEntriesFromNodes(result.MigrationCandidates)
	if err != nil {
		return nil, err
	}
	out := auditKindCoverageResult{
		TotalNodes:          result.TotalNodes,
		ByKind:              result.ByKind,
		LegacyDominantPct:   result.LegacyDominantPct,
		MigrationCandidates: migrationCandidates,
		ResultsTruncated:    result.ResultsTruncated,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
