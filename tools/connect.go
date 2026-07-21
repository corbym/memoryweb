package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/corbym/memoryweb/db"
)

var connectVerdictValues = []string{"false_positive", "reconciled", "superseded"}

func validateConnectVerdict(verdict string) error {
	if verdict == "" {
		return nil
	}
	for _, v := range connectVerdictValues {
		if verdict == v {
			return nil
		}
	}
	return fmt.Errorf("invalid verdict %q — must be one of: %s", verdict, strings.Join(connectVerdictValues, ", "))
}

func connectVerdictForRelationship(relationship, verdict string) string {
	if relationship == "resolved" {
		return verdict
	}
	return ""
}

func (h *Handler) addEdge(args json.RawMessage) (*ToolResult, error) {
	return dispatchBatch(args, "connect", h.addEdgeSingle, h.addEdgesBatch)
}

func (h *Handler) addEdgeSingle(args json.RawMessage) (*ToolResult, error) {
	// Detect retired parameter names before unmarshalling.
	if msg := detectLegacyEdgeKeys(args); msg != "" {
		return errorResult(msg), nil
	}

	var a struct {
		FromMemory   string `json:"from_memory"`
		ToMemory     string `json:"to_memory"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
		Verdict      string `json:"verdict"`
	}
	if err := decodeParams(args, &a, "connect"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(map[string]string{
		"from_memory": a.FromMemory,
		"to_memory":   a.ToMemory,
	}); err != nil {
		return nil, err
	}
	if err := validateConnectVerdict(a.Verdict); err != nil {
		return errorResult(err.Error()), nil
	}
	edge, err := h.store.AddEdge(a.FromMemory, a.ToMemory, a.Relationship, a.Narrative, connectVerdictForRelationship(a.Relationship, a.Verdict))
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(edge, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

// detectLegacyEdgeKeys inspects raw JSON for retired connect parameter names
// (from_node, to_node). Returns a non-empty error message if found.
func detectLegacyEdgeKeys(raw json.RawMessage) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	_, hasFromNode := m["from_node"]
	_, hasToNode := m["to_node"]
	_, hasFromMemory := m["from_memory"]
	_, hasToMemory := m["to_memory"]
	var bad []string
	if hasFromNode && !hasFromMemory {
		bad = append(bad, "'from_node'")
	}
	if hasToNode && !hasToMemory {
		bad = append(bad, "'to_node'")
	}
	if len(bad) == 0 {
		return ""
	}
	return "Unknown parameter " + strings.Join(bad, " and ") +
		". The connect tool uses 'from_memory' and 'to_memory'. Call tools/list to refresh your schema."
}

// addEdgesBatch handles the batch mode of connect: items is the raw JSON array of edge objects.
func (h *Handler) addEdgesBatch(items json.RawMessage) (*ToolResult, error) {
	type edgeItem struct {
		FromMemory   string `json:"from_memory"`
		ToMemory     string `json:"to_memory"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
		Verdict      string `json:"verdict"`
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(items, &rawItems); err != nil {
		return nil, err
	}
	for i, raw := range rawItems {
		if msg := detectLegacyEdgeKeys(raw); msg != "" {
			return errorResult(fmt.Sprintf("item %d: %s", i, msg)), nil
		}
	}
	edgeList, err := decodeBatchItems[edgeItem](items, "connect")
	if err != nil {
		return nil, err
	}
	inputs := make([]db.EdgeInput, len(edgeList))
	for i, e := range edgeList {
		if err := validateConnectVerdict(e.Verdict); err != nil {
			return errorResult(fmt.Sprintf("item %d: %s", i, err.Error())), nil
		}
		inputs[i] = db.EdgeInput{
			FromNode:     e.FromMemory,
			ToNode:       e.ToMemory,
			Relationship: e.Relationship,
			Narrative:    e.Narrative,
			Verdict:      connectVerdictForRelationship(e.Relationship, e.Verdict),
		}
	}
	edges, err := h.store.AddEdgesBatch(inputs)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(map[string]int{"edges_created": len(edges)}, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) suggestEdges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := decodeParams(args, &a, "suggest_connections"); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if a.Limit <= 0 {
		a.Limit = 5
	}
	suggestions, err := h.store.SuggestEdges(a.ID, a.Limit)
	if err != nil {
		return nil, err
	}
	if suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}
	b, _ := json.MarshalIndent(suggestions, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) disconnect(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := decodeParams(args, &a, "disconnect"); err != nil {
		return nil, err
	}
	if a.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.store.DeleteEdge(a.ID); err != nil {
		return errorResult(err.Error()), nil
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Edge %q removed.", a.ID)}}}, nil
}
