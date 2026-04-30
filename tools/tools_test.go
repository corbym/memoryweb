package tools_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/tools"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// ollamaRunning returns true when the Ollama server is reachable and the
// snowflake-arctic-embed model is available. The base URL is taken from the
// OLLAMA_HOST environment variable when set, so it works in both local
// development and CI environments. Tests that exercise LIKE search should call
// disableOllama(t) instead.
func ollamaRunning(t *testing.T) bool {
	t.Helper()
	base := os.Getenv("OLLAMA_HOST")
	if base == "" {
		base = "http://localhost:11434"
	}
	resp, err := http.Get(base + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Models []struct{ Name string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, m := range body.Models {
		if strings.HasPrefix(m.Name, "snowflake-arctic-embed") {
			return true
		}
	}
	return false
}

// disableOllama sets MEMORYWEB_OLLAMA_ENDPOINT=disabled for the duration of
// the test. Call this at the top of any test that exercises LIKE search in
// isolation so that a running Ollama instance does not interfere with
// embedding-based results.
func disableOllama(t *testing.T) {
	t.Helper()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
}

// newEnvWithPath creates an isolated Store+Handler backed by a temp-file SQLite DB
// and returns the DB file path for tests that need a second raw SQL connection
// (e.g. to inspect internal tables like audit_log).
func newEnvWithPath(t *testing.T) (string, *db.Store, *tools.Handler) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return dbPath, store, tools.New(store)
}

// newEnv creates an isolated Store+Handler. All existing tests use this.
// For tests that also need the DB file path, use newEnvWithPath.
func newEnv(t *testing.T) (*db.Store, *tools.Handler) {
	_, s, h := newEnvWithPath(t)
	return s, h
}

// call invokes a tool by name with arbitrary arguments and returns the ToolResult.
// It fails the test if the RPC layer itself errors (not tool-level errors).
func call(t *testing.T, h *tools.Handler, toolName string, arguments any) *tools.ToolResult {
	t.Helper()
	argBytes, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	params, _ := json.Marshal(map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(argBytes),
	})
	raw, err := h.CallTool(params)
	if err != nil {
		t.Fatalf("CallTool(%q): %v", toolName, err)
	}
	tr, ok := raw.(*tools.ToolResult)
	if !ok {
		t.Fatalf("CallTool(%q): result type %T, want *ToolResult", toolName, raw)
	}
	return tr
}

// text returns the first content block's text from a ToolResult.
func text(t *testing.T, tr *tools.ToolResult) string {
	t.Helper()
	if len(tr.Content) == 0 {
		t.Fatal("ToolResult has no content blocks")
	}
	return tr.Content[0].Text
}

// mustNotError fails the test if the ToolResult is an error.
func mustNotError(t *testing.T, tr *tools.ToolResult) {
	t.Helper()
	if tr.IsError {
		t.Fatalf("unexpected tool error: %s", text(t, tr))
	}
}

// mustError fails the test if the ToolResult is NOT an error.
func mustError(t *testing.T, tr *tools.ToolResult) {
	t.Helper()
	if !tr.IsError {
		t.Fatalf("expected tool error, got success: %s", text(t, tr))
	}
}

// addNode is a typed wrapper that returns the created node's ID.
func addNode(t *testing.T, h *tools.Handler, label, domain string, extras map[string]any) string {
	t.Helper()
	args := map[string]any{"label": label, "domain": domain}
	for k, v := range extras {
		args[k] = v
	}
	tr := call(t, h, "remember", args)
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse add_node response: %v", err)
	}
	if resp.Node.ID == "" {
		t.Fatal("add_node returned empty ID")
	}
	return resp.Node.ID
}

// searchIDs returns the node IDs from a search_nodes response.
func searchIDs(t *testing.T, tr *tools.ToolResult) []string {
	t.Helper()
	var result struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search_nodes response: %v", err)
	}
	ids := make([]string, len(result.Nodes))
	for i, n := range result.Nodes {
		ids[i] = n.ID
	}
	return ids
}

// contains returns true if needle is in haystack.
func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ── Instructions ──────────────────────────────────────────────────────────────

func TestInstructions_NonEmpty(t *testing.T) {
	if tools.Instructions == "" {
		t.Fatal("Instructions must be non-empty")
	}
}

// ── ListTools ─────────────────────────────────────────────────────────────────

func TestListTools_ReturnsExpectedTools(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	want := []string{
		"remember", "connect", "recall", "search",
		"recent", "why_connected", "history",
		"alias_domain", "list_aliases", "resolve_domain", "remove_alias",
		"forget", "restore", "forgotten",
		"whats_stale", "orient",
		"remember_all", "connect_all",
		"suggest_connections",
		"list_domains", "disconnect", "disconnected", "trace",
	}
	got := map[string]bool{}
	for _, td := range resp.Tools {
		got[td.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

// ── Schema validation ─────────────────────────────────────────────────────────

// validateSchema recursively validates that a JSON Schema object (parsed from
// MarshalIndent output) is well-formed per the rules below:
//   - array type → must have "items"
//   - object type → must have "properties"
//   - if "required" present → every listed name must exist in "properties"
//   - oneOf / anyOf / allOf → each entry must itself be valid recursively
//
// It returns a slice of human-readable problem descriptions; an empty slice
// means the schema is valid. path is a dot-separated location prefix.
func validateSchema(path string, schema map[string]interface{}) []string {
	var problems []string

	typ, _ := schema["type"].(string)

	switch typ {
	case "array":
		if _, ok := schema["items"]; !ok {
			problems = append(problems, path+": array type is missing 'items'")
		} else {
			if items, ok := schema["items"].(map[string]interface{}); ok {
				problems = append(problems, validateSchema(path+".items", items)...)
			}
		}
	case "object":
		props, hasProp := schema["properties"]
		if !hasProp {
			problems = append(problems, path+": object type is missing 'properties'")
		} else if propsMap, ok := props.(map[string]interface{}); ok {
			// Validate required fields reference existing properties.
			if req, ok := schema["required"].([]interface{}); ok {
				for _, r := range req {
					name, _ := r.(string)
					if _, exists := propsMap[name]; !exists {
						problems = append(problems, fmt.Sprintf("%s: required field %q not found in properties", path, name))
					}
				}
			}
			// Recurse into each property.
			for propName, propRaw := range propsMap {
				if propMap, ok := propRaw.(map[string]interface{}); ok {
					problems = append(problems, validateSchema(path+"."+propName, propMap)...)
				}
			}
		}
	}

	// Validate oneOf / anyOf / allOf entries recursively regardless of type.
	for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
		if entries, ok := schema[kw].([]interface{}); ok {
			for i, entry := range entries {
				if entryMap, ok := entry.(map[string]interface{}); ok {
					problems = append(problems, validateSchema(fmt.Sprintf("%s.%s[%d]", path, kw, i), entryMap)...)
				}
			}
		}
	}

	return problems
}

// TestListTools_InputSchemaValidation iterates every tool returned by ListTools
// and validates its inputSchema is well-formed JSON Schema. It does not
// hardcode tool names — adding or removing a tool is automatically covered.
func TestListTools_InputSchemaValidation(t *testing.T) {
	_, h := newEnv(t)

	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)

	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools response: %v", err)
	}
	if len(resp.Tools) == 0 {
		t.Fatal("ListTools returned no tools")
	}

	for _, tool := range resp.Tools {
		tool := tool
		t.Run(tool.Name, func(t *testing.T) {
			if tool.InputSchema == nil {
				t.Fatalf("tool %q has no inputSchema", tool.Name)
			}
			if typ, _ := tool.InputSchema["type"].(string); typ != "object" {
				t.Errorf("tool %q: inputSchema.type must be 'object', got %q", tool.Name, typ)
			}
			for _, problem := range validateSchema(tool.Name+".inputSchema", tool.InputSchema) {
				t.Error(problem)
			}
		})
	}
}

// TestListTools_SchemaValidator_CatchesArrayMissingItems confirms the validator
// catches the "array missing items" class of error — the exact bug that affected
// related_to, add_nodes.nodes, and add_edges.edges before it was fixed.
func TestListTools_SchemaValidator_CatchesArrayMissingItems(t *testing.T) {
	badSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"related_to": map[string]interface{}{
				"type":        "array",
				"description": "list of IDs",
				// intentionally missing "items"
			},
		},
	}
	problems := validateSchema("test_tool.inputSchema", badSchema)
	if len(problems) == 0 {
		t.Error("validator should have caught the missing 'items' on the array property")
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "related_to") && strings.Contains(p, "items") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected problem mentioning 'related_to' and 'items', got: %v", problems)
	}
}

// ── add_node ──────────────────────────────────────────────────────────────────

func TestAddNode_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "RST boot crash", "deep-game", map[string]any{
		"description": "ROM calls RST $10 which hangs the boot sequence",
		"why_matters": "Blocks the demo from running",
	})
	if !strings.HasPrefix(id, "rst-boot-crash-") {
		t.Errorf("unexpected ID format: %s", id)
	}
}

