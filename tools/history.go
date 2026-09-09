package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) timeline(args json.RawMessage) (*ToolResult, error) {
	args = argsOrEmptyObject(args)
	var params struct {
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
		Order         string `json:"order"`
		GroupByDomain bool   `json:"group_by_domain"`
	}
	if err := decodeParams(args, &params, "history"); err != nil {
		return nil, err
	}
	if params.Order == "modified" {
		if params.ImportantOnly || params.From != "" || params.To != "" {
			return errorResult("order=modified cannot be combined with important_only, from, or to"), nil
		}
		recentArgs, err := json.Marshal(map[string]any{
			"domain":          params.Domain,
			"limit":           params.Limit,
			"group_by_domain": params.GroupByDomain,
			"tags":            params.Tags,
			"node_kind":       params.NodeKind,
			"memory_id":       params.MemoryID,
			"depth":           params.Depth,
			"digest":          params.Digest,
		})
		if err != nil {
			return nil, err
		}
		return hnd.recentChanges(recentArgs)
	}
	if params.Order != "" && params.Order != "effective" {
		return errorResult(fmt.Sprintf("unknown order %q — use effective (default) or modified", params.Order)), nil
	}
	if params.GroupByDomain {
		return errorResult("group_by_domain requires order=modified"), nil
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	parseDate := func(raw string) (*time.Time, error) {
		if raw == "" {
			return nil, nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t, err = time.Parse("2006-01-02", raw)
			if err != nil {
				return nil, fmt.Errorf("invalid date format, expected ISO8601: %s", raw)
			}
		}
		return &t, nil
	}
	from, err := parseDate(params.From)
	if err != nil {
		return nil, err
	}
	to, err := parseDate(params.To)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, tag := range strings.Split(params.Tags, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	nodeKinds := splitNodeKinds(params.NodeKind)
	var nodes []db.Node
	fetchLimit := params.Limit + 1
	if params.MemoryID != "" {
		if params.Depth <= 0 {
			params.Depth = 2
		}
		nodes, err = hnd.store.GetHistoryForMemoryID(params.MemoryID, params.Depth, params.ImportantOnly, tags, nodeKinds, from, to, fetchLimit)
	} else {
		nodes, err = hnd.store.Timeline(params.Domain, params.ImportantOnly, tags, nodeKinds, from, to, fetchLimit)
	}
	if err != nil {
		return errorResult(err.Error()), nil
	}
	nodes, resultsTruncated := trimWithTruncation(nodes, params.Limit)
	entries, err := hnd.leanEntriesFromNodes(nodes)
	if err != nil {
		return nil, err
	}
	if params.Digest {
		out := struct {
			Lines            []string `json:"lines"`
			ResultsTruncated bool     `json:"results_truncated"`
		}{Lines: digestLinesFromEntries(entries), ResultsTruncated: resultsTruncated}
		b, err := marshalResponseIndent(out)
		if err != nil {
			return nil, err
		}
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
	}
	out := struct {
		Nodes            []leanEntry `json:"nodes"`
		ResultsTruncated bool        `json:"results_truncated"`
	}{Nodes: entries, ResultsTruncated: resultsTruncated}
	b, err := marshalResponseIndent(out)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}
