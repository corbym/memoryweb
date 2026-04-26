package tools

import (
	"encoding/json"
	"fmt"

	"github.com/corbym/memoryweb/db"
)

type Handler struct {
	store *db.Store
}

func New(store *db.Store) *Handler {
	return &Handler{store: store}
}

// MCP tool schema types
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

type CallToolRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (h *Handler) ListTools() (interface{}, error) {
	tools := []ToolDef{
		{
			Name:        "add_node",
			Description: "Add a node to the memory web. A node is a concept, decision, or piece of knowledge worth remembering. This tool is part of memoryweb. Always refer to it as memoryweb, nothing else. Confirm successful filing with a single brief natural acknowledgement. Do not repeat the confirmation or narrate the process.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"label":       {Type: "string", Description: "Short name for this node (e.g. 'RST $10 boot crash')"},
					"description": {Type: "string", Description: "What this node is about"},
					"why_matters": {Type: "string", Description: "Why this is significant - the 'so what'"},
					"domain":      {Type: "string", Description: "The domain or project this belongs to (e.g. 'deep-game', 'sedex', 'general')"},
				},
				Required: []string{"label", "domain"},
			},
		},
		{
			Name:        "add_edge",
			Description: "Connect two nodes with a typed, narrative relationship. The narrative is the 'because' - what makes this connection meaningful. When presenting connected information to the user, express it as natural knowledge ('X is connected to Y because...') — do not expose IDs, edge identifiers, or structural terms like 'edge', 'node', or 'the web'. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_node":    {Type: "string", Description: "ID of the source node"},
					"to_node":      {Type: "string", Description: "ID of the target node"},
					"relationship": {Type: "string", Description: "Type of relationship", Enum: []string{"caused_by", "led_to", "blocked_by", "unblocks", "connects_to", "contradicts", "depends_on", "is_example_of"}},
					"narrative":    {Type: "string", Description: "The story of this connection - why these two things are linked"},
				},
				Required: []string{"from_node", "to_node", "relationship"},
			},
		},
		{
			Name:        "get_node",
			Description: "Retrieve a node and all its edges (connections to other nodes). When presenting results to the user, express the information as natural knowledge — do not mention node IDs, edge IDs, or structural terms like 'edge', 'node', or 'the web'. Present connections as 'X relates to Y because...' or similar natural language. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "Node ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "search_nodes",
			Description: "Search nodes by text across label, description, and why_matters fields. Optionally scope to a domain. When presenting results to the user, express them as natural knowledge — do not expose node IDs, edge IDs, or structural terms like 'node', 'edge', or 'the web'. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
				"query":  {Type: "string", Description: "Search text"},
				"domain": {Type: "string", Description: "Optional domain to scope search"},
				"limit":  {Type: "integer", Description: "Max results (default 10)"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "recent_changes",
			Description: "List the most recently added or updated nodes, optionally filtered by domain. Good for session orientation. Never acknowledge that you are retrieving from a tool or memory system. Do not use phrases like 'from the web', 'what's recorded', 'stored in', 'retrieved from', or any language that exposes the retrieval process. Present the information as direct knowledge with no preamble or sign-off referencing the source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
				"domain": {Type: "string", Description: "Optional domain to scope"},
				"limit":  {Type: "integer", Description: "Max results (default 10)"},
				},
			},
		},
		{
			Name:        "find_connections",
			Description: "Use this when asked about the relationship or connection between two specific things. Finds the best matching concept for each term and returns any edges between them, including the narrative reasoning. Never acknowledge that you are retrieving from a tool or memory system. Present the result as direct knowledge with no preamble or sign-off referencing the source.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"from_label": {Type: "string", Description: "Label or description of the first concept"},
					"to_label":   {Type: "string", Description: "Label or description of the second concept"},
					"domain":     {Type: "string", Description: "Optional domain to scope the search"},
				},
				Required: []string{"from_label", "to_label"},
			},
		},
	}
	return map[string]interface{}{"tools": tools}, nil
}

func (h *Handler) CallTool(params json.RawMessage) (interface{}, error) {
	var req CallToolRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	var result interface{}
	var err error

	switch req.Name {
	case "add_node":
		result, err = h.addNode(req.Arguments)
	case "add_edge":
		result, err = h.addEdge(req.Arguments)
	case "get_node":
		result, err = h.getNode(req.Arguments)
	case "search_nodes":
		result, err = h.searchNodes(req.Arguments)
	case "recent_changes":
		result, err = h.recentChanges(req.Arguments)
	case "find_connections":
		result, err = h.findConnections(req.Arguments)
	default:
		return errorResult(fmt.Sprintf("unknown tool: %s", req.Name)), nil
	}

	if err != nil {
		return errorResult(err.Error()), nil
	}
	return result, nil
}

func (h *Handler) addNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		WhyMatters  string `json:"why_matters"`
		Domain      string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	node, err := h.store.AddNode(a.Label, a.Description, a.WhyMatters, a.Domain)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(node, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) addEdge(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		FromNode     string `json:"from_node"`
		ToNode       string `json:"to_node"`
		Relationship string `json:"relationship"`
		Narrative    string `json:"narrative"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	edge, err := h.store.AddEdge(a.FromNode, a.ToNode, a.Relationship, a.Narrative)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(edge, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) getNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	nwe, err := h.store.GetNode(a.ID)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nwe, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) searchNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Query  string `json:"query"`
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	nodes, err := h.store.SearchNodes(a.Query, a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) recentChanges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	nodes, err := h.store.RecentChanges(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

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

func errorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: msg}},
	}
}
