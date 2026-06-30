package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) timeline(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var a struct {
		Domain        string `json:"domain"`
		MemoryID      string `json:"memory_id"`
		Depth         int    `json:"depth"`
		ImportantOnly bool   `json:"important_only"`
		Tags          string `json:"tags"`
		NodeKind      string `json:"node_kind"`
		From          string `json:"from"`
		To            string `json:"to"`
		Limit         int    `json:"limit"`
		Digest        bool   `json:"digest"`
	}
	if err := decodeParams(args, &a, "history"); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.Limit > 500 {
		a.Limit = 500
	}
	parseDate := func(s string) (*time.Time, error) {
		if s == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t, err = time.Parse("2006-01-02", s)
			if err != nil {
				return nil, fmt.Errorf("invalid date format, expected ISO8601: %s", s)
			}
		}
		return &t, nil
	}
	from, err := parseDate(a.From)
	if err != nil {
		return nil, err
	}
	to, err := parseDate(a.To)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, tag := range strings.Split(a.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	nodeKinds := splitNodeKinds(a.NodeKind)
	var nodes []db.Node
	if a.MemoryID != "" {
		if a.Depth <= 0 {
			a.Depth = 2
		}
		nodes, err = h.store.GetHistoryForMemoryID(a.MemoryID, a.Depth, a.ImportantOnly, tags, nodeKinds, from, to, a.Limit)
	} else {
		nodes, err = h.store.Timeline(a.Domain, a.ImportantOnly, tags, nodeKinds, from, to, a.Limit)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if a.Digest {
		out := struct {
			Lines []string `json:"lines"`
		}{Lines: digestLinesFromEntries(toLeanEntries(nodes))}
		b, _ := json.MarshalIndent(out, "", "  ")
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
	}
	b, _ := json.MarshalIndent(toLeanEntries(nodes), "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
