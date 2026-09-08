package tools

import (
	"encoding/json"
	"strings"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) handleSignificance(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Domain        string `json:"domain"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		Limit         int    `json:"limit"`
		DeclaredLimit int    `json:"declared_limit"`
		RecencyWindow int    `json:"recency_window"`
		Tags          string `json:"tags"`
		NodeKind      string `json:"node_kind"`
		Mode          string `json:"mode"`
		Digest        bool   `json:"digest"`
	}
	if err := decodeParams(args, &params, "significance"); err != nil {
		return nil, err
	}
	if params.Domain == "" && params.MemoryID == "" {
		return errorResult("domain or memory_id is required"), nil
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.DeclaredLimit <= 0 {
		params.DeclaredLimit = 100
	}
	if params.DeclaredLimit > 500 {
		params.DeclaredLimit = 500
	}
	if params.RecencyWindow <= 0 {
		params.RecencyWindow = 90
	}

	var tags []string
	for _, tag := range strings.Split(params.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	nodeKinds := splitNodeKinds(params.NodeKind)

	if params.Mode == "trust" {
		var res db.TrustResult
		var err error
		if params.MemoryID != "" {
			if params.Depth <= 0 {
				params.Depth = 2
			}
			res, err = hnd.store.GetTrustForMemoryID(params.MemoryID, params.Depth, params.RecencyWindow, nodeKinds)
		} else {
			res, err = hnd.store.GetTrust(params.Domain, params.Limit, params.RecencyWindow, tags, nodeKinds)
		}
		if err != nil {
			return errorResult(err.Error()), nil
		}
		var out []byte
		if params.Digest {
			out, err = json.Marshal(toDigestTrustResult(res))
		} else {
			out, err = json.Marshal(toLeanTrustResult(res))
		}
		if err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
	}

	var res db.SignificanceResult
	var err error
	if params.MemoryID != "" {
		if params.Depth <= 0 {
			params.Depth = 2
		}
		res, err = hnd.store.GetSignificanceForMemoryID(params.MemoryID, params.Depth, params.RecencyWindow, nodeKinds)
	} else {
		res, err = hnd.store.GetSignificance(params.Domain, params.Limit, params.RecencyWindow, tags, nodeKinds, params.DeclaredLimit)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}

	var out []byte
	var err2 error
	if params.Digest {
		digest, err := hnd.digestSignificanceResult(res)
		if err != nil {
			return nil, err
		}
		out, err2 = json.Marshal(digest)
	} else {
		lean, err := hnd.leanSignificanceResult(res)
		if err != nil {
			return nil, err
		}
		out, err2 = json.Marshal(lean)
	}
	if err2 != nil {
		return nil, err2
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}