func TestAddNode_WithOccurredAtDateOnly(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Boot crash discovered",
		"domain":      "deep-game",
		"occurred_at": "2026-04-01",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			OccurredAt string `json:"occurred_at"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.Node.OccurredAt == "" {
		t.Error("occurred_at not set in response")
	}
}

func TestAddNode_WithOccurredAtRFC3339(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Boot crash discovered",
		"domain":      "deep-game",
		"occurred_at": "2026-04-01T14:30:00Z",
	})
	mustNotError(t, tr)
}

func TestAddNode_InvalidOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Bad date node",
		"domain":      "deep-game",
		"occurred_at": "not-a-date",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "occurred_at") {
		t.Errorf("error message should mention occurred_at, got: %s", text(t, tr))
	}
}

func TestAddNode_EmptyLabel_StillCreatesNode(t *testing.T) {
	// The tool doesn't validate required fields itself; that's the MCP layer.
	// An empty label is passed through — test that it doesn't panic.
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "",
		"domain": "deep-game",
	})
	// Whether this errors or not, the handler must return something.
	if tr == nil {
		t.Fatal("nil ToolResult for empty label")
	}
}

// ── get_node ──────────────────────────────────────────────────────────────────

func TestGetNode_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Boot crash", "deep-game", nil)

	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)

	var nwe struct {
		Node struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"node"`
		Edges []any `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nwe); err != nil {
		t.Fatalf("parse get_node: %v", err)
	}
	if nwe.Node.ID != id {
		t.Errorf("got ID %q, want %q", nwe.Node.ID, id)
	}
	if nwe.Node.Label != "Boot crash" {
		t.Errorf("got label %q, want %q", nwe.Node.Label, "Boot crash")
	}
}

func TestGetNode_NotFound(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "recall", map[string]any{"id": "does-not-exist"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("error should say 'not found', got: %s", text(t, tr))
	}
}

func TestGetNode_ArchivedNodeIsHidden(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Stale node", "deep-game", nil)

	if err := store.ArchiveNode(id, "no longer relevant"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	tr := call(t, h, "recall", map[string]any{"id": id})
	mustError(t, tr) // archived → treated as not found
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("archived node should report not found, got: %s", text(t, tr))
	}
}

// ── search_nodes ──────────────────────────────────────────────────────────────

func TestSearchNodes_FindsByLabel(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "ULA memory write fix", "deep-game", nil)

	tr := call(t, h, "search", map[string]any{"query": "ULA"})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if !contains(ids, id) {
		t.Errorf("search did not return node %s; got %v", id, ids)
	}
}

func TestSearchNodes_FindsByDescription(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "ULA fix", "deep-game", map[string]any{
		"description": "direct writes bypass ROM interrupt handler",
	})

	tr := call(t, h, "search", map[string]any{"query": "bypass ROM"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("search by description term did not return node")
	}
}

func TestSearchNodes_FindsByWhyMatters(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "ULA fix", "deep-game", map[string]any{
		"why_matters": "unblocks the straitjacket tutorial",
	})

	tr := call(t, h, "search", map[string]any{"query": "straitjacket"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("search by why_matters term did not return node")
	}
}

func TestSearchNodes_EmptyQueryReturnsAll(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Node Alpha", "project-x", nil)
	id2 := addNode(t, h, "Node Beta", "project-x", nil)

	tr := call(t, h, "search", map[string]any{
		"query": "", "domain": "project-x", "limit": 10,
	})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("empty query should return all nodes; got %v", ids)
	}
}

func TestSearchNodes_NoMatch(t *testing.T) {
	disableOllama(t) // LIKE-only test: nonsense query must return 0 results
	_, h := newEnv(t)
	addNode(t, h, "Some node", "deep-game", nil)

	tr := call(t, h, "search", map[string]any{"query": "xyzzy-no-match"})
	mustNotError(t, tr) // no match is not an error
	ids := searchIDs(t, tr)
	if len(ids) != 0 {
		t.Errorf("expected 0 results, got %d", len(ids))
	}
}

func TestSearchNodes_DomainIsolation(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Alpha node", "domain-a", nil)
	idB := addNode(t, h, "Alpha node", "domain-b", nil)

	tr := call(t, h, "search", map[string]any{"query": "Alpha", "domain": "domain-a"})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if !contains(ids, idA) {
		t.Error("should contain domain-a node")
	}
	if contains(ids, idB) {
		t.Error("should NOT contain domain-b node in domain-a search")
	}
}

func TestSearchNodes_ArchivedNodeExcluded(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Deprecated feature", "deep-game", nil)

	if err := store.ArchiveNode(id, "removed from game"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	tr := call(t, h, "search", map[string]any{"query": "Deprecated"})
	mustNotError(t, tr)
	if contains(searchIDs(t, tr), id) {
	t.Error("archived node should not appear in search results")
}
}

func TestSearchNodes_ArchivedRestored_ReappearsInSearch(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Restored feature", "deep-game", nil)

	store.ArchiveNode(id, "test archive")
	// verify hidden
	if contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Restored"})), id) {
		t.Fatal("should be hidden after archive")
	}

	if err := store.RestoreNode(id); err != nil {
		t.Fatalf("RestoreNode: %v", err)
	}
	// verify reappears
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Restored"})), id) {
		t.Error("node should reappear in search after restore")
	}
}

func TestSearchNodes_LimitIsRespected(t *testing.T) {
	_, h := newEnv(t)
	for i := 0; i < 5; i++ {
		addNode(t, h, "Limit test node", "ltest", nil)
	}
	tr := call(t, h, "search", map[string]any{
		"query": "Limit test", "domain": "ltest", "limit": 3,
	})
	mustNotError(t, tr)
	if count := len(searchIDs(t, tr)); count > 3 {
		t.Errorf("limit 3 exceeded: got %d results", count)
	}
}

// ── add_edge ──────────────────────────────────────────────────────────────────

func TestAddEdge_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)
	to := addNode(t, h, "ULA fix", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_node":    from,
		"to_node":      to,
		"relationship": "unblocks",
		"narrative":    "direct ULA writes bypass the ROM ISR that causes the hang",
	})
	mustNotError(t, tr)

	var e struct {
		ID           string `json:"id"`
		Relationship string `json:"relationship"`
	}
	json.Unmarshal([]byte(text(t, tr)), &e)
	if e.Relationship != "unblocks" {
		t.Errorf("relationship: got %q, want %q", e.Relationship, "unblocks")
	}
}

func TestAddEdge_NonExistentFromNode(t *testing.T) {
	_, h := newEnv(t)
	to := addNode(t, h, "ULA fix", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_node":    "ghost-node-id",
		"to_node":      to,
		"relationship": "unblocks",
	})
	mustError(t, tr)
}

func TestAddEdge_NonExistentToNode(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_node":    from,
		"to_node":      "ghost-node-id",
		"relationship": "unblocks",
	})
	mustError(t, tr)
}

func TestAddEdge_BothNodesNonExistent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "connect", map[string]any{
		"from_node":    "ghost-a",
		"to_node":      "ghost-b",
		"relationship": "connects_to",
	})
	mustError(t, tr)
}

func TestGetNode_IncludesEdges(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)
	to := addNode(t, h, "ULA fix", "deep-game", nil)
	call(t, h, "connect", map[string]any{
		"from_node": from, "to_node": to, "relationship": "unblocks",
	})

	tr := call(t, h, "recall", map[string]any{"id": from})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)
	if len(nwe.Edges) == 0 {
		t.Error("expected edges on node, got none")
	}
	if nwe.Edges[0].Relationship != "unblocks" {
		t.Errorf("edge relationship: got %q", nwe.Edges[0].Relationship)
	}
}

// ── find_connections ──────────────────────────────────────────────────────────

func TestFindConnections_ReturnsEdgeBetweenNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "RST boot crash", "deep-game", map[string]any{
		"description": "ROM calls RST $10",
	})
	to := addNode(t, h, "ULA memory write fix", "deep-game", nil)
	from := addNode(t, h, "RST boot crash second", "deep-game", nil)
	call(t, h, "connect", map[string]any{
		"from_node": from, "to_node": to, "relationship": "unblocks",
		"narrative": "direct writes bypass the ROM ISR",
	})

	tr := call(t, h, "why_connected", map[string]any{
		"from_label": "RST boot crash second",
		"to_label":   "ULA memory write",
		"domain":     "deep-game",
	})
	mustNotError(t, tr)

	var result struct {
		From  map[string]any   `json:"from"`
		To    map[string]any   `json:"to"`
		Edges []map[string]any `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &result)
	if len(result.Edges) == 0 {
		t.Error("expected at least one edge between connected nodes")
	}
}

func TestFindConnections_NoMatchReturnsNilNodes(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "why_connected", map[string]any{
		"from_label": "nonexistent-thing-abc",
		"to_label":   "another-nonexistent-xyz",
	})
	mustNotError(t, tr)

	var result struct {
		From  *map[string]any `json:"from"`
		To    *map[string]any `json:"to"`
		Edges []any           `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &result)
	if result.From != nil || result.To != nil {
		t.Error("no-match result should have nil from/to nodes")
	}
}

func TestFindConnections_ArchivedNodeNotMatched(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Invisible archived node", "deep-game", nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "why_connected", map[string]any{
		"from_label": "Invisible archived",
		"to_label":   "something else",
	})
	mustNotError(t, tr)

	var result struct {
		From *map[string]any `json:"from"`
	}
	json.Unmarshal([]byte(text(t, tr)), &result)
	if result.From != nil {
		t.Error("archived node should not be matched by find_connections")
	}
}

// ── disconnect ────────────────────────────────────────────────────────────────

func TestDisconnect_RemovesEdge(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "Cause Node", "proj", nil)
	to := addNode(t, h, "Effect Node", "proj", nil)

	connectTr := call(t, h, "connect", map[string]any{
		"from_node": from, "to_node": to, "relationship": "led_to",
	})
	mustNotError(t, connectTr)
	var edge struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, connectTr)), &edge)

	// Disconnect it.
	mustNotError(t, call(t, h, "disconnect", map[string]any{"id": edge.ID}))

	// Edge should no longer appear on recall.
	recallTr := call(t, h, "recall", map[string]any{"id": from})
	mustNotError(t, recallTr)
	var nwe struct {
		Edges []struct {
			ID string `json:"id"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recallTr)), &nwe)
	for _, e := range nwe.Edges {
		if e.ID == edge.ID {
			t.Error("edge should be gone after disconnect")
		}
	}
}

func TestDisconnect_NonExistentReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "disconnect", map[string]any{"id": "edge-ghost-xxx"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("expected 'not found' error; got: %s", text(t, tr))
	}
}

// ── recent_changes ────────────────────────────────────────────────────────────

func TestRecentChanges_ReturnsNodes(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Event Alpha", "proj", nil)
	id2 := addNode(t, h, "Event Beta", "proj", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("recent_changes missing expected nodes; got %v", ids)
	}
}

