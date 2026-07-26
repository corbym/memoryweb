package tools

import "encoding/json"

func (h *Handler) searchNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Query    string `json:"query"`
		Domain   string `json:"domain"`
		Limit    int    `json:"limit"`
		Exact    bool   `json:"exact"`
		MemoryID string `json:"memory_id"`
		NodeKind string `json:"node_kind"`
		Digest   bool   `json:"digest"`
	}
	if err := decodeParams(args, &a, "search"); err != nil {
		return nil, err
	}
	nodeKinds := splitNodeKinds(a.NodeKind)
	if a.Query == "" && len(nodeKinds) == 0 {
		if err := requireNonEmpty(map[string]string{"query": a.Query}); err != nil {
			return nil, err
		}
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	if a.Exact {
		result, err := h.store.SearchNodesExact(a.Query, a.Domain, a.Limit, a.MemoryID, nodeKinds)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	result, err := h.store.SearchNodes(a.Query, a.Domain, a.Limit, a.MemoryID, nodeKinds)
	if err != nil {
		return nil, err
	}
	var b []byte
	var err2 error
	if a.Digest {
		digest, err := h.digestSearchResult(result)
		if err != nil {
			return nil, err
		}
		b, err2 = json.MarshalIndent(digest, "", "  ")
	} else {
		lean, err := h.leanSearchResult(result)
		if err != nil {
			return nil, err
		}
		b, err2 = json.MarshalIndent(lean, "", "  ")
	}
	if err2 != nil {
		return nil, err2
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
