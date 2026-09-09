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
	if verdict == "supersedes" {
		return fmt.Errorf(`invalid verdict %q — use "superseded" (verdict enum is past tense; relationship type is "supersedes")`, verdict)
	}
	for _, value := range connectVerdictValues {
		if verdict == value {
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

func (hnd *Handler) addEdge(args json.RawMessage) (*ToolResult, error) {
	return dispatchBatch(args, "connect", hnd.addEdgeSingle, hnd.addEdgesBatch)
}

func (hnd *Handler) addEdgeSingle(args json.RawMessage) (*ToolResult, error) {
	// Detect retired parameter names before unmarshalling.
	if msg := detectLegacyEdgeKeys(args); msg != "" {
		return errorResult(msg), nil
	}

	var params struct {
		FromMemory   string `json:"from_memory"`
		ToMemory     string `json:"to_memory"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
		Verdict      string `json:"verdict"`
	}
	if err := decodeParams(args, &params, "connect"); err != nil {
		return nil, err
	}
	if err := requireNonEmpty(map[string]string{
		"from_memory":  params.FromMemory,
		"to_memory":    params.ToMemory,
		"relationship": params.Relationship,
	}); err != nil {
		return nil, err
	}
	if err := validateConnectVerdict(params.Verdict); err != nil {
		return errorResult(err.Error()), nil
	}
	edge, err := hnd.store.AddEdge(params.FromMemory, params.ToMemory, params.Relationship, params.Narrative, connectVerdictForRelationship(params.Relationship, params.Verdict))
	if err != nil {
		return nil, err
	}
	b, err := marshalResponseIndent(edge)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
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
func (hnd *Handler) addEdgesBatch(items json.RawMessage) (*ToolResult, error) {
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
	type rejection struct {
		FromMemory string `json:"from_memory"`
		ToMemory   string `json:"to_memory"`
		ErrorClass string `json:"error_class"`
		Message    string `json:"message"`
	}
	rejections := make([]rejection, 0)
	inputs := make([]db.EdgeInput, 0, len(edgeList))
	for i, edge := range edgeList {
		if err := validateConnectVerdict(edge.Verdict); err != nil {
			return errorResult(fmt.Sprintf("item %d: %s", i, err.Error())), nil
		}
		if strings.TrimSpace(edge.FromMemory) == "" {
			rejections = append(rejections, rejection{
				FromMemory: edge.FromMemory,
				ToMemory:   edge.ToMemory,
				ErrorClass: "validation",
				Message:    fmt.Sprintf("item %d: from_memory is required", i),
			})
			continue
		}
		if strings.TrimSpace(edge.ToMemory) == "" {
			rejections = append(rejections, rejection{
				FromMemory: edge.FromMemory,
				ToMemory:   edge.ToMemory,
				ErrorClass: "validation",
				Message:    fmt.Sprintf("item %d: to_memory is required", i),
			})
			continue
		}
		if strings.TrimSpace(edge.Relationship) == "" {
			rejections = append(rejections, rejection{
				FromMemory: edge.FromMemory,
				ToMemory:   edge.ToMemory,
				ErrorClass: "validation",
				Message:    fmt.Sprintf("item %d: relationship is required", i),
			})
			continue
		}
		inputs = append(inputs, db.EdgeInput{
			FromNode:     edge.FromMemory,
			ToNode:       edge.ToMemory,
			Relationship: edge.Relationship,
			Narrative:    edge.Narrative,
			Verdict:      connectVerdictForRelationship(edge.Relationship, edge.Verdict),
		})
	}
	edges, err := hnd.store.AddEdgesBatch(inputs)
	if err != nil {
		return nil, err
	}
	resp := map[string]any{"edges_created": len(edges)}
	if len(rejections) > 0 {
		resp["rejections"] = rejections
	}
	b, err := marshalResponseIndent(resp)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

func (hnd *Handler) suggestEdges(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := decodeParams(args, &params, "suggest_connections", "id"); err != nil {
		return nil, err
	}
	if params.Limit <= 0 {
		params.Limit = 5
	}
	suggestions, err := hnd.store.SuggestEdges(params.ID, params.Limit)
	if err != nil {
		return nil, err
	}
	if suggestions == nil {
		suggestions = []db.EdgeSuggestion{}
	}
	b, err := marshalResponseIndent(suggestions)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: b}}}, nil
}

func (hnd *Handler) disconnect(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := decodeParams(args, &params, "disconnect", "id"); err != nil {
		return nil, err
	}
	if err := hnd.store.DeleteEdge(params.ID); err != nil {
		return errorResult(err.Error()), nil
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Edge %q removed.", params.ID)}}}, nil
}

// disconnectAll hard-deletes multiple edges in a single atomic transaction.
func (hnd *Handler) disconnectAll(args json.RawMessage) (*ToolResult, error) {
	var params struct {
		Items []struct {
			EdgeID string `json:"edge_id"`
		} `json:"items"`
	}
	if err := decodeParams(args, &params, "disconnect_all", "items"); err != nil {
		return nil, err
	}
	if len(params.Items) == 0 {
		return errorResult("items is required and must not be empty"), nil
	}
	ids := make([]string, len(params.Items))
	for i, item := range params.Items {
		if item.EdgeID == "" {
			return errorResult(fmt.Sprintf("item %d is missing edge_id", i)), nil
		}
		ids[i] = item.EdgeID
	}
	if err := hnd.store.DeleteEdgesBatch(ids); err != nil {
		return errorResult(err.Error()), nil
	}
	b, err := json.Marshal(map[string]any{"removed": len(ids)})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool response: %w", err)
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