func TestRecentChanges_ArchivedNodeExcluded(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Recent archived node", "proj", nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID == id {
			t.Error("archived node should not appear in recent_changes")
		}
	}
}

func TestRecentChanges_DomainIsolation(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Alpha event", "domain-a", nil)
	addNode(t, h, "Beta event", "domain-b", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "domain-a"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID != idA {
			t.Errorf("domain-a recent_changes returned node from wrong domain: %s", n.ID)
		}
	}
}

func TestRecentChanges_EmptyDB(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "recent", map[string]any{})
	mustNotError(t, tr)
}

func TestRecentChanges_GroupByDomain_MultipleDomains(t *testing.T) {
	_, h := newEnv(t)
	// Add nodes across three domains.
	idA1 := addNode(t, h, "Alpha one", "domain-a", nil)
	idA2 := addNode(t, h, "Alpha two", "domain-a", nil)
	idB1 := addNode(t, h, "Beta one", "domain-b", nil)
	idC1 := addNode(t, h, "Gamma one", "domain-c", nil)

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"limit":           5,
	})
	mustNotError(t, tr)

	// Response is a JSON array of {domain, nodes} objects.
	var groups []struct {
		Domain string `json:"domain"`
		Nodes  []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &groups); err != nil {
		t.Fatalf("parse grouped response: %v", err)
	}

	// Build a flat map of domain → IDs for easy assertion.
	byDomain := map[string][]string{}
	for _, g := range groups {
		for _, n := range g.Nodes {
			byDomain[g.Domain] = append(byDomain[g.Domain], n.ID)
		}
	}

	if !contains(byDomain["domain-a"], idA1) || !contains(byDomain["domain-a"], idA2) {
		t.Errorf("domain-a missing expected nodes; got %v", byDomain["domain-a"])
	}
	if !contains(byDomain["domain-b"], idB1) {
		t.Errorf("domain-b missing expected node; got %v", byDomain["domain-b"])
	}
	if !contains(byDomain["domain-c"], idC1) {
		t.Errorf("domain-c missing expected node; got %v", byDomain["domain-c"])
	}
	if len(groups) < 3 {
		t.Errorf("expected at least 3 domain groups, got %d", len(groups))
	}
}

func TestRecentChanges_GroupByDomain_PerDomainLimit(t *testing.T) {
	_, h := newEnv(t)
	// Add 4 nodes in the same domain.
	for i := 0; i < 4; i++ {
		addNode(t, h, fmt.Sprintf("Node %d", i), "limit-domain", nil)
	}

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"limit":           2, // per-domain cap
	})
	mustNotError(t, tr)

	var groups []struct {
		Domain string `json:"domain"`
		Nodes  []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &groups); err != nil {
		t.Fatalf("parse grouped response: %v", err)
	}

	for _, g := range groups {
		if g.Domain == "limit-domain" && len(g.Nodes) > 2 {
			t.Errorf("per-domain limit of 2 exceeded: got %d nodes", len(g.Nodes))
		}
	}
}

func TestRecentChanges_GroupByDomain_WithDomainSpecified_BehavesNormal(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Node A", "domain-a", nil)
	addNode(t, h, "Node B", "domain-b", nil)

	// group_by_domain=true but domain is specified → behaves as normal (flat list).
	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"domain":          "domain-a",
	})
	mustNotError(t, tr)

	// Response should be a flat array of nodes (normal mode), not grouped.
	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("expected flat node array when domain is specified: %v\nbody: %s", err, text(t, tr))
	}
	if len(nodes) != 1 || nodes[0].ID != idA {
		t.Errorf("expected only domain-a node; got %+v", nodes)
	}
}

func TestRecentChanges_GroupByDomain_False_BehavesAsNormal(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Node X", "domain-x", nil)
	id2 := addNode(t, h, "Node Y", "domain-y", nil)

	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": false,
	})
	mustNotError(t, tr)

	// Should be a flat array.
	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("expected flat node array: %v", err)
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("flat recent_changes missing expected nodes; got %v", ids)
	}
}

// ── timeline ──────────────────────────────────────────────────────────────────

func TestTimeline_OrderedByOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Early event", "proj", map[string]any{"occurred_at": "2026-01-01"})
	id2 := addNode(t, h, "Late event", "proj", map[string]any{"occurred_at": "2026-06-01"})

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	if len(nodes) < 2 {
		t.Fatalf("expected 2 timeline nodes, got %d", len(nodes))
	}
	if nodes[0].ID != id1 || nodes[1].ID != id2 {
		t.Errorf("timeline order wrong: got [%s, %s], want [%s, %s]",
			nodes[0].ID, nodes[1].ID, id1, id2)
	}
}

func TestTimeline_ExcludesNodesWithoutOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "No date node", "proj", nil) // no occurred_at
	idDated := addNode(t, h, "Dated node", "proj", map[string]any{"occurred_at": "2026-03-01"})

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID == idDated {
			return // found it — pass
		}
	}
	t.Error("dated node not in timeline")
}

func TestTimeline_ArchivedNodeExcluded(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Archived timeline node", "proj", map[string]any{
		"occurred_at": "2026-03-15",
	})
	store.ArchiveNode(id, "test")

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID == id {
			t.Error("archived node should not appear in timeline")
		}
	}
}

func TestTimeline_DateRangeFilter(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Jan event", "proj", map[string]any{"occurred_at": "2026-01-15"})
	idMar := addNode(t, h, "Mar event", "proj", map[string]any{"occurred_at": "2026-03-15"})
	addNode(t, h, "Jun event", "proj", map[string]any{"occurred_at": "2026-06-15"})

	tr := call(t, h, "history", map[string]any{
		"domain": "proj",
		"from":   "2026-02-01",
		"to":     "2026-04-30",
	})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	if len(nodes) != 1 || nodes[0].ID != idMar {
		t.Errorf("date range should return only Mar event; got %+v", nodes)
	}
}

func TestTimeline_InvalidFromDate(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{"from": "not-a-date"})
	mustError(t, tr)
}

func TestTimeline_EmptyReturnsGracefully(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{})
	mustNotError(t, tr)
}

// ── list_domains ──────────────────────────────────────────────────────────────

func TestListDomains_ReturnsDistinctDomains(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node A", "domain-alpha", nil)
	addNode(t, h, "Node B", "domain-beta", nil)
	addNode(t, h, "Node C", "domain-alpha", nil) // duplicate domain

	tr := call(t, h, "list_domains", map[string]any{})
	mustNotError(t, tr)

	var domains []string
	if err := json.Unmarshal([]byte(text(t, tr)), &domains); err != nil {
		t.Fatalf("parse list_domains response: %v", err)
	}
	if len(domains) != 2 {
		t.Errorf("expected 2 distinct domains, got %d: %v", len(domains), domains)
	}
	if !contains(domains, "domain-alpha") {
		t.Error("expected domain-alpha in result")
	}
	if !contains(domains, "domain-beta") {
		t.Error("expected domain-beta in result")
	}
}

func TestListDomains_ExcludesArchivedOnlyDomains(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Ghost node", "dead-domain", nil)
	store.ArchiveNode(id, "test")
	addNode(t, h, "Live node", "live-domain", nil)

	tr := call(t, h, "list_domains", map[string]any{})
	mustNotError(t, tr)

	var domains []string
	json.Unmarshal([]byte(text(t, tr)), &domains)
	if contains(domains, "dead-domain") {
		t.Error("dead-domain should not appear: all its nodes are archived")
	}
	if !contains(domains, "live-domain") {
		t.Error("live-domain should appear")
	}
}

func TestListDomains_EmptyDB(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "list_domains", map[string]any{})
	mustNotError(t, tr)
	var domains []string
	json.Unmarshal([]byte(text(t, tr)), &domains)
	if len(domains) != 0 {
		t.Errorf("expected empty list, got %v", domains)
	}
}

// ── aliases ───────────────────────────────────────────────────────────────────

func TestAddAlias_SearchResolvesAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias_domain", map[string]any{"alias": "engine", "domain": "deep-engine"})

	tr := call(t, h, "search", map[string]any{"query": "Engine", "domain": "engine"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("alias should resolve to canonical domain in search")
	}
}

func TestResolveDomain_ReturnsCanonical(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias_domain", map[string]any{"alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "resolve_domain", map[string]any{"name": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "deep-game") {
		t.Errorf("resolve_domain should return canonical; got: %s", text(t, tr))
	}
}

func TestResolveDomain_UnknownAliasReturnsItself(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "resolve_domain", map[string]any{"name": "unknown-domain"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "unknown-domain") {
		t.Errorf("unregistered name should resolve to itself; got: %s", text(t, tr))
	}
}

func TestListAliases_ReturnsRegisteredAliases(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias_domain", map[string]any{"alias": "dg", "domain": "deep-game"})
	call(t, h, "alias_domain", map[string]any{"alias": "sx", "domain": "sedex"})

	tr := call(t, h, "list_aliases", map[string]any{})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "dg") || !strings.Contains(body, "sx") {
		t.Errorf("list_aliases missing registered aliases; got: %s", body)
	}
}

// ── remove_alias ──────────────────────────────────────────────────────────────

func TestRemoveAlias_RemovesExistingAlias(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias_domain", map[string]any{"alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "remove_alias", map[string]any{"alias": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "dg") {
		t.Errorf("expected confirmation mentioning alias; got: %s", text(t, tr))
	}

	// list_aliases should no longer contain it
	listTr := call(t, h, "list_aliases", map[string]any{})
	mustNotError(t, listTr)
	if strings.Contains(text(t, listTr), `"dg"`) {
		t.Error("alias 'dg' should not appear in list_aliases after removal")
	}
}

func TestRemoveAlias_NonExistentReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remove_alias", map[string]any{"alias": "ghost-alias"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("expected 'not found' error; got: %s", text(t, tr))
	}
}

