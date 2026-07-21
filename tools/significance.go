package tools

import (
	"encoding/json"
	"strings"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) handleSignificance(args json.RawMessage) (*ToolResult, error) {
	var a struct {
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
	if err := decodeParams(args, &a, "significance"); err != nil {
		return nil, err
	}
	if a.Domain == "" && a.MemoryID == "" {
		return errorResult("domain or memory_id is required"), nil
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.DeclaredLimit <= 0 {
		a.DeclaredLimit = 100
	}
	if a.DeclaredLimit > 500 {
		a.DeclaredLimit = 500
	}
	if a.RecencyWindow <= 0 {
		a.RecencyWindow = 90
	}

	var tags []string
	for _, tag := range strings.Split(a.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	nodeKinds := splitNodeKinds(a.NodeKind)

	if a.Mode == "trust" {
		var res db.TrustResult
		var err error
		if a.MemoryID != "" {
			if a.Depth <= 0 {
				a.Depth = 2
			}
			res, err = h.store.GetTrustForMemoryID(a.MemoryID, a.Depth, a.RecencyWindow, nodeKinds)
		} else {
			res, err = h.store.GetTrust(a.Domain, a.Limit, a.RecencyWindow, tags, nodeKinds)
		}
		if err != nil {
			return errorResult(err.Error()), nil
		}
		var out []byte
		if a.Digest {
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
	if a.MemoryID != "" {
		if a.Depth <= 0 {
			a.Depth = 2
		}
		res, err = h.store.GetSignificanceForMemoryID(a.MemoryID, a.Depth, a.RecencyWindow, nodeKinds)
	} else {
		res, err = h.store.GetSignificance(a.Domain, a.Limit, a.RecencyWindow, tags, nodeKinds, a.DeclaredLimit)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}

	var out []byte
	if a.Digest {
		out, err = json.Marshal(toDigestSignificanceResult(res))
	} else {
		out, err = json.Marshal(toLeanSignificanceResult(res))
	}
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}
