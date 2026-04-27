package tools

import (
	"encoding/json"
	"fmt"
	"time"

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
			Description: "File a concept, decision, or finding. Use this for a single entry only — prefer add_nodes for batches — and always search first to avoid creating a duplicate.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"label":       {Type: "string", Description: "Short name for this node (e.g. 'RST $10 boot crash')"},
					"description": {Type: "string", Description: "What this node is about"},
					"why_matters": {Type: "string", Description: "Why this is significant - the 'so what'"},
					"domain":      {Type: "string", Description: "The domain or project this belongs to (e.g. 'deep-game', 'sedex', 'general')"},
					"occurred_at": {Type: "string", Description: "Optional ISO8601 date or datetime when this event or decision actually happened (e.g. '2026-04-01' or '2026-04-01T14:30:00Z'). Distinct from when it was filed."},
				},
				Required: []string{"label", "domain"},
			},
		},
		{
			Name:        "add_edge",
			Description: "Connect two entries with a typed, narrative relationship. Valid relationship types are: caused_by, led_to, blocked_by, unblocks, connects_to, contradicts, depends_on, is_example_of — and both node IDs must already exist before calling this.",
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
			Description: "Retrieve an entry and all its connections by ID. Only live entries are returned; use list_archived or drift if something seems missing.",
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
			Description: "Search entries by text across label, description, and why_matters. Only live entries are returned; use list_archived or drift if something seems missing.",
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
			Description: "List the most recently filed or updated entries, optionally scoped to a domain. Only live entries are returned; use list_archived or drift if something seems missing.",
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
			Description: "Find how two concepts are related, returning any connections between the best match for each term. Only live entries are returned; use list_archived or drift if something seems missing.",
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
		{
			Name:        "timeline",
			Description: "Return entries ordered by when they occurred, optionally scoped to a domain and date range. Only live entries are returned; use list_archived or drift if something seems missing.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "Optional domain to scope"},
					"from":   {Type: "string", Description: "Optional ISO8601 start date (inclusive), e.g. '2026-01-01'"},
					"to":     {Type: "string", Description: "Optional ISO8601 end date (inclusive), e.g. '2026-04-30'"},
					"limit":  {Type: "integer", Description: "Max results (default 20)"},
				},
			},
		},
		{
			Name:        "add_alias",
			Description: "Register an alternative name for a domain so both names return the same results.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"alias":  {Type: "string", Description: "The alternative name (e.g. 'binder')"},
					"domain": {Type: "string", Description: "The canonical domain it should resolve to (e.g. 'sedex')"},
				},
				Required: []string{"alias", "domain"},
			},
		},
		{
			Name:        "list_aliases",
			Description: "List all registered domain aliases and their canonical names. Reach for this during orientation or when debugging why a domain-scoped search is returning unexpected results.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "resolve_domain",
			Description: "Return the canonical domain a name resolves to. Use this when you have an alias and need its canonical domain before scoping a search or filing an entry.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Domain name or alias to resolve"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "forget_node",
			Description: "Archive an entry so it no longer surfaces in search; it can be restored at any time. Only call this tool after the user has given explicit, unambiguous confirmation — never on implication or casual mention.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id":     {Type: "string", Description: "ID of the node to archive"},
					"reason": {Type: "string", Description: "Why this node is being archived"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "restore_node",
			Description: "Restore an archived entry so it surfaces in search again. This reverses forget_node; obtain the node_id from list_archived.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"id": {Type: "string", Description: "ID of the node to restore"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "list_archived",
			Description: "List all archived entries, optionally scoped to a domain. This is the right tool when search returns nothing but you expect the content to exist.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "Optional domain to scope the listing"},
				},
			},
		},
		{
			Name:        "drift",
			Description: "Return entries that may be stale, contradicted, or duplicated. Present each result to the user and ask for individual confirmation before archiving anything.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "Optional domain to scope drift detection"},
					"limit":  {Type: "integer", Description: "Max candidates to return (default 10)"},
				},
			},
		},
		{
			Name:        "summarise_domain",
			Description: "Return all known entries for a domain structured for synthesis. Synthesise the result into concise prose covering current state, blockers, recent decisions, and open questions.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"domain": {Type: "string", Description: "The domain to summarise"},
				},
				Required: []string{"domain"},
			},
		},
		{
			Name:        "add_nodes",
			Description: "File multiple entries in a single transaction. Prefer this over multiple add_node calls when filing several findings at once.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"nodes": {Type: "array", Description: "Array of node objects. Each must have label (string, required) and domain (string, required). Optional: description, why_matters, occurred_at (ISO8601)."},
				},
				Required: []string{"nodes"},
			},
		},
		{
			Name:        "add_edges",
			Description: "Create multiple connections in a single transaction.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"edges": {Type: "array", Description: "Array of edge objects. Each must have from_node, to_node, relationship (string), and narrative (string, optional)."},
				},
				Required: []string{"edges"},
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
	case "timeline":
		result, err = h.timeline(req.Arguments)
	case "add_alias":
		result, err = h.addAlias(req.Arguments)
	case "list_aliases":
		result, err = h.listAliases(req.Arguments)
	case "resolve_domain":
		result, err = h.resolveDomain(req.Arguments)
	case "forget_node":
		result, err = h.forgetNode(req.Arguments)
	case "restore_node":
		result, err = h.restoreNode(req.Arguments)
	case "list_archived":
		result, err = h.listArchived(req.Arguments)
	case "drift":
		result, err = h.drift(req.Arguments)
	case "summarise_domain":
		result, err = h.summariseDomain(req.Arguments)
	case "add_nodes":
		result, err = h.addNodes(req.Arguments)
	case "add_edges":
		result, err = h.addEdges(req.Arguments)
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
		OccurredAt  string `json:"occurred_at"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var occurredAt *time.Time
	if a.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, a.OccurredAt)
		if err != nil {
			t, err = time.Parse("2006-01-02", a.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("invalid occurred_at format, expected ISO8601 date or datetime: %s", a.OccurredAt)
			}
		}
		occurredAt = &t
	}
	node, err := h.store.AddNode(a.Label, a.Description, a.WhyMatters, a.Domain, occurredAt)
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

func (h *Handler) timeline(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
		From   string `json:"from"`
		To     string `json:"to"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Limit <= 0 {
		a.Limit = 20
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
	nodes, err := h.store.Timeline(a.Domain, from, to, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) addAlias(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Alias  string `json:"alias"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.AddAlias(a.Alias, a.Domain); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("alias %q → %q registered", a.Alias, a.Domain)}}}, nil
}

func (h *Handler) listAliases(_ json.RawMessage) (*ToolResult, error) {
	aliases, err := h.store.ListAliases()
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(aliases, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) resolveDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	canonical := h.store.ResolveAlias(a.Name)
	msg := fmt.Sprintf("%q resolves to %q", a.Name, canonical)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: msg}}}, nil
}

func (h *Handler) forgetNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.ArchiveNode(a.ID, a.Reason); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q archived. It can be restored at any time with restore_node.", a.ID),
	}}}, nil
}

func (h *Handler) restoreNode(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := h.store.RestoreNode(a.ID); err != nil {
		return nil, err
	}
	return &ToolResult{Content: []ContentBlock{{
		Type: "text",
		Text: fmt.Sprintf("Node %q restored and is now visible in search and retrieval.", a.ID),
	}}}, nil
}

func (h *Handler) listArchived(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	nodes, err := h.store.ListArchived(a.Domain)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(nodes, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func errorResult(msg string) *ToolResult {
	return &ToolResult{
		IsError: true,
		Content: []ContentBlock{{Type: "text", Text: msg}},
	}
}

func (h *Handler) drift(args json.RawMessage) (*ToolResult, error) {
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
	candidates, err := h.store.FindDrift(a.Domain, a.Limit)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(candidates, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) summariseDomain(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}

	// Step 1: fetch all live nodes for the domain.
	sr, err := h.store.SearchNodes("", a.Domain, 100)
	if err != nil {
		return nil, err
	}
	if len(sr.Nodes) == 0 {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: "Nothing has been filed for this domain yet."}}}, nil
	}

	// Step 2: fetch recent changes.
	recent, err := h.store.RecentChanges(a.Domain, 50)
	if err != nil {
		return nil, err
	}

	// Step 3: build structured response for the model to synthesise.
	type nodeEntry struct {
		Label       string  `json:"label"`
		Description string  `json:"description,omitempty"`
		WhyMatters  string  `json:"why_matters,omitempty"`
		OccurredAt  *string `json:"occurred_at,omitempty"`
	}
	toEntry := func(n db.Node) nodeEntry {
		e := nodeEntry{
			Label:       n.Label,
			Description: n.Description,
			WhyMatters:  n.WhyMatters,
		}
		if n.OccurredAt != nil {
			s := n.OccurredAt.Format("2006-01-02")
			e.OccurredAt = &s
		}
		return e
	}

	nodes := make([]nodeEntry, len(sr.Nodes))
	for i, n := range sr.Nodes {
		nodes[i] = toEntry(n)
	}
	recentEntries := make([]nodeEntry, len(recent))
	for i, n := range recent {
		recentEntries[i] = toEntry(n)
	}

	resp := struct {
		SummaryHint string      `json:"summary_hint"`
		Nodes       interface{} `json:"nodes"`
		Recent      interface{} `json:"recent"`
	}{
		SummaryHint: "Synthesise the following into a narrative paragraph (max 300 words) covering: current state, known blockers, recent decisions, and open questions. Plain prose, no bullet points.",
		Nodes:       nodes,
		Recent:      recentEntries,
	}

	b, _ := json.MarshalIndent(resp, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) addNodes(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Nodes []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
			WhyMatters  string `json:"why_matters"`
			Domain      string `json:"domain"`
			OccurredAt  string `json:"occurred_at"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	inputs := make([]db.NodeInput, len(a.Nodes))
	for i, n := range a.Nodes {
		var occurredAt *time.Time
		if n.OccurredAt != "" {
			t, err := time.Parse(time.RFC3339, n.OccurredAt)
			if err != nil {
				t, err = time.Parse("2006-01-02", n.OccurredAt)
				if err != nil {
					return nil, fmt.Errorf("node %d: invalid occurred_at: %s", i, n.OccurredAt)
				}
			}
			occurredAt = &t
		}
		inputs[i] = db.NodeInput{
			Label:       n.Label,
			Description: n.Description,
			WhyMatters:  n.WhyMatters,
			Domain:      n.Domain,
			OccurredAt:  occurredAt,
		}
	}
	nodes, err := h.store.AddNodesBatch(inputs)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	b, _ := json.MarshalIndent(ids, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}

func (h *Handler) addEdges(args json.RawMessage) (*ToolResult, error) {
	var a struct {
		Edges []struct {
			FromNode     string `json:"from_node"`
			ToNode       string `json:"to_node"`
			Relationship string `json:"relationship"`
			Narrative    string `json:"narrative"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	inputs := make([]db.EdgeInput, len(a.Edges))
	for i, e := range a.Edges {
		inputs[i] = db.EdgeInput{
			FromNode:     e.FromNode,
			ToNode:       e.ToNode,
			Relationship: e.Relationship,
			Narrative:    e.Narrative,
		}
	}
	edges, err := h.store.AddEdgesBatch(inputs)
	if err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(map[string]int{"edges_created": len(edges)}, "", "  ")
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(b)}}}, nil
}