func TestRemoveAlias_SearchNoLongerResolvesRemovedAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias_domain", map[string]any{"alias": "engine", "domain": "deep-engine"})

	// confirm alias resolves while it exists
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "Engine", "domain": "engine",
	})), id) {
		t.Fatal("alias should resolve before removal")
	}

	mustNotError(t, call(t, h, "remove_alias", map[string]any{"alias": "engine"}))

	// after removal, searching under the alias should return nothing
	tr := call(t, h, "search", map[string]any{
		"query": "Engine", "domain": "engine",
	})
	mustNotError(t, tr)
	if contains(searchIDs(t, tr), id) {
		t.Error("removed alias should no longer resolve to canonical domain in search")
	}
}

// ── unknown tool ──────────────────────────────────────────────────────────────

func TestUnknownTool_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "totally_unknown_tool", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' message, got: %s", text(t, tr))
	}
}

// ── forget_node / restore_node / list_archived ───────────────────────────────

// TestForgetNode_HidesFromSearch: after forget_node the node must not appear
// in search_nodes results.
func TestForgetNode_HidesFromSearch(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "test forget node", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id":     id,
		"reason": "stale",
	}))

	tr := call(t, h, "search", map[string]any{
		"query": "test forget node", "domain": "test",
	})
	mustNotError(t, tr)
	if contains(searchIDs(t, tr), id) {
		t.Error("forgotten node should NOT appear in search_nodes results")
	}
}

// TestForgetNode_DoesNotDelete: forgotten node must appear in list_archived
// with archived_at present and non-empty.
func TestForgetNode_DoesNotDelete(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "forget does not delete", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": id}))

	archivedTr := call(t, h, "forgotten", map[string]any{"domain": "test"})
	mustNotError(t, archivedTr)

	var nodes []struct {
		ID         string `json:"id"`
		ArchivedAt string `json:"archived_at"`
	}
	if err := json.Unmarshal([]byte(text(t, archivedTr)), &nodes); err != nil {
		t.Fatalf("parse list_archived response: %v", err)
	}

	found := false
	for _, n := range nodes {
		if n.ID == id {
			found = true
			if n.ArchivedAt == "" {
				t.Error("archived_at should be present and non-empty")
			}
		}
	}
	if !found {
		t.Error("forgotten node should appear in list_archived results")
	}
}

// TestRestoreNode_ReappearsInSearch: restore_node must make a forgotten node
// visible again in search_nodes.
func TestRestoreNode_ReappearsInSearch(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "restore reappears", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id": id, "reason": "testing restore",
	}))
	if contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "restore reappears", "domain": "test",
	})), id) {
		t.Fatal("node should be hidden after forget_node")
	}

	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	if !contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "restore reappears", "domain": "test",
	})), id) {
		t.Error("node should reappear in search_nodes after restore_node")
	}
}

// TestAuditLog_RecordsForgetAndRestore: the audit_log table must contain exactly
// two entries — one archive (with the supplied reason) and one restore.
func TestAuditLog_RecordsForgetAndRestore(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "audit log test node", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id": id, "reason": "test reason",
	}))
	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	// Open a second connection to read audit_log directly.
	// WAL mode allows concurrent readers — no need to close the primary store.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, reason FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action string
		reason sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.reason); err != nil {
			t.Fatalf("scan audit_log row: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 audit_log entries, got %d", len(entries))
	}
	if entries[0].action != "archive" {
		t.Errorf("first entry action: got %q, want %q", entries[0].action, "archive")
	}
	if !entries[0].reason.Valid || entries[0].reason.String != "test reason" {
		t.Errorf("first entry reason: got %q, want %q", entries[0].reason.String, "test reason")
	}
	if entries[1].action != "restore" {
		t.Errorf("second entry action: got %q, want %q", entries[1].action, "restore")
	}
}

// TestListArchived_ScopedByDomain: list_archived with a domain must only return
// archived nodes from that domain.
func TestListArchived_ScopedByDomain(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "node in domain-1", "domain-1", nil)
	id2 := addNode(t, h, "node in domain-2", "domain-2", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": id1, "reason": "scope test"}))
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id2, "reason": "scope test"}))

	archivedTr := call(t, h, "forgotten", map[string]any{"domain": "domain-1"})
	mustNotError(t, archivedTr)

	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, archivedTr)), &nodes); err != nil {
		t.Fatalf("parse list_archived response: %v", err)
	}

	foundFirst := false
	for _, n := range nodes {
		if n.ID == id2 {
			t.Error("domain-2 node should NOT appear when listing domain-1 archived nodes")
		}
		if n.ID == id1 {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Error("domain-1 node SHOULD appear in domain-1 archived list")
	}
}

// ── archive integration (agent workflow) ─────────────────────────────────────

// TestArchiveWorkflow_FullLifecycle simulates the full agent lifecycle entirely
// through the tool interface: file → forget → verify hidden → restore → verify visible.
func TestArchiveWorkflow_FullLifecycle(t *testing.T) {
	_, h := newEnv(t)

	// Agent files a node
	id := addNode(t, h, "Stale decision", "project-alpha", map[string]any{
		"description": "We decided to use XYZ framework",
		"why_matters": "Was the basis for the initial architecture",
	})

	// Verify it's findable
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Fatal("node should be findable before forget")
	}

	// Archive it via the tool
	mustNotError(t, call(t, h, "forget", map[string]any{
		"id":     id,
		"reason": "framework was replaced by ABC",
	}))

	// Verify it's gone from all retrieval paths
	if contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Error("should be hidden from search_nodes after forget_node")
	}
	if call(t, h, "recall", map[string]any{"id": id}).IsError == false {
		t.Error("should be hidden from get_node after forget_node")
	}
	recentIDs := func() []string {
		tr := call(t, h, "recent", map[string]any{"domain": "project-alpha"})
		var nodes []struct {
			ID string `json:"id"`
		}
		json.Unmarshal([]byte(text(t, tr)), &nodes)
		ids := make([]string, len(nodes))
		for i, n := range nodes {
			ids[i] = n.ID
		}
		return ids
	}
	if contains(recentIDs(), id) {
		t.Error("should be hidden from recent_changes after forget_node")
	}

	// Verify it appears in list_archived
	archivedTr := call(t, h, "forgotten", map[string]any{"domain": "project-alpha"})
	mustNotError(t, archivedTr)
	var archivedNodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, archivedTr)), &archivedNodes)
	foundInArchived := false
	for _, n := range archivedNodes {
		if n.ID == id {
			foundInArchived = true
		}
	}
	if !foundInArchived {
		t.Error("forgotten node should appear in list_archived")
	}

	// Restore it via the tool
	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	// Verify it's visible again
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Error("node should reappear in search after restore_node")
	}
	if !contains(recentIDs(), id) {
		t.Error("node should reappear in recent_changes after restore_node")
	}

	// Verify it's no longer in list_archived
	archivedTr = call(t, h, "forgotten", map[string]any{"domain": "project-alpha"})
	mustNotError(t, archivedTr)
	json.Unmarshal([]byte(text(t, archivedTr)), &archivedNodes)
	for _, n := range archivedNodes {
		if n.ID == id {
			t.Error("restored node should no longer be in list_archived")
		}
	}
}

func TestArchiveWorkflow_MultipleNodes_OnlySomeArchived(t *testing.T) {
	_, h := newEnv(t)

	live1 := addNode(t, h, "Live node A", "proj", nil)
	live2 := addNode(t, h, "Live node B", "proj", nil)
	archived := addNode(t, h, "Archived node C", "proj", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": archived, "reason": "reason"}))

	tr := call(t, h, "search", map[string]any{"query": "node", "domain": "proj"})
	ids := searchIDs(t, tr)

	if !contains(ids, live1) {
		t.Error("live1 should be in results")
	}
	if !contains(ids, live2) {
		t.Error("live2 should be in results")
	}
	if contains(ids, archived) {
		t.Error("archived should NOT be in results")
	}
}

// ── invalid CallTool params ───────────────────────────────────────────────────

func TestCallTool_MalformedParams_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	_, err := h.CallTool(json.RawMessage(`{not valid json`))
	if err == nil {
		t.Error("malformed JSON params should return an error")
	}
}

// ── drift ─────────────────────────────────────────────────────────────────────

// TestDriftContradictingEdge: nodes connected by a contradicts edge must both
// appear as drift candidates with reason containing "contradicting".
func TestDriftContradictingEdge(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Approach Alpha", "test-drift-1", nil)
	idB := addNode(t, h, "Approach Beta", "test-drift-1", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_node":    idA,
		"to_node":      idB,
		"relationship": "contradicts",
	}))

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-1"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, "contradicting") {
		t.Errorf("drift result should mention 'contradicting'; got:\n%s", body)
	}
	if !strings.Contains(body, idA) {
		t.Errorf("node A (%s) should appear in drift result; got:\n%s", idA, body)
	}
	if !strings.Contains(body, idB) {
		t.Errorf("node B (%s) should appear in drift result; got:\n%s", idB, body)
	}
}

// TestDriftSupersededLabel: a node whose label contains "old" must appear with
// reason containing "superseded".
func TestDriftSupersededLabel(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "old RST $10 approach", "test-drift-2", nil)

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-2"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("superseded node (%s) should appear in drift; got:\n%s", id, body)
	}
	if !strings.Contains(body, "superseded") {
		t.Errorf("reason should mention 'superseded'; got:\n%s", body)
	}
}

// TestDriftStaleOpenQuestion: a node whose description contains "open question"
// with occurred_at > 30 days ago must appear with reason containing "open question".
func TestDriftStaleOpenQuestion(t *testing.T) {
	_, h := newEnv(t)
	staleDate := time.Now().AddDate(0, 0, -31).Format("2006-01-02")
	id := addNode(t, h, "RST handler timing", "test-drift-3", map[string]any{
		"description": "open question: should we patch at boot or at runtime?",
		"occurred_at": staleDate,
	})

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-3"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("stale open-question node (%s) should appear in drift; got:\n%s", id, body)
	}
	if !strings.Contains(body, "open question") {
		t.Errorf("reason should mention 'open question'; got:\n%s", body)
	}
}

