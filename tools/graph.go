package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

func (h *Handler) findConnections(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		FromLabel string `json:"from_label"`
		ToLabel   string `json:"to_label"`
		Domain    string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	result, err := h.store.FindConnections(a.FromLabel, a.ToLabel, a.Domain)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) tracePath(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		FromID string `json:"from_id"`
		ToID   string `json:"to_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.FromID == "" || a.ToID == "" {
		return nil, fmt.Errorf("from_id and to_id are required")
	}
	result, err := h.store.FindPath(a.FromID, a.ToID, 6)
	if err != nil {
		return nil, err
	}
	if len(result.Path) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("No path found between %q and %q within 6 hops.", a.FromID, a.ToID)}}}, nil
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

func (h *Handler) visualise(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain   string `json:"domain"`
		MemoryID string `json:"memory_id"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	var nodes []db.Node
	var edges []db.Edge
	var truncated bool
	var nodesTotal, edgesTotal int

	switch {
	case a.MemoryID != "":
		var err error
		nodes, edges, err = h.store.GetNodeNeighbourhood(a.MemoryID)
		if err != nil {
			return &ToolResult{IsError: true, Content: []ContentBlock{{Type: "text", Text: err.Error()}}}, nil
		}
	case a.Domain != "":
		if a.Limit <= 0 {
			a.Limit = 40
		}
		var err error
		nodes, edges, truncated, nodesTotal, edgesTotal, err = h.store.GetDomainGraph(a.Domain, a.Limit)
		if err != nil {
			return nil, err
		}
		if len(nodes) == 0 {
			return &ToolResult{Content: []ContentBlock{{Type: "text", Text: `{"error":"no content found for domain"}`}}}, nil
		}
	default:
		return nil, fmt.Errorf("domain or memory_id is required")
	}

	// Build positional alias map (n0, n1, …) so Mermaid source stays readable.
	idMap := make(map[string]string, len(nodes))
	for i, n := range nodes {
		idMap[n.ID] = fmt.Sprintf("n%d", i)
	}

	var sb strings.Builder
	sb.WriteString("flowchart TD\n")
	for i, n := range nodes {
		label := sanitiseMermaidLabel(n.Label)
		fmt.Fprintf(&sb, "  n%d[\"%s\"]\n", i, label)
	}
	for _, e := range edges {
		from, ok1 := idMap[e.FromNode]
		to, ok2 := idMap[e.ToNode]
		if !ok1 || !ok2 {
			continue
		}
		fmt.Fprintf(&sb, "  %s -- \"%s\" --> %s\n", from, e.Relationship, to)
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
	for i, n := range nodes {
		nodeList[i] = nodeEntry{ID: n.ID, Label: n.Label}
	}
	edgeList := make([]edgeEntry, 0, len(edges))
	for _, e := range edges {
		if _, ok1 := idMap[e.FromNode]; !ok1 {
			continue
		}
		if _, ok2 := idMap[e.ToNode]; !ok2 {
			continue
		}
		edgeList = append(edgeList, edgeEntry{From: e.FromNode, To: e.ToNode, Relationship: e.Relationship})
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
