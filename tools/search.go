package tools

import "encoding/json"

func (h *Handler) searchNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Query    string `json:"query"`
		Domain   string `json:"domain"`
		Limit    int    `json:"limit"`
		Exact    bool   `json:"exact"`
		MemoryID string `json:"memory_id"`
		Digest   bool   `json:"digest"`
	}
	if err := decodeParams(args, &a, "search"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(map[string]string{"query": a.Query}); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	if a.Exact {
		result, err := h.store.SearchNodesExact(a.Query, a.Domain, a.Limit, a.MemoryID)
		if err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}

	result, err := h.store.SearchNodes(a.Query, a.Domain, a.Limit, a.MemoryID)
	if err != nil {
		return nil, err
	}
	var b []byte
	if a.Digest {
		b, _ = json.MarshalIndent(toDigestSearchResult(result), "", "  ")
	} else {
		b, _ = json.MarshalIndent(toLeanSearchResult(result), "", "  ")
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