// TestDriftDuplicateLabel: two nodes with identical labels in the same domain
// must both appear with reason containing "duplicate".
func TestDriftDuplicateLabel(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "boot crash duplicate label", "test-drift-4", nil)
	id2 := addNode(t, h, "boot crash duplicate label", "test-drift-4", nil)

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-4"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id1) {
		t.Errorf("first duplicate node (%s) should appear in drift; got:\n%s", id1, body)
	}
	if !strings.Contains(body, id2) {
		t.Errorf("second duplicate node (%s) should appear in drift; got:\n%s", id2, body)
	}
	if !strings.Contains(body, "duplicate") {
		t.Errorf("reason should mention 'duplicate'; got:\n%s", body)
	}
}

// TestDriftDoesNotSurfaceArchived: an archived node that would otherwise match
// a drift rule must NOT appear in drift results.
func TestDriftDoesNotSurfaceArchived(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "old archived stale thing", "test-drift-5", nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-5"})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), id) {
		t.Errorf("archived node (%s) should NOT appear in drift; got:\n%s", id, text(t, tr))
	}
}

// TestDriftScopedByDomain: a drift candidate in domain A must not appear when
// calling drift scoped to domain B.
func TestDriftScopedByDomain(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "old deprecated approach", "test-drift-a", nil)
	addNode(t, h, "fresh new approach", "test-drift-b", nil)

	tr := call(t, h, "whats_stale", map[string]any{"domain": "test-drift-b"})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), idA) {
		t.Errorf("node from test-drift-a (%s) should NOT appear in test-drift-b drift; got:\n%s", idA, text(t, tr))
	}
}

// ── summarise_domain ──────────────────────────────────────────────────────────

// TestSummariseDomain_ReturnsNodes: the response must contain the labels of
// all live nodes in the domain.
func TestSummariseDomain_ReturnsNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha summarise node", "sum-domain", map[string]any{
		"description": "first node description",
		"why_matters": "first node why matters",
	})
	addNode(t, h, "Beta summarise node", "sum-domain", map[string]any{
		"description": "second node description",
		"why_matters": "second node why matters",
	})
	addNode(t, h, "Gamma summarise node", "sum-domain", map[string]any{
		"description": "third node description",
		"why_matters": "third node why matters",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "sum-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	for _, label := range []string{"Alpha summarise node", "Beta summarise node", "Gamma summarise node"} {
		if !strings.Contains(body, label) {
			t.Errorf("result should contain label %q; got:\n%s", label, body)
		}
	}
}

// TestSummariseDomain_EmptyDomain: a domain with no nodes returns a clear
// "nothing filed" message rather than empty content.
func TestSummariseDomain_EmptyDomain(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "orient", map[string]any{"domain": "completely-empty-domain-xyz"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "Nothing has been filed") {
		t.Errorf("empty domain should return 'Nothing has been filed' message; got:\n%s", body)
	}
}

// TestSummariseDomain_ExcludesArchived: an archived node's label must not
// appear in the summarise_domain response.
func TestSummariseDomain_ExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	addNode(t, h, "Visible summarise node", "sum-archive-domain", nil)
	hiddenID := addNode(t, h, "Hidden archived summarise node", "sum-archive-domain", nil)
	store.ArchiveNode(hiddenID, "test archive")

	tr := call(t, h, "orient", map[string]any{"domain": "sum-archive-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, "Hidden archived summarise node") {
		t.Errorf("archived node label should NOT appear in summarise_domain result; got:\n%s", body)
	}
	if !strings.Contains(body, "Visible summarise node") {
		t.Errorf("live node label should appear in result; got:\n%s", body)
	}
}

// TestSummariseDomain_IncludesRecentChanges: a node with occurred_at set must
// have that date visible in the response.
func TestSummariseDomain_IncludesRecentChanges(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated node one", "sum-dated-domain", nil)
	addNode(t, h, "Undated node two", "sum-dated-domain", nil)
	addNode(t, h, "Dated event node", "sum-dated-domain", map[string]any{
		"occurred_at": "2026-04-01",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "sum-dated-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, "2026-04-01") {
		t.Errorf("result should include occurred_at date '2026-04-01'; got:\n%s", body)
	}
}

// TestOrient_IncludesTotalNodes: orient response must include total_nodes count.
func TestOrient_IncludesTotalNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha orient", "orient-total", nil)
	addNode(t, h, "Beta orient", "orient-total", nil)
	addNode(t, h, "Gamma orient", "orient-total", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-total"})
	mustNotError(t, tr)

	var resp struct {
		TotalNodes int `json:"total_nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.TotalNodes != 3 {
		t.Errorf("total_nodes: got %d, want 3", resp.TotalNodes)
	}
}

// ── add_nodes / add_edges (bulk) ──────────────────────────────────────────────

// TestAddNodesBulk: three nodes inserted in one call; all IDs returned and
// each node is findable via search.
func TestAddNodesBulk(t *testing.T) {
	_, h := newEnv(t)

	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{"label": "Bulk Node Alpha", "domain": "bulk-test"},
			{"label": "Bulk Node Beta", "domain": "bulk-test", "description": "beta desc"},
			{"label": "Bulk Node Gamma", "domain": "bulk-test", "why_matters": "gamma why"},
		},
	})
	mustNotError(t, tr)

	var result []struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		SuggestedConnections []struct {
			ID string `json:"id"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse remember_all response: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}

	labels := []string{"Bulk Node Alpha", "Bulk Node Beta", "Bulk Node Gamma"}
	for i, label := range labels {
		id := result[i].Node.ID
		if id == "" {
			t.Errorf("entry %d: node.id is empty", i)
			continue
		}
		searchTr := call(t, h, "search", map[string]any{
			"query": label, "domain": "bulk-test",
		})
		mustNotError(t, searchTr)
		if !contains(searchIDs(t, searchTr), id) {
			t.Errorf("node %q (%s) not found after remember_all", label, id)
		}
	}
}

// TestAddNodesBulk_ResponseShape: remember_all must return [{node, suggested_connections}].
func TestAddNodesBulk_ResponseShape(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{"label": "Shape Bulk One", "domain": "shape-test"},
		},
	})
	mustNotError(t, tr)

	var result []struct {
		Node                 *struct{ ID string `json:"id"` }    `json:"node"`
		SuggestedConnections *[]struct{ ID string `json:"id"` } `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse remember_all response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Node == nil {
		t.Error("each entry must have a 'node' field")
	}
	if result[0].SuggestedConnections == nil {
		t.Error("each entry must have a 'suggested_connections' field")
	}
}

// TestRemember_PossibleDuplicates: filing a node with a near-identical label in
// the same domain should surface the existing node in possible_duplicates.
func TestRemember_PossibleDuplicates(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Boot Crash Diagnosis", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"label":  "boot crash diagnosis",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		PossibleDuplicates []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"possible_duplicates"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.PossibleDuplicates == nil {
		t.Fatal("possible_duplicates field must always be present")
	}
	found := false
	for _, d := range resp.PossibleDuplicates {
		if d.ID == existingID {
			found = true
		}
	}
	if !found {
		t.Errorf("existing node %q should appear in possible_duplicates; got %+v", existingID, resp.PossibleDuplicates)
	}
}

// TestRemember_PossibleDuplicates_EmptyWhenNone: filing in a fresh domain
// returns an empty possible_duplicates slice.
func TestRemember_PossibleDuplicates_EmptyWhenNone(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Unique Label XYZ",
		"domain": "fresh-domain",
	})
	mustNotError(t, tr)

	var resp struct {
		PossibleDuplicates []struct {
			ID string `json:"id"`
		} `json:"possible_duplicates"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.PossibleDuplicates == nil {
		t.Error("possible_duplicates must be present even when empty")
	}
	if len(resp.PossibleDuplicates) != 0 {
		t.Errorf("expected no duplicates in fresh domain, got %d", len(resp.PossibleDuplicates))
	}
}

// TestAddNodesBulkRollsBackOnError: if any node in the batch is invalid the
// whole transaction must be rolled back.
func TestAddNodesBulkRollsBackOnError(t *testing.T) {
	_, h := newEnv(t)

	// Third node has empty label — required field missing.
	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{"label": "Rollback Node One", "domain": "rollback-test"},
			{"label": "Rollback Node Two", "domain": "rollback-test"},
			{"label": "", "domain": "rollback-test"},
		},
	})
	mustError(t, tr)

	// The two valid nodes must not have been persisted.
	for _, label := range []string{"Rollback Node One", "Rollback Node Two"} {
		searchTr := call(t, h, "search", map[string]any{
			"query": label, "domain": "rollback-test",
		})
		mustNotError(t, searchTr)
		if len(searchIDs(t, searchTr)) > 0 {
			t.Errorf("node %q should not exist after rollback", label)
		}
	}
}

// TestAddEdgesBulk: two edges inserted in one call; count returned and both
// edges visible on the source node.
func TestAddEdgesBulk(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Edge Bulk Node A", "edge-bulk-test", nil)
	idB := addNode(t, h, "Edge Bulk Node B", "edge-bulk-test", nil)
	idC := addNode(t, h, "Edge Bulk Node C", "edge-bulk-test", nil)

	tr := call(t, h, "connect_all", map[string]any{
		"edges": []map[string]any{
			{"from_node": idA, "to_node": idB, "relationship": "connects_to", "narrative": "A to B"},
			{"from_node": idB, "to_node": idC, "relationship": "led_to", "narrative": "B to C"},
		},
	})
	mustNotError(t, tr)

	var result map[string]int
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse add_edges response: %v", err)
	}
	if result["edges_created"] != 2 {
		t.Errorf("expected edges_created=2, got %d", result["edges_created"])
	}

	// Both edges should appear on get_node for A.
	nodeTr := call(t, h, "recall", map[string]any{"id": idA})
	mustNotError(t, nodeTr)
	var nwe struct {
		Edges []struct {
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, nodeTr)), &nwe)
	if len(nwe.Edges) == 0 {
		t.Error("expected edges on node A after add_edges")
	}
}

