package tools

import "encoding/json"

func (hnd *Handler) searchNodes(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Query    string `json:"query"`
		Domain   string `json:"domain"`
		Limit    int    `json:"limit"`
		Exact    bool   `json:"exact"`
		MemoryID string `json:"memory_id"`
		NodeKind string `json:"node_kind"`
		Digest   bool   `json:"digest"`
	}
	if err := decodeParams(args, &params, "search"); err != nil {
		return nil, err
	}
	nodeKinds := splitNodeKinds(params.NodeKind)
	if params.Query == "" && len(nodeKinds) == 0 {
		if err := requireNonEmpty(map[string]string{"query": params.Query}); err != nil {
			return nil, err
		}
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	if params.Exact {
		result, err := hnd.store.SearchNodesExact(params.Query, params.Domain, params.Limit, params.MemoryID, nodeKinds)
		if err != nil {
			return nil, err
		}
		b, err := marshalResponseIndent(result)
		if err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
	}

	result, err := hnd.store.SearchNodes(params.Query, params.Domain, params.Limit, params.MemoryID, nodeKinds)
	if err != nil {
		return nil, err
	}
	var b []byte
	var err2 error
	if params.Digest {
		digest, err := hnd.digestSearchResult(result)
		if err != nil {
			return nil, err
		}
		b, err2 = json.MarshalIndent(digest, "", "  ")
	} else {
		lean, err := hnd.leanSearchResult(result)
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
