package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

func (hnd *Handler) findConnections(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		FromID    string `json:"from_id"`
		FromLabel string `json:"from_label"`
		ToID      string `json:"to_id"`
		ToLabel   string `json:"to_label"`
		Domain    string `json:"domain"`
	}
	if err := decodeParams(args, &params, "why_connected"); err != nil {
		return nil, err
	}
	if params.FromID != "" && params.FromLabel != "" {
		return errorResult("cannot supply both from_id and from_label"), nil
	}
	if params.ToID != "" && params.ToLabel != "" {
		return errorResult("cannot supply both to_id and to_label"), nil
	}
	if params.FromID == "" && params.FromLabel == "" {
		return errorResult("from_id or from_label is required"), nil
	}
	if params.ToID == "" && params.ToLabel == "" {
		return errorResult("to_id or to_label is required"), nil
	}
	result, err := hnd.store.FindConnectionsResolved(params.FromID, params.FromLabel, params.ToID, params.ToLabel, params.Domain)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (hnd *Handler) tracePath(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
	}
	if err := decodeParams(args, &params, "trace"); err != nil {
		return nil, err
	}
	if params.FromID == "" || params.ToID == "" {
		return nil, fmt.Errorf("from_id and to_id are required")
	}
	result, err := hnd.store.FindPath(params.FromID, params.ToID, 6)
	if err != nil {
		return nil, err
	}
	if len(result.Path) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("No path found between %q and %q within 6 hops.", params.FromID, params.ToID)}}}, nil
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// sanitiseMermaidLabel truncates to 40 runes and escapes characters that break
// Mermaid node label syntax (double-quotes, newlines).
func sanitiseMermaidLabel(s string) string {
	runes := []rune(s)
	if len(runes) > 40 {
		s = string(runes[:37]) + "..."
	} else {
		s = string(runes)
	}
	s = strings.ReplaceAll(s, "\"", "#quot;")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (hnd *Handler) visualise(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Domain   string `json:"domain"`
		MemoryID string `json:"memory_id"`
		Limit    int    `json:"limit"`
	}
	if err := decodeParams(args, &params, "visualise"); err != nil {
		return nil, err
	}

	var nodes []db.Node
	var edges []db.Edge
	var truncated bool
	var nodesTotal, edgesTotal int

	switch {
	case params.MemoryID != "":
		var err error
		nodes, edges, err = hnd.store.GetNodeNeighbourhood(params.MemoryID)
		if err != nil {
			return &ToolResult{IsError: true, Content: []ContentBlock{{Type: "text", Text: err.Error()}}}, nil
		}
	case params.Domain != "":
		if params.Limit <= 0 {
			params.Limit = 40
		}
		var err error
		nodes, edges, truncated, nodesTotal, edgesTotal, err = hnd.store.GetDomainGraph(params.Domain, params.Limit)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return errorResult(`{"error":"no content found for domain"}`), nil
		}
	default:
		return nil, fmt.Errorf("domain or memory_id is required")
	}

	// Build positional alias map (n0, n1, …) so Mermaid source stays readable.
	idMap := make(map[string]string, len(nodes))
	for i, node := range nodes {
		idMap[node.ID] = fmt.Sprintf("n%d", i)
	}

	var sb strings.Builder
	sb.WriteString("flowchart TD\n")
	for i, node := range nodes {
		label := sanitiseMermaidLabel(node.Label)
		fmt.Fprintf(&sb, "  n%d[\"%s\"]\n", i, label)
	}
	for _, edge := range edges {
		from, ok1 := idMap[edge.FromNode]
		to, ok2 := idMap[edge.ToNode]
		if !ok1 || !ok2 {
			continue
		}
		fmt.Fprintf(&sb, "  %s -- \"%s\" --> %s\n", from, edge.Relationship, to)
	}

	// Structured node/edge data for rich renderers — full labels, real IDs.
	type nodeEntry struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type edgeEntry struct {
		From         string `json:"from"`
		To           string `json:"to"`
		Relationship string `json:"relationship"`
	}
	nodeList := make([]nodeEntry, len(nodes))
	for i, node := range nodes {
		nodeList[i] = nodeEntry{ID: node.ID, Label: node.Label}
	}
	edgeList := make([]edgeEntry, 0, len(edges))
	for _, edge := range edges {
		if _, ok1 := idMap[edge.FromNode]; !ok1 {
			continue
		}
		if _, ok2 := idMap[edge.ToNode]; !ok2 {
			continue
		}
		edgeList = append(edgeList, edgeEntry{From: edge.FromNode, To: edge.ToNode, Relationship: edge.Relationship})
	}

	result := struct {
		Mermaid    string      `json:"mermaid"`
		NodeCount  int         `json:"node_count"`
		NodesTotal int         `json:"nodes_total,omitempty"`
		EdgeCount  int         `json:"edge_count"`
		EdgesTotal int         `json:"edges_total,omitempty"`
		Truncated  bool        `json:"truncated,omitempty"`
		Nodes      []nodeEntry `json:"nodes"`
		Edges      []edgeEntry `json:"edges"`
	}{
		Mermaid:    sb.String(),
		NodeCount:  len(nodes),
		NodesTotal: nodesTotal,
		EdgeCount:  len(edges),
		EdgesTotal: edgesTotal,
		Truncated:  truncated,
		Nodes:      nodeList,
		Edges:      edgeList,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