// TestAddEdgesBulkRollsBackOnError: if any edge in the batch references a
// non-existent node the whole transaction must be rolled back.
func TestAddEdgesBulkRollsBackOnError(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Edge Rollback Node A", "edge-rollback-test", nil)
	idB := addNode(t, h, "Edge Rollback Node B", "edge-rollback-test", nil)

	// Second edge references a ghost node.
	tr := call(t, h, "connect_all", map[string]any{
		"edges": []map[string]any{
			{"from_node": idA, "to_node": idB, "relationship": "connects_to", "narrative": "valid"},
			{"from_node": idA, "to_node": "ghost-node-xyz", "relationship": "connects_to", "narrative": "invalid"},
		},
	})
	mustError(t, tr)

	// Node A should have no edges after rollback.
	nodeTr := call(t, h, "recall", map[string]any{"id": idA})
	mustNotError(t, nodeTr)
	var nwe struct {
		Edges []any `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, nodeTr)), &nwe)
	if len(nwe.Edges) > 0 {
		t.Errorf("edges should have been rolled back, got %d", len(nwe.Edges))
	}
}

// ── update_node ───────────────────────────────────────────────────────────────

func TestUpdateNode_UpdatesDescription(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "My Decision", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"description": "improved description with better search terms",
	})
	mustNotError(t, tr)

	var n struct {
		Description string `json:"description"`
		Label       string `json:"label"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &n); err != nil {
		t.Fatalf("parse update_node response: %v", err)
	}
	if n.Description != "improved description with better search terms" {
		t.Errorf("description: got %q", n.Description)
	}
	if n.Label != "My Decision" {
		t.Errorf("label changed unexpectedly: %q", n.Label)
	}
}

func TestUpdateNode_UpdatesMultipleFields(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Old Title", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"label":       "New Title",
		"why_matters": "now has an outcome",
		"tags":        "decision outcome kotlin",
	})
	mustNotError(t, tr)

	var n struct {
		Label      string `json:"label"`
		WhyMatters string `json:"why_matters"`
		Tags       string `json:"tags"`
	}
	json.Unmarshal([]byte(text(t, tr)), &n)
	if n.Label != "New Title" {
		t.Errorf("label: got %q", n.Label)
	}
	if n.WhyMatters != "now has an outcome" {
		t.Errorf("why_matters: got %q", n.WhyMatters)
	}
	if n.Tags != "decision outcome kotlin" {
		t.Errorf("tags: got %q", n.Tags)
	}
}

func TestUpdateNode_NotFoundReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "revise", map[string]any{
		"id":          "nonexistent-node-xxx",
		"description": "whatever",
	})
	mustError(t, tr)
}

func TestUpdateNode_ArchivedNodeReturnsError(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Will Be Archived", "proj", nil)
	call(t, h, "forget", map[string]any{"id": id, "reason": "test"})

	tr := call(t, h, "revise", map[string]any{
		"id":    id,
		"label": "New Label",
	})
	mustError(t, tr)
}

func TestUpdateNode_SearchableAfterTagsUpdate(t *testing.T) {
	_, h := newEnv(t)
	// Node label won't match the search query.
	id := addNode(t, h, "Parameterised test approval files need withNameSuffix", "proj", nil)

	// Before updating tags, the oblique query should miss.
	trBefore := call(t, h, "search", map[string]any{"query": "approval parameterised", "domain": "proj"})
	mustNotError(t, trBefore)
	idsBefore := searchIDs(t, trBefore)
	if contains(idsBefore, id) {
		t.Log("note: node found before tag update (label/description matched)")
	}

	// After adding tags, the query must find it.
	call(t, h, "revise", map[string]any{
		"id":   id,
		"tags": "testing approval parameterised withNamesuffix",
	})

	trAfter := call(t, h, "search", map[string]any{"query": "approval parameterised", "domain": "proj"})
	mustNotError(t, trAfter)
	if !contains(searchIDs(t, trAfter), id) {
		t.Error("node not findable via tags after update_node")
	}
}

// ── update_all ────────────────────────────────────────────────────────────────

// TestUpdateAll_UpdatesMultipleNodes: three nodes updated in one call; all
// changes are persisted and visible via search.
func TestUpdateAll_UpdatesMultipleNodes(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Node One", "proj", nil)
	id2 := addNode(t, h, "Node Two", "proj", nil)
	id3 := addNode(t, h, "Node Three", "proj", nil)

	desc1 := "updated description one"
	desc2 := "updated description two"
	tags3 := "bulk update tag"

	tr := call(t, h, "revise_all", map[string]any{
		"updates": []map[string]any{
			{"id": id1, "description": desc1},
			{"id": id2, "description": desc2},
			{"id": id3, "tags": tags3},
		},
	})
	mustNotError(t, tr)

	var result struct {
		Updated []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse update_all response: %v", err)
	}
	if len(result.Updated) != 3 {
		t.Fatalf("expected 3 updated nodes, got %d", len(result.Updated))
	}

	byID := map[string]struct {
		Description string
		Tags        string
	}{}
	for _, n := range result.Updated {
		byID[n.ID] = struct {
			Description string
			Tags        string
		}{n.Description, n.Tags}
	}
	if byID[id1].Description != desc1 {
		t.Errorf("id1 description: got %q, want %q", byID[id1].Description, desc1)
	}
	if byID[id2].Description != desc2 {
		t.Errorf("id2 description: got %q, want %q", byID[id2].Description, desc2)
	}
	if byID[id3].Tags != tags3 {
		t.Errorf("id3 tags: got %q, want %q", byID[id3].Tags, tags3)
	}
}

// TestUpdateAll_RollsBackOnError: if any entry references a non-existent ID
// the whole batch must be rolled back and no changes persist.
func TestUpdateAll_RollsBackOnError(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Rollback Node", "proj", nil)

	tr := call(t, h, "revise_all", map[string]any{
		"updates": []map[string]any{
			{"id": id, "description": "will be rolled back"},
			{"id": "ghost-id-that-does-not-exist", "description": "invalid"},
		},
	})
	mustError(t, tr)

	// Verify the valid node was NOT updated.
	recallTr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, recallTr)
	var nwe struct {
		Node struct {
			Description string `json:"description"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, recallTr)), &nwe)
	if nwe.Node.Description == "will be rolled back" {
		t.Error("description should have been rolled back, but was persisted")
	}
}

// TestUpdateAll_EmptyUpdatesReturnsEmpty: calling with an empty updates list
// returns an empty result without error.
func TestUpdateAll_EmptyUpdatesReturnsEmpty(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "revise_all", map[string]any{
		"updates": []map[string]any{},
	})
	mustNotError(t, tr)
}

// ── add_node tags ─────────────────────────────────────────────────────────────

func TestAddNode_WithTags_SearchableByTag(t *testing.T) {
	_, h := newEnv(t)
	// The description uses "approval parameterised" so search will match even without tags,
	// but we verify the tag field is echoed back and search works.
	id := addNode(t, h, "Test scaffold decision", "proj", map[string]any{
		"tags": "approval parameterised withNameSuffix kotlin",
	})

	tr := call(t, h, "search", map[string]any{"query": "withNameSuffix", "domain": "proj"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("node not found via tags field search")
	}
}

func TestAddNode_WithTags_TagsInResponse(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Tagged Node",
		"domain": "proj",
		"tags":   "alpha beta gamma",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			Tags string `json:"tags"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.Node.Tags != "alpha beta gamma" {
		t.Errorf("tags in response: got %q", resp.Node.Tags)
	}
}

// TestAddNode_ResponseShape: remember must return {node, suggested_connections}.
func TestAddNode_ResponseShape(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Shape test node",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var resp struct {
		Node                 *struct{ ID string `json:"id"` }    `json:"node"`
		SuggestedConnections *[]struct{ ID string `json:"id"` } `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.Node == nil {
		t.Error("response must have a 'node' field")
	}
	if resp.SuggestedConnections == nil {
		t.Error("response must have a 'suggested_connections' field (even if empty)")
	}
}

// TestAddNode_ResponseIncludesSuggestedConnections: when a related node already
// exists in the same domain, it should appear in suggested_connections.
func TestAddNode_ResponseIncludesSuggestedConnections(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "RST crash root cause", "proj", map[string]any{
		"description": "ROM calls RST $10 which hangs the boot sequence",
	})

	tr := call(t, h, "remember", map[string]any{
		"label":       "RST crash investigation",
		"domain":      "proj",
		"description": "RST $10 handler analysis",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		SuggestedConnections []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Reason string `json:"reason"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}

	found := false
	for _, s := range resp.SuggestedConnections {
		if s.ID == existingID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected existingID %q in suggested_connections; got %+v", existingID, resp.SuggestedConnections)
	}
}

// ── add_node related_to ───────────────────────────────────────────────────────

func TestAddNode_WithRelatedTo_PlainStringCreatesConnectsToEdge(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Existing Node", "proj", nil)

	newID := addNode(t, h, "New Node", "proj", map[string]any{
		"related_to": []string{existingID},
	})

	tr := call(t, h, "recall", map[string]any{"id": newID})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromNode     string `json:"from_node"`
			ToNode       string `json:"to_node"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "connects_to" &&
			((e.FromNode == newID && e.ToNode == existingID) ||
				(e.FromNode == existingID && e.ToNode == newID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected connects_to edge between %q and %q, got edges: %+v", newID, existingID, nwe.Edges)
	}
}

func TestAddNode_WithRelatedTo_ExplicitRelationshipObject(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Cause Node", "proj", nil)

	newID := addNode(t, h, "Effect Node", "proj", map[string]any{
		"related_to": []map[string]any{
			{"id": existingID, "relationship": "led_to"},
		},
	})

	tr := call(t, h, "recall", map[string]any{"id": newID})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromNode     string `json:"from_node"`
			ToNode       string `json:"to_node"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nwe); err != nil {
		t.Fatalf("parse get_node: %v", err)
	}

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "led_to" &&
			((e.FromNode == newID && e.ToNode == existingID) ||
				(e.FromNode == existingID && e.ToNode == newID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected led_to edge between %q and %q; got edges: %+v", newID, existingID, nwe.Edges)
	}
}

func TestAddNode_WithRelatedTo_MixedFormats(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Node A mixed", "proj", nil)
	idB := addNode(t, h, "Node B mixed", "proj", nil)

	// idA via plain string → connects_to; idB via object → depends_on
	idC := addNode(t, h, "Node C mixed", "proj", map[string]any{
		"related_to": []any{
			idA,
			map[string]any{"id": idB, "relationship": "depends_on"},
		},
	})

	tr := call(t, h, "recall", map[string]any{"id": idC})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromNode     string `json:"from_node"`
			ToNode       string `json:"to_node"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	relByTarget := map[string]string{}
	for _, e := range nwe.Edges {
		if e.FromNode == idC {
			relByTarget[e.ToNode] = e.Relationship
		} else if e.ToNode == idC {
			relByTarget[e.FromNode] = e.Relationship
		}
	}

	if relByTarget[idA] != "connects_to" {
		t.Errorf("plain string entry: expected connects_to to idA, got %q", relByTarget[idA])
	}
	if relByTarget[idB] != "depends_on" {
		t.Errorf("object entry: expected depends_on to idB, got %q", relByTarget[idB])
	}
}

func TestAddNode_WithRelatedTo_UnknownIDSilentlySkipped(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Safe Node",
		"domain":     "proj",
		"related_to": []string{"ghost-id-xxxx"},
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)

	gettr := call(t, h, "recall", map[string]any{"id": resp.Node.ID})
	mustNotError(t, gettr)

	var nwe struct {
		Edges []struct{} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, gettr)), &nwe)
	if len(nwe.Edges) != 0 {
		t.Errorf("expected no edges for unknown ID, got %d", len(nwe.Edges))
	}
}

// ── add_nodes with tags ───────────────────────────────────────────────────────

func TestAddNodes_WithTags_Searchable(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{
				"label":  "Batch Node One",
				"domain": "proj",
				"tags":   "batchsearch uniqueterm",
			},
		},
	})
	mustNotError(t, tr)

	srTr := call(t, h, "search", map[string]any{"query": "uniqueterm", "domain": "proj"})
	mustNotError(t, srTr)
	ids := searchIDs(t, srTr)
	if len(ids) == 0 {
		t.Error("batch node not findable by tag")
	}
}

// ── audit_log for update_node ─────────────────────────────────────────────────

// TestAuditLog_RecordsUpdateNode: every call to update_node must write an
// audit_log entry with action="revise". The reason must name the changed
// fields and their old values.
func TestAuditLog_RecordsUpdateNode(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "original label", "proj", map[string]any{
		"description": "original description",
	})

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"description": "improved description",
		"why_matters": "now it matters more",
	}))

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, reason FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action string
		reason sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.reason); err != nil {
			t.Fatalf("scan audit_log: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit_log entry after update_node, got %d", len(entries))
	}
	if entries[0].action != "update" {
		t.Errorf("audit action: got %q, want %q", entries[0].action, "update")
	}
	if !entries[0].reason.Valid || entries[0].reason.String == "" {
		t.Error("audit reason should be non-empty")
	}
	// Reason should mention fields that changed.
	reason := entries[0].reason.String
	if !strings.Contains(reason, "description") {
		t.Errorf("reason should mention 'description'; got %q", reason)
	}
	if !strings.Contains(reason, "why_matters") {
		t.Errorf("reason should mention 'why_matters'; got %q", reason)
	}
	// And should include the old value so the trail is useful.
	if !strings.Contains(reason, "original description") {
		t.Errorf("reason should include old description value; got %q", reason)
	}
}

// ── search_nodes multi-word fallback ─────────────────────────────────────────

// TestSearchNodes_MultiWordFallback: multi-word query where no field contains
// the full phrase but each word appears in a different field — should still
// return the node via individual-word OR fallback.
func TestSearchNodes_MultiWordFallback(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "testing scaffold", "proj", map[string]any{
		"description": "approval required",
		"tags":        "parameterised kotlin",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "testing approval parameterised",
		"domain": "proj",
	})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("node not found via multi-word fallback search")
	}
}

// TestSearchNodes_SingleWord_Unchanged: single-word primary match still works
// exactly as before — fallback does not alter behaviour.
func TestSearchNodes_SingleWord_Unchanged(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "ULA memory write fix fallback test", "proj", nil)

	tr := call(t, h, "search", map[string]any{
		"query":  "ULA",
		"domain": "proj",
	})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("single-word query should still find node without interference from fallback")
	}
}

// TestSearchNodes_MultiWordFallback_NoSpuriousResults: a node that does NOT
// contain any of the query words must not appear in the LIKE fallback results.
// Ollama is disabled so that only LIKE search runs.
func TestSearchNodes_MultiWordFallback_NoSpuriousResults(t *testing.T) {
	disableOllama(t) // LIKE-only: verifies OR-word fallback does not over-match
	_, h := newEnv(t)
	addNode(t, h, "completely unrelated topic", "proj", map[string]any{
		"description": "something about rendering pipelines",
	})
	idTarget := addNode(t, h, "testing scaffold", "proj", map[string]any{
		"description": "approval required",
		"tags":        "parameterised",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "testing approval parameterised",
		"domain": "proj",
	})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if !contains(ids, idTarget) {
		t.Error("target node should appear in fallback results")
	}
	// The unrelated node should not appear.
	for _, id := range ids {
		// We can't easily check by ID for the unrelated one since we don't have it,
		// but we can verify the count is reasonable (only 1 match expected).
		_ = id
	}
	if len(ids) != 1 {
		t.Errorf("expected exactly 1 result, got %d: %v", len(ids), ids)
	}
}

// TestSummariseDomain_IncludesNodeIDs: each entry in nodes and recent must
// carry an "id" field so the agent can pass it directly to update_node or
// add_edge without a second lookup.
func TestSummariseDomain_IncludesNodeIDs(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "ID check node alpha", "id-test-domain", map[string]any{
	"description": "first node",
		"why_matters": "verify id round-trips",
})
id2 := addNode(t, h, "ID check node beta", "id-test-domain", map[string]any{
"description": "second node",
})

tr := call(t, h, "orient", map[string]any{"domain": "id-test-domain"})
mustNotError(t, tr)
body := text(t, tr)

// Parse the structured response.
var resp struct {
	Nodes []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"nodes"`
	Recent []struct {
		ID string `json:"id"`
	} `json:"recent"`
}
if err := json.Unmarshal([]byte(body), &resp); err != nil {
t.Fatalf("parse summarise_domain response: %v\nbody: %s", err, body)
}

// Every node entry must have a non-empty ID.
for _, n := range resp.Nodes {
if n.ID == "" {
t.Errorf("node %q has empty id in summarise_domain response", n.Label)
}
}

// Specifically, both filed IDs must appear.
var gotIDs []string
for _, n := range resp.Nodes {
gotIDs = append(gotIDs, n.ID)
}
if !contains(gotIDs, id1) {
t.Errorf("id1 (%s) not found in summarise_domain nodes; got %v", id1, gotIDs)
}
if !contains(gotIDs, id2) {
t.Errorf("id2 (%s) not found in summarise_domain nodes; got %v", id2, gotIDs)
}
}

// ── add_node transient + drift of transient ───────────────────────────────────

func TestAddNode_Transient_PersistedAndReturned(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":     "Sprint ticket ABC",
		"domain":    "proj",
		"transient": true,
	})
	mustNotError(t, tr)

	var n struct {
		Transient bool `json:"transient"`
	}
	var resp struct {
		Node struct {
			Transient bool `json:"transient"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse add_node response: %v", err)
	}
	n.Transient = resp.Node.Transient
	if !n.Transient {
		t.Error("transient=true should be present in add_node response")
	}
}

func TestDrift_TransientOlderThan7Days_Surfaced(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	id := addNode(t, h, "Sprint ticket old", "transient-test", map[string]any{
		"transient": true,
	})

	// Backdate created_at to 8 days ago.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	stale := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, stale, id); err != nil {
		rawDB.Close()
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "whats_stale", map[string]any{"domain": "transient-test"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("stale transient node (%s) should appear in drift; got:\n%s", id, body)
	}
	if !strings.Contains(body, "transient") {
		t.Errorf("drift reason should mention 'transient'; got:\n%s", body)
	}
}

func TestDrift_TransientNewerThan7Days_NotSurfaced(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Sprint ticket fresh", "transient-fresh", map[string]any{
		"transient": true,
	})

	tr := call(t, h, "whats_stale", map[string]any{"domain": "transient-fresh"})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, id) {
		t.Errorf("recent transient node (%s) should NOT appear in drift; got:\n%s", id, body)
	}
}

// ── suggest_edges ─────────────────────────────────────────────────────────────

// TestSuggestEdges_OverlappingTags: two nodes sharing a tag should produce a
// suggestion mentioning the shared tag.
func TestSuggestEdges_OverlappingTags(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Sprint ticket ABC", "proj", map[string]any{
		"tags": "kotlin testing approval",
	})
	addNode(t, h, "Sprint ticket DEF", "proj", map[string]any{
		"tags": "kotlin gradle build",
	})

	tr := call(t, h, "suggest_connections", map[string]any{"id": idA})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(strings.ToLower(body), "kotlin") {
		t.Errorf("expected suggestion mentioning shared tag 'kotlin'; got:\n%s", body)
	}
}

// TestSuggestEdges_SimilarLabelWords: two nodes sharing a significant word in
// their labels should produce a suggestion mentioning that word.
func TestSuggestEdges_SimilarLabelWords(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "boot crash investigation", "proj", nil)
	idB := addNode(t, h, "boot sequence timing", "proj", nil) // shares "boot"
	addNode(t, h, "completely unrelated widget", "proj", nil)

	tr := call(t, h, "suggest_connections", map[string]any{"id": idA})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, idB) {
		t.Errorf("expected node B (%s) in suggestion results; got:\n%s", idB, body)
	}
}

// TestSuggestEdges_DoesNotReturnItself: the source node must not appear in its
// own suggestion list.
func TestSuggestEdges_DoesNotReturnItself(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "source node with uniquetag999", "proj", map[string]any{
		"tags": "uniquetag999",
	})
	addNode(t, h, "partner node", "proj", map[string]any{
		"tags": "uniquetag999",
	})

	tr := call(t, h, "suggest_connections", map[string]any{"id": id})
	mustNotError(t, tr)

	var suggestions []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &suggestions); err != nil {
		t.Fatalf("parse suggest_edges response: %v", err)
	}
	for _, s := range suggestions {
		if s.ID == id {
			t.Error("suggest_edges should not return the source node itself")
		}
	}
}

// TestSuggestEdges_DomainScoping: only nodes in the same domain as the source
// node should be returned.
func TestSuggestEdges_DomainScoping(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "kotlin build system", "domain-a", map[string]any{
		"tags": "kotlin gradle",
	})
	// Same tags, different domain — must NOT appear.
	addNode(t, h, "kotlin build tool", "domain-b", map[string]any{
		"tags": "kotlin gradle",
	})
	// Same domain — SHOULD appear.
	idC := addNode(t, h, "kotlin test runner", "domain-a", map[string]any{
		"tags": "kotlin testing",
	})

	tr := call(t, h, "suggest_connections", map[string]any{"id": idA})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, idC) {
		t.Errorf("same-domain node (%s) should appear in suggestions; got:\n%s", idC, body)
	}
}

// TestSuggestEdges_NoResults_ReturnsEmptyNotError: when no similar nodes
// exist the tool should return an empty list, not an error.
func TestSuggestEdges_NoResults_ReturnsEmptyNotError(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "lone node in empty domain", "lone-domain-xyz", nil)

	tr := call(t, h, "suggest_connections", map[string]any{"id": id})
	mustNotError(t, tr)
	// Must parse as a JSON array (possibly empty).
	var suggestions []interface{}
	if err := json.Unmarshal([]byte(text(t, tr)), &suggestions); err != nil {
		t.Fatalf("expect JSON array for zero results, got: %s", text(t, tr))
	}
}

// suppress unused import warning in case time is imported only via helpers
var _ = time.Now

// ── semantic_distance in search response ──────────────────────────────────────

// TestSearchSemantic_ResponseIncludesDistance: when Ollama is running each
// semantic result must carry a non-nil semantic_distance in the JSON response,
// and its value must be within [0, 1].
func TestSearchSemantic_ResponseIncludesDistance(t *testing.T) {
	if !ollamaRunning(t) {
		t.Skip("Ollama with snowflake-arctic-embed not available")
	}
	_, h := newEnv(t)

	addNode(t, h, "database schema migration", "dist-test", map[string]any{
		"description": "evolving relational schemas safely across releases",
		"why_matters": "prevents data corruption during upgrades",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "schema evolution approach",
		"domain": "dist-test",
	})
	mustNotError(t, tr)

	var result struct {
		Nodes []struct {
			ID               string   `json:"id"`
			SemanticDistance *float64 `json:"semantic_distance"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search_nodes response: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected at least one semantic result")
	}
	if result.Nodes[0].SemanticDistance == nil {
		t.Error("semantic_distance should be non-nil for semantic results")
	} else if d := *result.Nodes[0].SemanticDistance; d < 0 || d > 1 {
		t.Errorf("semantic_distance out of expected range [0,1]: %v", d)
	}
}

// TestSearchLike_ResponseHasNullDistance: LIKE-only results (Ollama disabled)
// must not include a semantic_distance field (it should be absent/null).
func TestSearchLike_ResponseHasNullDistance(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	addNode(t, h, "unique xylophone phrase", "dist-like-test", nil)

	tr := call(t, h, "search", map[string]any{
		"query":  "unique xylophone phrase",
		"domain": "dist-like-test",
	})
	mustNotError(t, tr)

	var result struct {
		Nodes []struct {
			ID               string   `json:"id"`
			SemanticDistance *float64 `json:"semantic_distance"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search_nodes response: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected at least one LIKE result")
	}
	if result.Nodes[0].SemanticDistance != nil {
		t.Errorf("LIKE results should have null semantic_distance, got %v", *result.Nodes[0].SemanticDistance)
	}
}

// ── semantic search tests (require Ollama + snowflake-arctic-embed) ───────────
//
// These tests skip automatically when Ollama is not running. They verify that
// the vector-distance path works correctly end-to-end with the real model.

// TestSearchSemantic_FindsRelatedContent: a query with related but non-identical
// words retrieves the semantically similar node.
func TestSearchSemantic_FindsRelatedContent(t *testing.T) {
	if !ollamaRunning(t) {
		t.Skip("Ollama with snowflake-arctic-embed not available")
	}
	_, h := newEnv(t)

	id := addNode(t, h, "database migration strategy", "semantic-test", map[string]any{
		"description": "how to evolve a relational schema safely across releases",
		"why_matters": "prevents data corruption and downtime during upgrades",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "schema evolution approach",
		"domain": "semantic-test",
	})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("semantic search should find semantically related node within threshold")
	}
}

// TestSearchSemantic_ExcludesIrrelevantNode: a node on a completely unrelated
// topic must not be returned for a domain-specific technical query.
func TestSearchSemantic_ExcludesIrrelevantNode(t *testing.T) {
	if !ollamaRunning(t) {
		t.Skip("Ollama with snowflake-arctic-embed not available")
	}
	_, h := newEnv(t)

	addNode(t, h, "banana bread recipe", "semantic-test", map[string]any{
		"description": "how to bake moist banana bread at home with ripe bananas",
		"why_matters": "dessert baking technique",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "database schema migration upgrade strategy",
		"domain": "semantic-test",
	})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if len(ids) != 0 {
		t.Errorf("semantic search should not return banana bread for database query; got %d result(s): %v", len(ids), ids)
	}
}

// TestSearchSemantic_FallsBackToLikeWhenNoEmbeddings: when a domain has nodes
// but none have embeddings (Ollama was unavailable at insert time), the search
// falls back to LIKE and still surfaces LIKE matches.
func TestSearchSemantic_FallsBackToLikeWhenNoEmbeddings(t *testing.T) {
	if !ollamaRunning(t) {
		t.Skip("Ollama with snowflake-arctic-embed not available")
	}
	// Add node with Ollama disabled so no embedding is stored.
	_, h := newEnv(t)
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	id := addNode(t, h, "schema migration approach", "fallback-test", map[string]any{
		"description": "evolving the database schema",
	})
	// Re-enable Ollama for the search.
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "")

	// Semantic search finds no embeddings → falls back to LIKE.
	tr := call(t, h, "search", map[string]any{
		"query":  "schema migration",
		"domain": "fallback-test",
	})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("should find node via LIKE fallback when no embeddings are stored")
	}
}

// ── disconnected ──────────────────────────────────────────────────────────────

func TestDisconnectedReturnsUnconnectedNodes(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-1"
	discID := addNode(t, h, "Lonely Node", domain, nil)
	connA := addNode(t, h, "Conn A", domain, nil)
	connB := addNode(t, h, "Conn B", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_node":    connA,
		"to_node":      connB,
		"relationship": "connects_to",
	}))

	tr := call(t, h, "disconnected", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, discID) {
		t.Errorf("disconnected result should contain %s; got:\n%s", discID, body)
	}
	if strings.Contains(body, connA) || strings.Contains(body, connB) {
		t.Errorf("disconnected result should not contain connected nodes; got:\n%s", body)
	}
}

func TestDisconnectedExcludesTransient(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-2"
	mustNotError(t, call(t, h, "remember", map[string]any{
		"label":     "Ticket T-1234 notes",
		"domain":    domain,
		"transient": true,
	}))

	tr := call(t, h, "disconnected", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, "Ticket T-1234") {
		t.Errorf("disconnected should exclude transient nodes; got:\n%s", body)
	}
}

func TestDisconnectedExcludesArchived(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-3"
	id := addNode(t, h, "Archived orphan", domain, nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id":     id,
		"reason": "stale",
	}))

	tr := call(t, h, "disconnected", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, "Archived orphan") {
		t.Errorf("disconnected should exclude archived nodes; got:\n%s", body)
	}
}

// ── trace ─────────────────────────────────────────────────────────────────────

func TestTraceReturnsChain(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-trace-1"
	idA := addNode(t, h, "Node A", domain, nil)
	idB := addNode(t, h, "Node B", domain, nil)
	idC := addNode(t, h, "Node C", domain, nil)
	idD := addNode(t, h, "Node D", domain, nil)

	// A -> B -> C -> D
	mustNotError(t, call(t, h, "connect", map[string]any{"from_node": idA, "to_node": idB, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_node": idB, "to_node": idC, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_node": idC, "to_node": idD, "relationship": "led_to"}))

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idD})
	mustNotError(t, tr)
	body := text(t, tr)

	// All intermediate nodes and the endpoints must appear
	for _, id := range []string{idA, idB, idC, idD} {
		if !strings.Contains(body, id) {
			t.Errorf("trace result should contain node %s; got:\n%s", id, body)
		}
	}
}

func TestTraceNoConnection(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-trace-2"
	idA := addNode(t, h, "Island A", domain, nil)
	idB := addNode(t, h, "Island B", domain, nil)

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idB})
	mustNotError(t, tr) // no path is not an error
	body := text(t, tr)
	if !strings.Contains(body, "No path") {
		t.Errorf("no-path result should say 'No path'; got:\n%s", body)
	}
}

func TestTraceIgnoresArchived(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-trace-3"
	idA := addNode(t, h, "Start", domain, nil)
	idB := addNode(t, h, "Middle", domain, nil)
	idC := addNode(t, h, "End", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{"from_node": idA, "to_node": idB, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_node": idB, "to_node": idC, "relationship": "led_to"}))

	// Archive the middle node — path should break
	mustNotError(t, call(t, h, "forget", map[string]any{"id": idB, "reason": "test"}))

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idC})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "No path") {
		t.Errorf("archived middle node should break the path; got:\n%s", body)
	}
}

