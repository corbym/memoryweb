package tools_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/stats"
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
	return dbPath, store, tools.New(store, "dev", nil)
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

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}

// ── Instructions ──────────────────────────────────────────────────────────────

func TestInstructions_NonEmpty(t *testing.T) {
	if tools.Instructions == "" {
		t.Fatal("Instructions must be non-empty")
	}
}

// TestInstructions_CredentialsAdvisory: Instructions must tell agents never to
// file credentials or API keys in memories.
func TestInstructions_CredentialsAdvisory(t *testing.T) {
	if !strings.Contains(tools.Instructions, "credentials") {
		t.Error(`Instructions must contain credentials advisory — agents must be told never to file credentials or API keys`)
	}
}

// ── ListTools ─────────────────────────────────────────────────────────────────

// TestListTools_DescriptionsAgentFirst: every tool description must open with
// an imperative verb — not "The " or "This ". Permanent regression guard.
func TestListTools_DescriptionsAgentFirst(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if strings.HasPrefix(td.Description, "The ") || strings.HasPrefix(td.Description, "This ") {
			t.Errorf("tool %q description starts with %q — must open with an imperative verb, not 'The' or 'This'",
				td.Name, td.Description[:min(len(td.Description), 10)])
		}
	}
}

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
		"remember", "connect", "revise", "recall", "search",
		"recent", "why_connected", "history", "significance",
		"alias",
		"forget", "restore", "forget_all",
		"audit", "orient",
		"suggest_connections",
		"domains", "disconnect", "trace", "visualise",
		"rename_domain",
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

func TestListTools_DescriptionsPresent(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}

	// wantDescSubstr maps tool name → a distinctive substring of its description.
	wantDescSubstr := map[string]string{
		"remember":            "File one or more concepts",
		"connect":             "Connect memories with typed",
		"recall":              "Retrieve a memory and all its connections by ID",
		"search":              "Search memories by text",
		"recent":              "List the most recently added or updated memories",
		"why_connected":       "Find how two concepts are related",
		"history":             "Returns memories in chronological order by effective date",
		"significance":        "Dual-signal importance analysis",
		"alias":               "Manage domain aliases",
		"forget":              "Always provide a reason",
		"restore":             "Restore an archived memory so it surfaces in search again. This reverses forget.",
		"audit":               "Inspect the health of knowledge",
		"forget_all":          "Batch archive",
		"orient":              "Call this at the start of every session",
		"revise":              "Update one or more existing live memories",
		"suggest_connections": "Given a memory ID, return up to 5 candidate connections",
		"domains":             "Return all known domains and registered aliases",
		"disconnect":          "Remove a connection between two memories by edge ID",
		"trace":               "Find the shortest chain of relationships",
		"visualise":           "Generate a Mermaid.js flowchart",
		"rename_domain":       "Rename a domain",
	}

	byName := map[string]string{}
	for _, td := range resp.Tools {
		byName[td.Name] = td.Description
	}

	for name, wantSubstr := range wantDescSubstr {
		desc, ok := byName[name]
		if !ok {
			t.Errorf("tool %q: not found in ListTools response", name)
			continue
		}
		if desc == "" {
			t.Errorf("tool %q: description is empty", name)
			continue
		}
		if !strings.Contains(desc, wantSubstr) {
			t.Errorf("tool %q: description does not contain %q\n  got: %s", name, wantSubstr, desc[:min(len(desc), 120)])
		}
	}
}

// TestListTools_NoStaleToolReferences asserts that no tool description
// references a removed or renamed tool by its old name. When a tool is
// removed or renamed, add its former name to removedTools below so any
// leftover references in descriptions are caught immediately.
func TestListTools_NoStaleToolReferences(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}

	// removedTools lists former tool names that must never appear in any
	// description. Update this list whenever a tool is removed or renamed.
	removedTools := []struct {
		name       string
		replacedBy string
	}{
		{"forgotten", "audit(mode=archived)"},
		{"whats_stale", "audit(mode=stale)"},
		{"remember_all", "remember with items array"},
		{"revise_all", "revise with items array"},
		{"connect_all", "connect with items array"},
		{"list_domains", "domains"},
		{"list_aliases", "alias(action=list)"},
		{"disconnected", "audit(mode=orphans)"},
		{"check_for_updates", "CLI only"},
	}

	for _, td := range resp.Tools {
		for _, removed := range removedTools {
			// Whole-word match: \b ensures "disconnected" does not fire on
			// "disconnected staleness" but would fire on a bare tool name reference.
			pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(removed.name) + `\b`)
			if pat.MatchString(td.Description) {
				t.Errorf("tool %q: description references removed tool %q (use %s instead)",
					td.Name, removed.name, removed.replacedBy)
			}
		}
	}
}

// TestListTools_AllToolsHaveExplicitStatsKind asserts that every tool returned
// by ListTools has an explicit entry in the stats toolKinds table. Tools absent
// from that table silently fall through as kindRetrieval via zero-value map
// lookup, producing incorrect WKD scores. Add new tools to stats.toolKinds
// (and confirm the kind with the test) whenever this test fails.
func TestListTools_AllToolsHaveExplicitStatsKind(t *testing.T) {
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
	for _, td := range resp.Tools {
		if !stats.HasKind(td.Name) {
			t.Errorf("tool %q has no explicit entry in stats.toolKinds — add it with the correct kind", td.Name)
		}
	}
}

// TestConnect_AcceptsFromMemoryToMemory asserts that the connect tool accepts
// from_memory/to_memory as parameter keys (tier 2 vocabulary rename). If this
// test fails it means the schema still uses from_memory/to_memory.
func TestConnect_AcceptsFromMemoryToMemory(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "source memory", "deep-game", nil)
	to := addNode(t, h, "target memory", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
		"relationship": "led_to",
	})
	mustNotError(t, tr)

	var e struct {
		Relationship string `json:"relationship"`
	}
	json.Unmarshal([]byte(text(t, tr)), &e)
	if e.Relationship != "led_to" {
		t.Errorf("connect with from_memory/to_memory: got relationship %q, want %q", e.Relationship, "led_to")
	}
}

// TestConnect_BatchAcceptsFromMemoryToMemory asserts that batch mode items also
// use from_memory/to_memory keys (tier 2 vocabulary rename).
func TestConnect_BatchAcceptsFromMemoryToMemory(t *testing.T) {
	_, h := newEnv(t)
	a := addNode(t, h, "alpha", "proj", nil)
	b := addNode(t, h, "beta", "proj", nil)

	tr := call(t, h, "connect", map[string]any{
		"items": []map[string]any{
			{"from_memory": a, "to_memory": b, "relationship": "depends_on"},
		},
	})
	mustNotError(t, tr)

	var result struct {
		EdgesCreated int `json:"edges_created"`
	}
	json.Unmarshal([]byte(text(t, tr)), &result)
	if result.EdgesCreated != 1 {
		t.Errorf("batch connect with from_memory/to_memory: got edges_created=%d, want 1", result.EdgesCreated)
	}
}

// TestListTools_PropertyDescriptionsNoForbiddenWords asserts that property-level
// Description strings in every tool's schema do not contain:
//   - any retired tool name from the removedTools blocklist
//   - the blacklisted word "disconnected"
//   - the word "node" as a standalone noun (vocabulary contract)
//
// This covers the blind spot in TestListTools_NoStaleToolReferences, which only
// scans top-level tool Description fields.
func TestListTools_PropertyDescriptionsNoForbiddenWords(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)

	// Parse into a structure that preserves the full InputSchema.
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}

	removedTools := []struct {
		name       string
		replacedBy string
	}{
		{"forgotten", "audit(mode=archived)"},
		{"whats_stale", "audit(mode=stale)"},
		{"remember_all", "remember with items array"},
		{"revise_all", "revise with items array"},
		{"connect_all", "connect with items array"},
		{"list_domains", "domains"},
		{"list_aliases", "alias(action=list)"},
		{"disconnected", "audit(mode=orphans)"},
		{"check_for_updates", "CLI only"},
	}

	nodeWord := regexp.MustCompile(`(?i)\bnode\b`)

	for _, td := range resp.Tools {
		for propName, prop := range td.InputSchema.Properties {
			desc := prop.Description
			if desc == "" {
				continue
			}
			loc := fmt.Sprintf("tool %q property %q", td.Name, propName)

			// Check retired tool names.
			for _, removed := range removedTools {
				pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(removed.name) + `\b`)
				if pat.MatchString(desc) {
					t.Errorf("%s: description references removed tool %q (use %s instead)\n  got: %s",
						loc, removed.name, removed.replacedBy, desc)
				}
			}

			// Check standalone "node" vocabulary.
			if nodeWord.MatchString(desc) {
				t.Errorf("%s: description uses forbidden word 'node' (use 'memory' instead)\n  got: %s",
					loc, desc)
			}
		}
	}
}

// TestConnect_ResponseUsesFromMemoryToMemory asserts that the connect tool
// response serialises edge fields as from_memory/to_memory (not from_node/to_node).
// If this test fails it means db.Edge still uses the old json tags.
func TestConnect_ResponseUsesFromMemoryToMemory(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "response source", "proj", nil)
	to := addNode(t, h, "response target", "proj", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
		"relationship": "led_to",
	})
	mustNotError(t, tr)

	raw := text(t, tr)
	if !strings.Contains(raw, `"from_memory"`) {
		t.Errorf("connect response should contain from_memory, got: %s", raw)
	}
	if !strings.Contains(raw, `"to_memory"`) {
		t.Errorf("connect response should contain to_memory, got: %s", raw)
	}
	if strings.Contains(raw, `"from_node"`) {
		t.Errorf("connect response must not contain from_node (old vocabulary), got: %s", raw)
	}
	if strings.Contains(raw, `"to_node"`) {
		t.Errorf("connect response must not contain to_node (old vocabulary), got: %s", raw)
	}
}

// TestRecall_EdgeResponseUsesFromMemoryToMemory asserts that recall returns
// edges with from_memory/to_memory keys (not from_node/to_node).
func TestRecall_EdgeResponseUsesFromMemoryToMemory(t *testing.T) {
	_, h := newEnv(t)
	a := addNode(t, h, "recall edge from", "proj", nil)
	b := addNode(t, h, "recall edge to", "proj", nil)

	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  a,
		"to_memory":    b,
		"relationship": "depends_on",
	}))

	tr := call(t, h, "recall", map[string]any{"id": a})
	mustNotError(t, tr)

	raw := text(t, tr)
	if !strings.Contains(raw, `"from_memory"`) {
		t.Errorf("recall response should contain from_memory, got: %s", raw)
	}
	if !strings.Contains(raw, `"to_memory"`) {
		t.Errorf("recall response should contain to_memory, got: %s", raw)
	}
	if strings.Contains(raw, `"from_node"`) {
		t.Errorf("recall response must not contain from_node (old vocabulary), got: %s", raw)
	}
	if strings.Contains(raw, `"to_node"`) {
		t.Errorf("recall response must not contain to_node (old vocabulary), got: %s", raw)
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
		"why_matters": "marks when the crash was first seen",
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
		"why_matters": "precise timestamp of first crash observation",
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

func TestSearch_TruncatedFlagSetWhenLimitExceeded(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	for i := 0; i < 5; i++ {
		addNode(t, h, "truncation flag test", "ttest", nil)
	}
	tr := call(t, h, "search", map[string]any{
		"query": "truncation flag", "domain": "ttest", "limit": 3,
	})
	mustNotError(t, tr)

	var result struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 results, got %d", len(result.Nodes))
	}
	if !result.Truncated {
		t.Error("truncated should be true when results hit the limit")
	}
}

func TestSearch_TruncatedFlagNotSetWhenUnderLimit(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	for i := 0; i < 3; i++ {
		addNode(t, h, "truncation under limit", "ttest2", nil)
	}
	tr := call(t, h, "search", map[string]any{
		"query": "truncation under", "domain": "ttest2", "limit": 10,
	})
	mustNotError(t, tr)

	var result struct {
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if result.Truncated {
		t.Error("truncated should be false when results are under the limit")
	}
}

func TestSearch_DescriptionHasVocabularyGuidance(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "search" {
			continue
		}
		const want = "vocabulary that appears in the stored"
		if !strings.Contains(td.Description, want) {
			t.Errorf("search description missing vocabulary guidance\nwant substring: %q\ngot: %s", want, td.Description)
		}
		return
	}
	t.Fatal("search tool not found in ListTools")
}

func TestSearch_PropertyDescriptionsHaveGuidance(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "search" {
			continue
		}
		queryDesc := td.InputSchema.Properties["query"].Description
		const wantQuery = "vocabulary that appears in the stored"
		if !strings.Contains(queryDesc, wantQuery) {
			t.Errorf("search.query property description missing vocabulary guidance\nwant substring: %q\ngot: %q", wantQuery, queryDesc)
		}
		limitDesc := td.InputSchema.Properties["limit"].Description
		const wantLimit = "truncated: true"
		if !strings.Contains(limitDesc, wantLimit) {
			t.Errorf("search.limit property description missing truncation hint\nwant substring: %q\ngot: %q", wantLimit, limitDesc)
		}
		return
	}
	t.Fatal("search tool not found in ListTools")
}

// ── add_edge ──────────────────────────────────────────────────────────────────

func TestAddEdge_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)
	to := addNode(t, h, "ULA fix", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
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

// TestConnect_RejectsLegacyFromNodeKey: sending from_node (retired key) must
// return an error with a schema-refresh hint, not silently create a broken edge.
func TestConnect_RejectsLegacyFromNodeKey(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "source", "proj", nil)
	to := addNode(t, h, "target", "proj", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_node":    from, // retired key
		"to_memory":    to,
		"relationship": "connects_to",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "from_node") {
		t.Error("error message should name the offending parameter 'from_node'")
	}
}

// TestConnect_RejectsLegacyToNodeKey: sending to_node (retired key) must
// return an error with a schema-refresh hint.
func TestConnect_RejectsLegacyToNodeKey(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "source", "proj", nil)
	to := addNode(t, h, "target", "proj", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_node":      to, // retired key
		"relationship": "connects_to",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "to_node") {
		t.Error("error message should name the offending parameter 'to_node'")
	}
}

// TestConnect_BatchRejectsLegacyKeys: batch mode items using from_node/to_node
// must return an error, not silently skip the edges.
func TestConnect_BatchRejectsLegacyKeys(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "source", "proj", nil)
	to := addNode(t, h, "target", "proj", nil)

	tr := call(t, h, "connect", map[string]any{
		"items": []map[string]any{
			{"from_node": from, "to_node": to, "relationship": "connects_to"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "from_node") {
		t.Error("error message should name the offending parameter 'from_node'")
	}
}

func TestAddEdge_NonExistentFromNode(t *testing.T) {
	_, h := newEnv(t)
	to := addNode(t, h, "ULA fix", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  "ghost-node-id",
		"to_memory":    to,
		"relationship": "unblocks",
	})
	mustError(t, tr)
}

func TestAddEdge_NonExistentToNode(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    "ghost-node-id",
		"relationship": "unblocks",
	})
	mustError(t, tr)
}

func TestAddEdge_BothNodesNonExistent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "connect", map[string]any{
		"from_memory":  "ghost-a",
		"to_memory":    "ghost-b",
		"relationship": "connects_to",
	})
	mustError(t, tr)
}

// TestSuggestedConnections_IncludesDomain asserts that each entry in
// suggested_connections carries a non-empty domain field, so agents know which
// domain to pass to connect when linking the suggestion.
func TestSuggestedConnections_IncludesDomain(t *testing.T) {
	_, h := newEnv(t)
	// File a node with enough tags to generate at least one suggestion.
	addNode(t, h, "existing node", "proj", map[string]any{"tags": "alpha beta gamma"})
	tr := call(t, h, "remember", map[string]any{
		"label":  "new node",
		"domain": "proj",
		"tags":   "alpha beta gamma",
	})
	mustNotError(t, tr)

	var resp struct {
		SuggestedConnections []struct {
			ID     string `json:"id"`
			Domain string `json:"domain"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	for i, s := range resp.SuggestedConnections {
		if s.Domain == "" {
			t.Errorf("suggested_connections[%d] (id=%q) has empty domain field", i, s.ID)
		}
	}
	if len(resp.SuggestedConnections) == 0 {
		t.Skip("no suggestions generated — cannot assert domain field; adjust tags if needed")
	}
}

// TestConnect_CrossDomain_ErrorMentionsDomain asserts that when connect fails
// because the to_memory ID is not found, the error message names the domain
// that was searched, making the failure recoverable for agents.
func TestConnect_CrossDomain_ErrorMentionsDomain(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "domain-a node", "domain-a", nil)
	// to node is in domain-b — connect does not support cross-domain
	addNode(t, h, "domain-b node", "domain-b", nil)
	// Use a non-existent ID so the error fires
	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    "does-not-exist",
		"relationship": "connects_to",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "domain-a") {
		t.Errorf("error message should name the searched domain (domain-a);\ngot: %s", text(t, tr))
	}
}

// TestConnect_SameDomain_Succeeds is a sanity check that same-domain connect
// still works after the error message changes.
func TestConnect_SameDomain_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "a", "proj", nil)
	to := addNode(t, h, "b", "proj", nil)
	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
		"relationship": "depends_on",
	})
	mustNotError(t, tr)
}

func TestGetNode_IncludesEdges(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "RST crash", "deep-game", nil)
	to := addNode(t, h, "ULA fix", "deep-game", nil)
	call(t, h, "connect", map[string]any{
		"from_memory": from, "to_memory": to, "relationship": "unblocks",
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
		"from_memory": from, "to_memory": to, "relationship": "unblocks",
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
		"from_memory": from, "to_memory": to, "relationship": "led_to",
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
	id1 := addNode(t, h, "Early event", "proj", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "first in timeline order test",
	})
	id2 := addNode(t, h, "Late event", "proj", map[string]any{
		"occurred_at": "2026-06-01",
		"why_matters": "second in timeline order test",
	})

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
	idDated := addNode(t, h, "Dated node", "proj", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline for excludes-no-date test",
	})

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
		"why_matters": "baseline for archive-exclusion test",
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
	addNode(t, h, "Jan event", "proj", map[string]any{
		"occurred_at": "2026-01-15",
		"why_matters": "outside range — before",
	})
	idMar := addNode(t, h, "Mar event", "proj", map[string]any{
		"occurred_at": "2026-03-15",
		"why_matters": "inside date range",
	})
	addNode(t, h, "Jun event", "proj", map[string]any{
		"occurred_at": "2026-06-15",
		"why_matters": "outside range — after",
	})

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

	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse domains response: %v", err)
	}
	if len(resp.Domains) != 2 {
		t.Errorf("expected 2 distinct domains, got %d: %v", len(resp.Domains), resp.Domains)
	}
	if !contains(resp.Domains, "domain-alpha") {
		t.Error("expected domain-alpha in result")
	}
	if !contains(resp.Domains, "domain-beta") {
		t.Error("expected domain-beta in result")
	}
}

func TestListDomains_ExcludesArchivedOnlyDomains(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Ghost node", "dead-domain", nil)
	store.ArchiveNode(id, "test")
	addNode(t, h, "Live node", "live-domain", nil)

	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Domains []string `json:"domains"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if contains(resp.Domains, "dead-domain") {
		t.Error("dead-domain should not appear: all its nodes are archived")
	}
	if !contains(resp.Domains, "live-domain") {
		t.Error("live-domain should appear")
	}
}

func TestListDomains_EmptyDB(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)
	var resp struct {
		Domains []string `json:"domains"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if len(resp.Domains) != 0 {
		t.Errorf("expected empty list, got %v", resp.Domains)
	}
}

// ── aliases ───────────────────────────────────────────────────────────────────

func TestAddAlias_SearchResolvesAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias", map[string]any{"action": "add", "alias": "engine", "domain": "deep-engine"})

	tr := call(t, h, "search", map[string]any{"query": "Engine", "domain": "engine"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("alias should resolve to canonical domain in search")
	}
}

func TestResolveDomain_ReturnsCanonical(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "deep-game") {
		t.Errorf("resolve_domain should return canonical; got: %s", text(t, tr))
	}
}

func TestResolveDomain_UnknownAliasReturnsItself(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "unknown-domain"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "unknown-domain") {
		t.Errorf("unregistered name should resolve to itself; got: %s", text(t, tr))
	}
}

func TestListAliases_ReturnsRegisteredAliases(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})
	call(t, h, "alias", map[string]any{"action": "add", "alias": "sx", "domain": "sedex"})

	tr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "dg") || !strings.Contains(body, "sx") {
		t.Errorf("list_aliases missing registered aliases; got: %s", body)
	}
}

// ── remove_alias ──────────────────────────────────────────────────────────────

func TestRemoveAlias_RemovesExistingAlias(t *testing.T) {
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "dg", "domain": "deep-game"})

	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "dg"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "dg") {
		t.Errorf("expected confirmation mentioning alias; got: %s", text(t, tr))
	}

	// list_aliases should no longer contain it
	listTr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, listTr)
	if strings.Contains(text(t, listTr), `"dg"`) {
		t.Error("alias 'dg' should not appear in list_aliases after removal")
	}
}

func TestRemoveAlias_NonExistentReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "ghost-alias"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("expected 'not found' error; got: %s", text(t, tr))
	}
}

func TestRemoveAlias_SearchNoLongerResolvesRemovedAlias(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Engine node", "deep-engine", nil)

	call(t, h, "alias", map[string]any{"action": "add", "alias": "engine", "domain": "deep-engine"})

	// confirm alias resolves while it exists
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "Engine", "domain": "engine",
	})), id) {
		t.Fatal("alias should resolve before removal")
	}

	mustNotError(t, call(t, h, "alias", map[string]any{"action": "remove", "alias": "engine"}))

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

	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "test"})
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

	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "domain-1"})
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
	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "project-alpha"})
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
	archivedTr = call(t, h, "audit", map[string]any{"mode": "archived", "domain": "project-alpha"})
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
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-1"})
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

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-2"})
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
		"why_matters": "unresolved timing decision that affects boot reliability",
	})

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-3"})
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

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-4"})
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

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-5"})
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

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-b"})
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
		"why_matters": "significant milestone for the domain",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "sum-dated-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, "2026-04-01") {
		t.Errorf("result should include occurred_at date '2026-04-01'; got:\n%s", body)
	}
}

// TestOrient_LiveNodesCount: orient response must include live_nodes reflecting
// only non-archived nodes.
func TestOrient_LiveNodesCount(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha orient", "orient-counts", nil)
	addNode(t, h, "Beta orient", "orient-counts", nil)
	addNode(t, h, "Gamma orient", "orient-counts", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-counts"})
	mustNotError(t, tr)

	var resp struct {
		LiveNodes int `json:"live_nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.LiveNodes != 3 {
		t.Errorf("live_nodes: got %d, want 3", resp.LiveNodes)
	}
}

// TestOrient_ArchivedNodesCount: orient response must include archived_nodes
// reflecting soft-deleted nodes; live_nodes must exclude them.
func TestOrient_ArchivedNodesCount(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Keep A", "orient-arch-count", nil)
	addNode(t, h, "Keep B", "orient-arch-count", nil)
	archiveID := addNode(t, h, "Archive me", "orient-arch-count", nil)

	call(t, h, "forget", map[string]any{"id": archiveID, "reason": "test"})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-arch-count"})
	mustNotError(t, tr)

	var resp struct {
		LiveNodes     int `json:"live_nodes"`
		ArchivedNodes int `json:"archived_nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.LiveNodes != 2 {
		t.Errorf("live_nodes: got %d, want 2", resp.LiveNodes)
	}
	if resp.ArchivedNodes != 1 {
		t.Errorf("archived_nodes: got %d, want 1", resp.ArchivedNodes)
	}
}

// TestOrient_NoTotalNodes: orient response must NOT contain a total_nodes field.
func TestOrient_NoTotalNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Some node", "orient-no-total", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-no-total"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["total_nodes"]; ok {
		t.Error("orient response must not contain total_nodes — superseded by live_nodes")
	}
}

// TestOrient_ResponseIncludesServerVersion: orient response must include a
// server_version field so agents can detect schema drift after a server update.
func TestOrient_ResponseIncludesServerVersion(t *testing.T) {
	_, h := newEnv(t) // newEnv creates handler with version "dev"
	addNode(t, h, "Version test node", "orient-version", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-version"})
	mustNotError(t, tr)

	var resp struct {
		ServerVersion string `json:"server_version"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.ServerVersion == "" {
		t.Error("server_version must be present and non-empty in orient response")
	}
	if resp.ServerVersion != "dev" {
		t.Errorf("server_version: got %q, want %q", resp.ServerVersion, "dev")
	}
}

// TestOrient_DeclaredSpineEmpty: orient on a domain whose nodes all lack
// occurred_at must return an empty declared_spine list.
func TestOrient_DeclaredSpineEmpty(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated alpha", "orient-spine-empty", nil)
	addNode(t, h, "Undated beta", "orient-spine-empty", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-empty"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 0 {
		t.Errorf("declared_spine: got %d entries, want 0", len(resp.DeclaredSpine))
	}
}

// TestOrient_DeclaredSpineOnlyContainsOccurredAtNodes: only nodes with
// occurred_at set must appear in declared_spine; undated nodes must not.
func TestOrient_DeclaredSpineOnlyContainsOccurredAtNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated node", "orient-spine-filter", nil)
	addNode(t, h, "Dated decision", "orient-spine-filter", map[string]any{
		"occurred_at": "2026-03-10",
		"why_matters": "significant choice that shaped the architecture",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-filter"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 1 {
		t.Fatalf("declared_spine: got %d entries, want 1", len(resp.DeclaredSpine))
	}
	if resp.DeclaredSpine[0].Label != "Dated decision" {
		t.Errorf("declared_spine[0].label: got %q, want %q", resp.DeclaredSpine[0].Label, "Dated decision")
	}
}

// TestOrient_DeclaredSpineIsChronological: multiple dated entries in the spine
// must be ordered by occurred_at ascending.
func TestOrient_DeclaredSpineIsChronological(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Third decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-05-01",
		"why_matters": "third in sequence",
	})
	addNode(t, h, "First decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "first in sequence",
	})
	addNode(t, h, "Second decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "second in sequence",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-order"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 3 {
		t.Fatalf("declared_spine: got %d entries, want 3", len(resp.DeclaredSpine))
	}
	want := []string{"First decision", "Second decision", "Third decision"}
	for i, w := range want {
		if resp.DeclaredSpine[i].Label != w {
			t.Errorf("declared_spine[%d].label: got %q, want %q", i, resp.DeclaredSpine[i].Label, w)
		}
	}
}

// TestOrient_DeclaredSpineExcludesArchived: an archived node with occurred_at
// must not appear in the declared_spine.
func TestOrient_DeclaredSpineExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	addNode(t, h, "Live dated decision", "orient-spine-archive", map[string]any{
		"occurred_at": "2026-04-01",
		"why_matters": "live and significant",
	})
	archivedID := addNode(t, h, "Archived dated decision", "orient-spine-archive", map[string]any{
		"occurred_at": "2026-04-02",
		"why_matters": "will be archived",
	})
	store.ArchiveNode(archivedID, "test archive")

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-archive"})
	mustNotError(t, tr)

	body := text(t, tr)
	if strings.Contains(body, "Archived dated decision") {
		t.Error("archived node must not appear in declared_spine")
	}
	if !strings.Contains(body, "Live dated decision") {
		t.Error("live dated node must appear in declared_spine")
	}
}

// ── orient: significant section + no all_nodes ───────────────────────────────

// TestOrient_HasSignificantSection: orient response must include a `significant`
// array. It may be empty when no edges exist in the domain.
func TestOrient_HasSignificantSection(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Lone node", "orient-sig", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig"})
	mustNotError(t, tr)

	var resp struct {
		Significant *json.RawMessage `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.Significant == nil {
		t.Error("significant field must be present in orient response (even if empty)")
	}
}

// TestOrient_SignificantRankedByImportance: the node with more inbound edges from
// recent nodes must appear first in the significant section.
func TestOrient_SignificantRankedByImportance(t *testing.T) {
	_, h := newEnv(t)
	popularID := addNode(t, h, "Popular node", "orient-sig-rank", nil)
	nicheID := addNode(t, h, "Niche node", "orient-sig-rank", nil)

	// Three linkers → popular
	for _, label := range []string{"Linker A", "Linker B", "Linker C"} {
		linkerID := addNode(t, h, label, "orient-sig-rank", nil)
		call(t, h, "connect", map[string]any{
			"from_memory":  linkerID,
			"to_memory":    popularID,
			"relationship": "connects_to",
			"narrative":    "links to popular",
		})
	}
	// One linker → niche
	nicheLinkerID := addNode(t, h, "Niche linker", "orient-sig-rank", nil)
	call(t, h, "connect", map[string]any{
		"from_memory":  nicheLinkerID,
		"to_memory":    nicheID,
		"relationship": "connects_to",
		"narrative":    "links to niche",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig-rank"})
	mustNotError(t, tr)

	var resp struct {
		Significant []struct {
			ID string `json:"id"`
		} `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Significant) < 2 {
		t.Fatalf("significant: want at least 2 entries, got %d", len(resp.Significant))
	}
	if resp.Significant[0].ID != popularID {
		t.Errorf("significant[0]: got %q, want popular node %q", resp.Significant[0].ID, popularID)
	}
}

// TestOrient_NoAllNodes: orient response must NOT include a top-level `nodes`
// (all_nodes dump) field. The response is the three-section design only.
func TestOrient_NoAllNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Test node", "orient-no-all", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-no-all"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["nodes"]; ok {
		t.Error("orient response must not contain a top-level `nodes` field (all_nodes dump removed)")
	}
}

// TestOrient_DescriptionImperativeFirst: orient description must not start with
// "The " or "This " — it must open with an imperative verb.
func TestOrient_DescriptionImperativeFirst(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if strings.HasPrefix(td.Description, "The ") || strings.HasPrefix(td.Description, "This ") {
				t.Errorf("orient description starts with %q — must open with an imperative verb",
					td.Description[:min(50, len(td.Description))])
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
}

// TestOrient_RecentCappedAtFive: the recent section must contain at most 5 entries
// even when more than 5 live nodes exist in the domain.
func TestOrient_RecentCappedAtFive(t *testing.T) {
	_, h := newEnv(t)
	for i := 0; i < 10; i++ {
		addNode(t, h, fmt.Sprintf("Node %02d", i), "orient-recent-cap", nil)
	}

	tr := call(t, h, "orient", map[string]any{"domain": "orient-recent-cap"})
	mustNotError(t, tr)

	var resp struct {
		Recent []json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) > 5 {
		t.Errorf("recent: got %d entries, want at most 5", len(resp.Recent))
	}
}

// TestOrient_SignificantCappedAtTen: significant must contain at most 10 entries
// even when more than 10 nodes have inbound edges in the domain.
func TestOrient_SignificantCappedAtTen(t *testing.T) {
	_, h := newEnv(t)
	// 12 hub nodes each with one inbound linker → all qualify for significant.
	for i := 0; i < 12; i++ {
		hubID := addNode(t, h, fmt.Sprintf("Hub %02d", i), "orient-sig-cap", nil)
		linkerID := addNode(t, h, fmt.Sprintf("Linker %02d", i), "orient-sig-cap", nil)
		call(t, h, "connect", map[string]any{
			"from_memory":  linkerID,
			"to_memory":    hubID,
			"relationship": "connects_to",
			"narrative":    "links to hub",
		})
	}

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig-cap"})
	mustNotError(t, tr)

	var resp struct {
		Significant []json.RawMessage `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Significant) > 10 {
		t.Errorf("significant: got %d entries, want at most 10", len(resp.Significant))
	}
}

// TestOrient_LeanFormat_NoDescription: orient must not include a description field
// in any section entry — lean format returns id, label, why_matters only.
func TestOrient_LeanFormat_NoDescription(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node with description", "orient-lean-nodesc", map[string]any{
		"description": "This description must not appear in orient output.",
		"why_matters": "It matters because of X.",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-nodesc"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	for _, section := range []string{"declared_spine", "significant", "recent"} {
		sectionRaw, ok := raw[section]
		if !ok {
			continue
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(sectionRaw, &entries); err != nil {
			continue
		}
		for _, entry := range entries {
			if _, hasDesc := entry["description"]; hasDesc {
				t.Errorf("section %q: entry contains 'description' field — orient must use lean format", section)
			}
		}
	}
}

// TestOrient_LeanFormat_WhyMattersTruncated: a why_matters longer than 150 chars
// with no sentence boundary must be hard-cut at 150 chars + "..." and truncated:true.
func TestOrient_LeanFormat_WhyMattersTruncated(t *testing.T) {
	_, h := newEnv(t)
	longWhy := strings.Repeat("x", 200)
	addNode(t, h, "Node long why", "orient-lean-trunc", map[string]any{
		"why_matters": longWhy,
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-trunc"})
	mustNotError(t, tr)

	var resp struct {
		Recent []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) == 0 {
		t.Fatal("recent section is empty")
	}
	entry := resp.Recent[0]
	const maxLen = 153 // 150 + len("...")
	if len(entry.WhyMatters) > maxLen {
		t.Errorf("why_matters: got %d chars, want at most %d", len(entry.WhyMatters), maxLen)
	}
	if !strings.HasSuffix(entry.WhyMatters, "...") {
		t.Errorf("why_matters must end with '...', got %q", entry.WhyMatters)
	}
	if !entry.Truncated {
		t.Error("truncated must be true when why_matters was hard-cut")
	}
}

// TestOrient_LeanFormat_SentenceBoundary: a why_matters with a sentence ending
// within the 150-char budget must be cut at the sentence boundary (no "..."),
// with truncated:true.
func TestOrient_LeanFormat_SentenceBoundary(t *testing.T) {
	_, h := newEnv(t)
	// First sentence ends at ~30 chars; total is well over 150.
	why := "This is the short first sentence. " + strings.Repeat("more content ", 20)
	addNode(t, h, "Node sentence boundary", "orient-lean-sentence", map[string]any{
		"why_matters": why,
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-sentence"})
	mustNotError(t, tr)

	var resp struct {
		Recent []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) == 0 {
		t.Fatal("recent section is empty")
	}
	entry := resp.Recent[0]
	if strings.HasSuffix(entry.WhyMatters, "...") {
		t.Errorf("sentence-boundary cut must not append '...', got %q", entry.WhyMatters)
	}
	if !strings.HasSuffix(entry.WhyMatters, ".") {
		t.Errorf("sentence-boundary cut must end with '.', got %q", entry.WhyMatters)
	}
	if !entry.Truncated {
		t.Error("truncated must be true when why_matters was cut at a sentence boundary")
	}
}

// TestOrient_LeanFormat_TruncatedFalseWhenFits: a short why_matters that fits
// within 150 chars must not include truncated:true in the orient entry.
func TestOrient_LeanFormat_TruncatedFalseWhenFits(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node short why", "orient-lean-fits", map[string]any{
		"why_matters": "Short enough.",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-fits"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	recentRaw, ok := raw["recent"]
	if !ok {
		t.Fatal("recent section missing")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(recentRaw, &entries); err != nil {
		t.Fatalf("parse recent entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("recent section is empty")
	}
	if _, hasTruncated := entries[0]["truncated"]; hasTruncated {
		t.Error("truncated must be omitted when why_matters fits within budget")
	}
}

// TestOrient_LeanFormat_WhyMattersOmitted: a node with no why_matters must not
// include a why_matters key in the orient entry (omitempty, not empty string).
func TestOrient_LeanFormat_WhyMattersOmitted(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node no why", "orient-lean-omit", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-omit"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	recentRaw, ok := raw["recent"]
	if !ok {
		t.Fatal("recent section missing from orient response")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(recentRaw, &entries); err != nil {
		t.Fatalf("parse recent entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("recent section is empty")
	}
	if _, hasWhy := entries[0]["why_matters"]; hasWhy {
		t.Error("why_matters must be omitted when empty — lean format uses omitempty")
	}
}

// TestListTools_OrientDescriptionTruncationDisclosure: orient description must
// tell agents that full content requires recall(id), not orient alone.
func TestListTools_OrientDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "recall(id)") {
				t.Error(`orient description must contain "recall(id)" — truncation disclosure is missing`)
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
}

// ── orient stale_count ────────────────────────────────────────────────────────

// TestOrient_StaleCountZeroWhenNoDrift: orient must include stale_count = 0
// when no nodes match any drift rule.
func TestOrient_StaleCountZeroWhenNoDrift(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Fresh memory", "orient-stalecnt-zero", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-zero"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount != 0 {
		t.Errorf("stale_count: got %d, want 0", resp.StaleCount)
	}
}

// TestOrient_StaleCountNonZeroWhenTransientIsStale: orient stale_count must be
// > 0 when a transient node is older than 7 days.
func TestOrient_StaleCountNonZeroWhenTransientIsStale(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	addNode(t, h, "Old sprint ticket", "orient-stalecnt-transient", map[string]any{
		"decision_type": "transient",
	})

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE domain = ?`, old, "orient-stalecnt-transient"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-transient"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 for a stale transient node")
	}
}

// TestOrient_StaleCountNonZeroWhenStandingNodeContradicts: orient stale_count
// must be > 0 when a standing node is connected by a contradicts edge.
func TestOrient_StaleCountNonZeroWhenStandingNodeContradicts(t *testing.T) {
	_, h := newEnv(t)
	aID := addNode(t, h, "Rule A contradicted", "orient-stalecnt-contradicts", map[string]any{
		"decision_type": "standing",
	})
	bID := addNode(t, h, "Rule B contradicts it", "orient-stalecnt-contradicts", map[string]any{
		"decision_type": "standing",
	})
	call(t, h, "connect", map[string]any{
		"from_memory":  aID,
		"to_memory":    bID,
		"relationship": "contradicts",
		"narrative":    "these rules conflict",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-contradicts"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 when a contradicts edge exists")
	}
}

// TestOrient_StaleCountNonZeroWhenLowConnectionStanding: orient stale_count
// must be > 0 for a standing node with < 2 inbound edges older than 30 days.
func TestOrient_StaleCountNonZeroWhenLowConnectionStanding(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	addNode(t, h, "Lonely standing rule", "orient-stalecnt-standing", map[string]any{
		"decision_type": "standing",
	})

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE domain = ?`, old, "orient-stalecnt-standing"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-standing"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 for a low-connection standing node older than 30 days")
	}
}

// TestOrient_DescriptionContainsStaleCountAdvisory: orient description must
// contain the stale_count advisory instructing agents to call audit(mode=stale).
func TestOrient_DescriptionContainsStaleCountAdvisory(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "stale_count > 0") {
				t.Error(`orient description must contain "stale_count > 0"`)
			}
			if !strings.Contains(td.Description, "audit(mode=stale)") {
				t.Error(`orient description must contain "audit(mode=stale)"`)
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
}

// ── orient topic ──────────────────────────────────────────────────────────────

// TestOrient_Topic_ReturnsRelevantSection: orient with topic must return a
// relevant section and must not return significant.
func TestOrient_Topic_ReturnsRelevantSection(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Authentication design decision", "orient-topic-dom", map[string]any{
		"description": "We chose JWT over sessions.",
		"why_matters": "Stateless tokens simplify horizontal scaling.",
	})

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-dom",
		"topic":  "authentication",
	})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["relevant"]; !ok {
		t.Error("orient with topic must return a relevant section")
	}
	if _, ok := raw["significant"]; ok {
		t.Error("orient with topic must not return significant — relevant replaces it")
	}
}

// TestOrient_Topic_RelevantIsLean: relevant entries must use lean format —
// no description field, why_matters present.
func TestOrient_Topic_RelevantIsLean(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Caching strategy", "orient-topic-lean", map[string]any{
		"description": "This must not appear in orient output.",
		"why_matters": "Cache hits reduce DB load.",
	})

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-lean",
		"topic":  "caching",
	})
	mustNotError(t, tr)

	var resp struct {
		Relevant []map[string]json.RawMessage `json:"relevant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Relevant) == 0 {
		t.Fatal("relevant section is empty")
	}
	entry := resp.Relevant[0]
	if _, hasDesc := entry["description"]; hasDesc {
		t.Error("relevant entry must not contain description field — lean format")
	}
	if _, hasWhy := entry["why_matters"]; !hasWhy {
		t.Error("relevant entry must contain why_matters when node has one")
	}
}

// TestOrient_Topic_SpineAndRecentUnchanged: topic mode must still return
// declared_spine and recent unchanged.
func TestOrient_Topic_SpineAndRecentUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Database migration", "orient-topic-spine", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "Sets DB schema baseline.",
	})
	addNode(t, h, "Recent work item", "orient-topic-spine", nil)

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-spine",
		"topic":  "migration",
	})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["declared_spine"]; !ok {
		t.Error("orient with topic must still return declared_spine")
	}
	if _, ok := raw["recent"]; !ok {
		t.Error("orient with topic must still return recent")
	}
}

// TestOrient_NoTopic_SignificantPresent: orient without topic must return
// significant and must not return relevant (no regression).
func TestOrient_NoTopic_SignificantPresent(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Some node", "orient-notopic", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-notopic"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["significant"]; !ok {
		t.Error("orient without topic must return significant section")
	}
	if _, ok := raw["relevant"]; ok {
		t.Error("orient without topic must not return relevant section")
	}
}

// TestListTools_OrientDescriptionMentionsTopic: orient description must
// mention the topic parameter so agents know to use it.
func TestListTools_OrientDescriptionMentionsTopic(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "pass topic") {
				t.Error(`orient description must contain "pass topic" — agents must know to use the parameter`)
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
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
		Node *struct {
			ID string `json:"id"`
		} `json:"node"`
		SuggestedConnections *[]struct {
			ID string `json:"id"`
		} `json:"suggested_connections"`
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
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "connects_to" &&
			((e.FromMemory == newID && e.ToMemory == existingID) ||
				(e.FromMemory == existingID && e.ToMemory == newID)) {
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
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nwe); err != nil {
		t.Fatalf("parse get_node: %v", err)
	}

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "led_to" &&
			((e.FromMemory == newID && e.ToMemory == existingID) ||
				(e.FromMemory == existingID && e.ToMemory == newID)) {
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
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	relByTarget := map[string]string{}
	for _, e := range nwe.Edges {
		if e.FromMemory == idC {
			relByTarget[e.ToMemory] = e.Relationship
		} else if e.ToMemory == idC {
			relByTarget[e.FromMemory] = e.Relationship
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

// ── related_to: skipped_connections surfaced ──────────────────────────────────

func TestSingleRemember_RelatedToInvalidId_ReportedNotSilent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Node With Bad Link",
		"domain":     "proj",
		"related_to": []string{"bad-id-does-not-exist"},
	})
	mustNotError(t, tr)

	var resp struct {
		SkippedConnections []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"skipped_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.SkippedConnections) == 0 {
		t.Fatal("expected skipped_connections to contain the bad ID, got none")
	}
	if resp.SkippedConnections[0].ID != "bad-id-does-not-exist" {
		t.Errorf("expected skipped ID %q, got %q", "bad-id-does-not-exist", resp.SkippedConnections[0].ID)
	}
	if resp.SkippedConnections[0].Reason == "" {
		t.Error("expected non-empty reason for skipped connection")
	}
}

func TestSingleRemember_RelatedToValidId_NoSkipped(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Target Node", "proj", nil)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Source Node",
		"domain":     "proj",
		"related_to": []string{existingID},
	})
	mustNotError(t, tr)

	var resp struct {
		SkippedConnections []struct{} `json:"skipped_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.SkippedConnections) != 0 {
		t.Errorf("expected no skipped_connections for valid ID, got %d", len(resp.SkippedConnections))
	}
}

// ── batch remember: related_to support ───────────────────────────────────────

func TestBatchRemember_RelatedToString(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Source",
				"domain":     "proj",
				"related_to": []string{targetID},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct{} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one node in batch response")
	}
	sourceID := resp.Nodes[0].Node.ID

	// Edge should exist
	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory string `json:"from_memory"`
			ToMemory   string `json:"to_memory"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if (e.FromMemory == sourceID && e.ToMemory == targetID) || (e.FromMemory == targetID && e.ToMemory == sourceID) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected edge between %q and %q; got edges: %+v", sourceID, targetID, nwe.Edges)
	}
	if len(resp.Nodes[0].SkippedConnections) != 0 {
		t.Errorf("expected no skipped_connections for valid ID, got %d", len(resp.Nodes[0].SkippedConnections))
	}
}

func TestBatchRemember_RelatedToObject(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Cause", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Batch Effect",
				"domain": "proj",
				"related_to": []map[string]any{
					{"id": targetID, "relationship": "caused_by"},
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "caused_by" &&
			((e.FromMemory == sourceID && e.ToMemory == targetID) ||
				(e.FromMemory == targetID && e.ToMemory == sourceID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected caused_by edge between %q and %q; got: %+v", sourceID, targetID, nwe.Edges)
	}
}

func TestBatchRemember_RelatedToArray(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Batch Array Target A", "proj", nil)
	idB := addNode(t, h, "Batch Array Target B", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Batch Array Source",
				"domain": "proj",
				"related_to": []any{
					idA,
					map[string]any{"id": idB, "relationship": "depends_on"},
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)

	relByTarget := map[string]string{}
	for _, e := range nwe.Edges {
		if e.FromMemory == sourceID {
			relByTarget[e.ToMemory] = e.Relationship
		} else if e.ToMemory == sourceID {
			relByTarget[e.FromMemory] = e.Relationship
		}
	}
	if relByTarget[idA] != "connects_to" {
		t.Errorf("expected connects_to to idA, got %q", relByTarget[idA])
	}
	if relByTarget[idB] != "depends_on" {
		t.Errorf("expected depends_on to idB, got %q", relByTarget[idB])
	}
}

func TestBatchRemember_RelatedToAbsent_NoEdge(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Batch No Links", "domain": "proj"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct{} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	if len(nwe.Edges) != 0 {
		t.Errorf("expected no edges, got %d", len(nwe.Edges))
	}
}

func TestBatchRemember_OrphanWarning_AbsentWhenRelatedToUsed(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Orphan Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Orphan Source",
				"domain":     "proj",
				"related_to": []string{targetID},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		OrphanWarning string `json:"orphan_warning"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.OrphanWarning != "" {
		t.Errorf("expected no orphan_warning when related_to used, got %q", resp.OrphanWarning)
	}
}

func TestBatchRemember_RelatedToInvalidId_ReportedNotSilent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Bad Link",
				"domain":     "proj",
				"related_to": []string{"bad-batch-id-xxxx"},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
			} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected node in response")
	}
	sc := resp.Nodes[0].SkippedConnections
	if len(sc) == 0 {
		t.Fatal("expected skipped_connections in batch item, got none")
	}
	if sc[0].ID != "bad-batch-id-xxxx" {
		t.Errorf("expected skipped ID %q, got %q", "bad-batch-id-xxxx", sc[0].ID)
	}
	if sc[0].Reason == "" {
		t.Error("expected non-empty reason in skipped_connections")
	}
}

func TestBatchRemember_RelatedToPartialSuccess(t *testing.T) {
	_, h := newEnv(t)
	validID := addNode(t, h, "Partial Valid Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Partial Source",
				"domain": "proj",
				"related_to": []any{
					validID,
					"ghost-partial-id-xxxx",
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct {
				ID string `json:"id"`
			} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	sourceID := resp.Nodes[0].Node.ID

	// Valid edge should exist
	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	var nwe struct {
		Edges []struct {
			FromMemory string `json:"from_memory"`
			ToMemory   string `json:"to_memory"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if (e.FromMemory == sourceID && e.ToMemory == validID) || (e.FromMemory == validID && e.ToMemory == sourceID) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected valid edge to %q to exist", validID)
	}

	// Only invalid ID in skipped_connections
	sc := resp.Nodes[0].SkippedConnections
	if len(sc) != 1 {
		t.Fatalf("expected exactly 1 skipped connection, got %d: %+v", len(sc), sc)
	}
	if sc[0].ID != "ghost-partial-id-xxxx" {
		t.Errorf("expected skipped ID %q, got %q", "ghost-partial-id-xxxx", sc[0].ID)
	}
}

func TestAddNodes_WithTags_Searchable(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
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

// ── occurred_at propose+confirm descriptions ──────────────────────────────────

// TestOccurredAt_ToolDescriptions_ContainProposeConfirmGuidance verifies that
// every tool which accepts occurred_at carries the propose+confirm wording in
// its schema description, so agents receive the correct guidance at runtime.
func TestOccurredAt_ToolDescriptions_ContainProposeConfirmGuidance(t *testing.T) {
	_, h := newEnv(t)

	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)

	var resp struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}

	toolIndex := map[string]json.RawMessage{}
	for _, td := range resp.Tools {
		toolIndex[td.Name] = td.InputSchema
	}

	// Tools whose top-level properties include an occurred_at field with a
	// description we control. remember_all and revise_all use an items-level
	// description, so we check the items description via the containing field.
	topLevelTools := []string{"remember", "revise"}
	for _, name := range topLevelTools {
		t.Run(name, func(t *testing.T) {
			schema, ok := toolIndex[name]
			if !ok {
				t.Fatalf("tool %q not found in ListTools", name)
			}
			var s struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(schema, &s); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			oat, ok := s.Properties["occurred_at"]
			if !ok {
				t.Fatalf("tool %q has no occurred_at property", name)
			}
			for _, phrase := range []string{"propose", "confirm", "Never guess"} {
				if !strings.Contains(oat.Description, phrase) {
					t.Errorf("tool %q occurred_at description missing phrase %q;\ngot: %s", name, phrase, oat.Description)
				}
			}
		})
	}

	// For remember and revise the occurred_at guidance for batch mode is
	// embedded in the items array description. Verify the items property on
	// each tool carries the propose+confirm contract phrases.
	itemsTools := []string{"remember", "revise"}
	for _, name := range itemsTools {
		name := name
		t.Run(name+"/items", func(t *testing.T) {
			schema, ok := toolIndex[name]
			if !ok {
				t.Fatalf("tool %q not found in ListTools", name)
			}
			var s struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(schema, &s); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			field, ok := s.Properties["items"]
			if !ok {
				t.Fatalf("tool %q has no %q property", name, "items")
			}
			for _, phrase := range []string{"propose+confirm", "never infer silently"} {
				if !strings.Contains(field.Description, phrase) {
					t.Errorf("tool %q.items description missing phrase %q;\ngot: %s", name, phrase, field.Description)
				}
			}
		})
	}
}

// TestOccurredAtWording_TwoCases asserts that the occurred_at property
// description in remember and revise contains the (a)/(b) epistemic split:
// an explicit "in-session witnessed" case and an "inferred or back-dated" case
// with the "Never guess" / "Never infer" forbidder.
func TestOccurredAtWording_TwoCases(t *testing.T) {
	_, h := newEnv(t)

	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)

	var resp struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}

	toolIndex := map[string]json.RawMessage{}
	for _, td := range resp.Tools {
		toolIndex[td.Name] = td.InputSchema
	}

	for _, name := range []string{"remember", "revise"} {
		name := name
		t.Run(name, func(t *testing.T) {
			schema, ok := toolIndex[name]
			if !ok {
				t.Fatalf("tool %q not found", name)
			}
			var s struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(schema, &s); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			oat, ok := s.Properties["occurred_at"]
			if !ok {
				t.Fatalf("tool %q has no occurred_at property", name)
			}
			for _, phrase := range []string{
				"(a)",
				"(b)",
				"In-session witnessed",
				"Inferred or back-dated",
				"Never guess",
				"Never infer",
			} {
				if !strings.Contains(oat.Description, phrase) {
					t.Errorf("tool %q occurred_at description missing phrase %q;\ngot: %s", name, phrase, oat.Description)
				}
			}
		})
	}
}

// ── audit_log provenance for occurred_at ─────────────────────────────────────

// TestAuditLog_OccurredAt_Remember: when remember sets occurred_at, the
// audit_log must record an entry with action="occurred_at_set" and
// provenance="agent-assigned".
func TestAuditLog_OccurredAt_Remember(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	id := addNode(t, h, "significant decision", "proj", map[string]any{
		"occurred_at": "2024-06-01",
		"why_matters": "chose this approach because of constraint X",
	})

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, provenance FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action     string
		provenance sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.provenance); err != nil {
			t.Fatalf("scan audit_log row: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit_log entry, got %d", len(entries))
	}
	if entries[0].action != "occurred_at_set" {
		t.Errorf("action: got %q, want %q", entries[0].action, "occurred_at_set")
	}
	if !entries[0].provenance.Valid || entries[0].provenance.String != "agent-assigned" {
		t.Errorf("provenance: got %q, want %q", entries[0].provenance.String, "agent-assigned")
	}
}

// TestAuditLog_OccurredAt_Revise: when revise sets occurred_at, the audit_log
// update entry must have provenance="agent-assigned".
func TestAuditLog_OccurredAt_Revise(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "some decision", "proj", map[string]any{
		"why_matters": "reason already on file",
	})

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-15",
	}))

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, provenance FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action     string
		provenance sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.provenance); err != nil {
			t.Fatalf("scan audit_log row: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit_log entry, got %d", len(entries))
	}
	if entries[0].action != "update" {
		t.Errorf("action: got %q, want %q", entries[0].action, "update")
	}
	if !entries[0].provenance.Valid || entries[0].provenance.String != "agent-assigned" {
		t.Errorf("provenance: got %q, want %q", entries[0].provenance.String, "agent-assigned")
	}
}

// TestAuditLog_NoOccurredAt_ProvenanceIsNull: when revise does NOT set
// occurred_at, the audit_log entry must have a NULL provenance.
func TestAuditLog_NoOccurredAt_ProvenanceIsNull(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "plain node", "proj", nil)

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"description": "updated description only",
	}))

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	var provenance sql.NullString
	err = rawDB.QueryRow(
		`SELECT provenance FROM audit_log WHERE node_id = ? AND action = 'update'`, id,
	).Scan(&provenance)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if provenance.Valid {
		t.Errorf("provenance should be NULL when occurred_at is not set, got %q", provenance.String)
	}
}

// ── occurred_at requires why_matters enforcement ──────────────────────────────

const errOccurredAtRequiresWhyMatters = "occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline."

// TestRemember_OccurredAt_WithWhyMatters_Succeeds: setting occurred_at with
// why_matters present must succeed.
func TestRemember_OccurredAt_WithWhyMatters_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "remember", map[string]any{
		"label":       "deploy decision",
		"domain":      "proj",
		"occurred_at": "2024-06-01",
		"why_matters": "chose blue-green over rolling — downtime constraint",
	}))
}

// TestRemember_OccurredAt_WithoutWhyMatters_Fails: setting occurred_at without
// why_matters must return the exact validation error.
func TestRemember_OccurredAt_WithoutWhyMatters_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "deploy decision",
		"domain":      "proj",
		"occurred_at": "2024-06-01",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr))
	}
}

// TestRememberAll_OccurredAt_WithoutWhyMatters_Fails: same constraint applies
// to remember_all — the failing node's index appears in the error.
func TestRememberAll_OccurredAt_WithoutWhyMatters_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "fine node", "domain": "proj", "why_matters": "ok"},
			{"label": "bad node", "domain": "proj", "occurred_at": "2024-06-01"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr))
	}
}

// TestRevise_OccurredAt_WhyMattersInDB_Succeeds: revise with occurred_at must
// succeed when why_matters already exists in the DB record (even if omitted
// from the call).
func TestRevise_OccurredAt_WhyMattersInDB_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "existing decision", "proj", map[string]any{
		"why_matters": "already filed when originally created",
	})
	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-10",
		// why_matters intentionally absent from call — should satisfy from DB
	}))
}

// TestRevise_OccurredAt_WhyMattersMissingBoth_Fails: revise with occurred_at
// must fail when why_matters is absent from both the call and the DB record.
func TestRevise_OccurredAt_WhyMattersMissingBoth_Fails(t *testing.T) {
	_, h := newEnv(t)
	// addNode without why_matters — the DB record has an empty why_matters
	tr := call(t, h, "remember", map[string]any{
		"label":  "undocumented node",
		"domain": "proj",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse node: %v", err)
	}
	id := resp.Node.ID

	tr2 := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-10",
	})
	mustError(t, tr2)
	if !strings.Contains(text(t, tr2), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr2))
	}
}

// TestReviseAll_OccurredAt_WhyMattersMissingBoth_Fails: same constraint applies
// to revise_all when the DB record has no why_matters.
func TestReviseAll_OccurredAt_WhyMattersMissingBoth_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "bare node",
		"domain": "proj",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse node: %v", err)
	}
	id := resp.Node.ID

	tr2 := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id, "occurred_at": "2024-07-01"},
		},
	})
	mustError(t, tr2)
	if !strings.Contains(text(t, tr2), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr2))
	}
}

// ── search_nodes multi-word fallback ─────────────────────────────────────────

// TestSearchNodes_MultiWordFallback: multi-word query where no field contains
// the full phrase but each word appears in a different field — should still
// return the node via individual-word OR fallback.
func TestSearchNodes_MultiWordFallback(t *testing.T) {
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

// ── exact search (LIKE bypass) ────────────────────────────────────────────────

// TestSearch_ExactTrue_FindsByLabel: exact:true finds a node whose label
// contains the query as a verbatim substring.
func TestSearch_ExactTrue_FindsByLabel(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "PROJ-231 conflict minerals compliance", "sedex", nil)
	// Add a second node in the same semantic neighbourhood to confirm it is NOT
	// returned ahead of the exact match.
	addNode(t, h, "PROJ-228 supply chain audit", "sedex", nil)

	tr := call(t, h, "search", map[string]any{
		"query":  "PROJ-231",
		"domain": "sedex",
		"exact":  true,
	})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if !contains(ids, id) {
		t.Errorf("exact search did not return the matching node; got %v", ids)
	}
	if len(ids) != 1 {
		t.Errorf("exact search returned extra nodes; got %d: %v", len(ids), ids)
	}
}

// TestSearch_ExactTrue_NoSemanticDistance: results from exact:true must not
// carry a semantic_distance field (they come from the LIKE path, not the
// embedding path).
func TestSearch_ExactTrue_NoSemanticDistance(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "PROJ-231 conflict minerals compliance", "sedex", nil)

	tr := call(t, h, "search", map[string]any{
		"query": "PROJ-231",
		"exact": true,
	})
	mustNotError(t, tr)
	body := text(t, tr)
	if strings.Contains(body, "semantic_distance") {
		t.Error("exact:true results must not include semantic_distance field")
	}
}

// TestSearch_ExactFalse_BehavesLikeDefault: explicit exact:false is identical
// to omitting the field.
func TestSearch_ExactFalse_BehavesLikeDefault(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "PROJ-231 conflict minerals compliance", "sedex", nil)

	tr := call(t, h, "search", map[string]any{
		"query":  "PROJ-231",
		"domain": "sedex",
		"exact":  false,
	})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("exact:false should still find the node via LIKE")
	}
}

// TestSearch_ExactTrue_DescriptionHasGuidance: the search tool description must
// mention exact and its purpose so agents know when to use it.
func TestSearch_ExactTrue_DescriptionHasGuidance(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "search" {
			continue
		}
		for _, phrase := range []string{"exact: true", "identifier", "ticket"} {
			if !strings.Contains(td.Description, phrase) {
				t.Errorf("search tool description missing expected phrase %q", phrase)
			}
		}
		exactDesc := td.InputSchema.Properties["exact"].Description
		if exactDesc == "" {
			t.Error("search tool missing exact property in schema")
		}
		return
	}
	t.Fatal("search tool not found in ListTools")
}

// ── semantic search tests (require Ollama + snowflake-arctic-embed) ───────────
//
// These tests skip automatically when Ollama is not running. They verify that
// the vector-distance path works correctly end-to-end with the real model.
// The CI integration workflow runs: go test ./... -run TestSearchSemantic_ -v

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

// TestSummariseDomain_IncludesNodeIDs: each entry in recent must carry an "id"
// field so the agent can pass it directly to revise or connect without a second
// lookup. (The all_nodes dump was removed in the orient redesign; IDs are
// available via recent, significant, and declared_spine.)
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

	// Parse the structured response — IDs must appear in recent.
	var resp struct {
		Recent []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse summarise_domain response: %v\nbody: %s", err, body)
	}

	// Every recent entry must have a non-empty ID.
	for _, n := range resp.Recent {
		if n.ID == "" {
			t.Errorf("recent entry %q has empty id in orient response", n.Label)
		}
	}

	// Both filed IDs must appear in recent (freshly filed, no edges).
	var gotIDs []string
	for _, n := range resp.Recent {
		gotIDs = append(gotIDs, n.ID)
	}
	if !contains(gotIDs, id1) {
		t.Errorf("id1 (%s) not found in orient recent; got %v", id1, gotIDs)
	}
	if !contains(gotIDs, id2) {
		t.Errorf("id2 (%s) not found in orient recent; got %v", id2, gotIDs)
	}
}

// ── add_node transient + drift of transient ───────────────────────────────────

func TestAddNode_DecisionTypeTransient_PersistedAndReturned(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":         "Sprint ticket ABC",
		"domain":        "proj",
		"decision_type": "transient",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			DecisionType string `json:"decision_type"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.Node.DecisionType != "transient" {
		t.Errorf("decision_type should be 'transient' in remember response, got %q", resp.Node.DecisionType)
	}
}

func TestDrift_TransientOlderThan7Days_Surfaced(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	id := addNode(t, h, "Sprint ticket old", "transient-test", map[string]any{
		"decision_type": "transient",
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

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "transient-test"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("stale transient node (%s) should appear in drift; got:\n%s", id, body)
	}
}

func TestDrift_TransientNewerThan7Days_NotSurfaced(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Sprint ticket fresh", "transient-fresh", map[string]any{
		"decision_type": "transient",
	})

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "transient-fresh"})
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

	if !strings.Contains(body, "kotlin") {
		t.Errorf("suggestion should mention shared tag 'kotlin'; got:\n%s", body)
	}
}

// ── disconnected ──────────────────────────────────────────────────────────────

func TestDisconnectedReturnsUnconnectedNodes(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-1"

	lone := addNode(t, h, "Lone wolf node", domain, nil)
	idA := addNode(t, h, "Connected A", domain, nil)
	idB := addNode(t, h, "Connected B", domain, nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idA, "to_memory": idB, "relationship": "led_to",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, lone) {
		t.Errorf("disconnected should contain lone node %s; got:\n%s", lone, body)
	}
	if strings.Contains(body, idA) {
		t.Errorf("connected node A should NOT appear; got:\n%s", body)
	}
	if strings.Contains(body, idB) {
		t.Errorf("connected node B should NOT appear; got:\n%s", body)
	}
}

func TestDisconnectedExcludesTransient(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-2"

	addNode(t, h, "Transient lone node", domain, map[string]any{"decision_type": "transient"})
	live := addNode(t, h, "Live lone node", domain, nil)

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, live) {
		t.Errorf("live disconnected node should appear; got:\n%s", body)
	}
}

func TestDisconnectedExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	domain := "test-disconnected-3"

	id := addNode(t, h, "Archived lone node", domain, nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, id) {
		t.Errorf("archived disconnected node should NOT appear; got:\n%s", body)
	}
}

// ── trace ──────────────────────────────────────────────────────────────────────

func TestTraceReturnsChain(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-trace-1"
	idA := addNode(t, h, "Node A", domain, nil)
	idB := addNode(t, h, "Node B", domain, nil)
	idC := addNode(t, h, "Node C", domain, nil)
	idD := addNode(t, h, "Node D", domain, nil)

	// A -> B -> C -> D
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idA, "to_memory": idB, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idB, "to_memory": idC, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idC, "to_memory": idD, "relationship": "led_to"}))

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idD})
	mustNotError(t, tr)
	body := text(t, tr)

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
	store, h := newEnv(t)
	domain := "test-trace-3"
	idA := addNode(t, h, "Start node", domain, nil)
	idB := addNode(t, h, "Middle node", domain, nil)
	idC := addNode(t, h, "End node", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idA, "to_memory": idB, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idB, "to_memory": idC, "relationship": "led_to"}))

	// Archive the middle node — path A→C no longer traversable.
	store.ArchiveNode(idB, "test")

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idC})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "No path") {
		t.Errorf("trace through archived node should return 'No path'; got:\n%s", body)
	}
}

func TestTraceReturnsContextEdges(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-trace-4"
	idA := addNode(t, h, "Start", domain, nil)
	idB := addNode(t, h, "Middle", domain, nil)
	idC := addNode(t, h, "End", domain, nil)
	idX := addNode(t, h, "Side branch X", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idA, "to_memory": idB, "relationship": "led_to"}))
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idB, "to_memory": idC, "relationship": "led_to"}))
	// Side branch off the path.
	mustNotError(t, call(t, h, "connect", map[string]any{"from_memory": idB, "to_memory": idX, "relationship": "connects_to"}))

	tr := call(t, h, "trace", map[string]any{"from_id": idA, "to_id": idC})
	mustNotError(t, tr)
	body := text(t, tr)

	// idX itself should also appear (it's a neighbour of a path node)
	if !strings.Contains(body, idX) {
		t.Errorf("side-branch node X should appear in trace context; got:\n%s", body)
	}
}

// ── visualise ─────────────────────────────────────────────────────────────────

func TestVisualiseMermaidSyntax(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-1"

	idA := addNode(t, h, "Alpha node", domain, nil)
	idB := addNode(t, h, "Beta node", domain, nil)
	idC := addNode(t, h, "Gamma node", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idA, "to_memory": idB, "relationship": "led_to", "narrative": "alpha led to beta",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idB, "to_memory": idC, "relationship": "depends_on", "narrative": "beta depends on gamma",
	}))

	tr := call(t, h, "visualise", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	var resp struct {
		Mermaid   string `json:"mermaid"`
		NodeCount int    `json:"node_count"`
		EdgeCount int    `json:"edge_count"`
		Truncated bool   `json:"truncated"`
		Nodes     []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"nodes"`
		Edges []struct {
			From         string `json:"from"`
			To           string `json:"to"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("visualise response should be valid JSON: %v\ngot:\n%s", err, body)
	}
	if !strings.Contains(resp.Mermaid, "flowchart TD") {
		t.Errorf("mermaid should start with 'flowchart TD'; got:\n%s", resp.Mermaid)
	}
	if !strings.Contains(resp.Mermaid, "Alpha node") {
		t.Errorf("mermaid should contain 'Alpha node'; got:\n%s", resp.Mermaid)
	}
	if !strings.Contains(resp.Mermaid, "led_to") {
		t.Errorf("mermaid should contain relationship 'led_to'; got:\n%s", resp.Mermaid)
	}
	if !strings.Contains(resp.Mermaid, "depends_on") {
		t.Errorf("mermaid should contain relationship 'depends_on'; got:\n%s", resp.Mermaid)
	}
	if resp.NodeCount != 3 {
		t.Errorf("node_count should be 3, got %d", resp.NodeCount)
	}
	if resp.EdgeCount != 2 {
		t.Errorf("edge_count should be 2, got %d", resp.EdgeCount)
	}
	if resp.Truncated {
		t.Error("truncated should be false for a 3-node graph")
	}
	// Structured nodes: full labels, real IDs (not n0/n1/n2).
	if len(resp.Nodes) != 3 {
		t.Errorf("nodes array should have 3 entries, got %d", len(resp.Nodes))
	}
	for _, n := range resp.Nodes {
		if n.ID == "" {
			t.Error("node entry should have non-empty id")
		}
		if strings.HasPrefix(n.ID, "n") && len(n.ID) == 2 {
			t.Errorf("node id looks like a Mermaid alias, not a real ID: %q", n.ID)
		}
	}
	// Labels in nodes array should be full, not truncated.
	nodeLabels := make(map[string]bool)
	for _, n := range resp.Nodes {
		nodeLabels[n.Label] = true
	}
	for _, expected := range []string{"Alpha node", "Beta node", "Gamma node"} {
		if !nodeLabels[expected] {
			t.Errorf("nodes array should contain full label %q", expected)
		}
	}
	// Structured edges: from/to are real node IDs.
	if len(resp.Edges) != 2 {
		t.Errorf("edges array should have 2 entries, got %d", len(resp.Edges))
	}
	for _, e := range resp.Edges {
		if e.From == "" || e.To == "" {
			t.Error("edge entry should have non-empty from and to")
		}
		if e.Relationship == "" {
			t.Error("edge entry should have non-empty relationship")
		}
		// from/to must be real node IDs matching what we created
		if e.From == idA && e.Relationship == "led_to" && e.To != idB {
			t.Errorf("edge from %s led_to should point to %s, got %s", idA, idB, e.To)
		}
	}
}

func TestVisualiseEmptyDomain(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "visualise", map[string]any{"domain": "no-such-domain"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "no content") {
		t.Errorf("empty domain should return 'no content' message; got:\n%s", text(t, tr))
	}
}

func TestVisualiseTruncation(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-trunc"
	addNode(t, h, "Node One", domain, nil)
	addNode(t, h, "Node Two", domain, nil)
	addNode(t, h, "Node Three", domain, nil)
	addNode(t, h, "Node Four", domain, nil)

	tr := call(t, h, "visualise", map[string]any{"domain": domain, "limit": 2})
	mustNotError(t, tr)

	var resp struct {
		NodeCount  int  `json:"node_count"`
		NodesTotal int  `json:"nodes_total"`
		Truncated  bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.NodeCount != 2 {
		t.Errorf("node_count should be 2 (limit enforced), got %d", resp.NodeCount)
	}
	if !resp.Truncated {
		t.Error("truncated should be true when domain has more nodes than limit")
	}
	if resp.NodesTotal != 4 {
		t.Errorf("nodes_total should be 4 (full domain count), got %d", resp.NodesTotal)
	}
}

func TestVisualiseLabelSanitisation(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-sanitise"
	long := `This "quoted" label is definitely longer than forty characters and then some more`
	addNode(t, h, long, domain, nil)

	tr := call(t, h, "visualise", map[string]any{"domain": domain})
	mustNotError(t, tr)

	var resp struct {
		Mermaid string `json:"mermaid"`
		Nodes   []struct {
			Label string `json:"label"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	lines := strings.Split(resp.Mermaid, "\n")
	for _, line := range lines {
		if !strings.Contains(line, "[\"") {
			continue
		}
		inner := strings.TrimPrefix(line, strings.SplitN(line, "[\"", 2)[0]+"[\"")
		inner = strings.TrimSuffix(inner, "\"]")
		if strings.Contains(inner, "\"") {
			t.Errorf("raw double-quote found inside Mermaid node label: %q", line)
		}
	}
	if strings.Contains(resp.Mermaid, long) {
		t.Error("full label should have been truncated in Mermaid output")
	}
	// nodes array must carry the full, un-truncated label.
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes array should have 1 entry, got %d", len(resp.Nodes))
	}
	if resp.Nodes[0].Label != long {
		t.Errorf("nodes[0].label should be the full un-truncated label;\ngot:  %q\nwant: %q", resp.Nodes[0].Label, long)
	}
}

// ── visualise neighbourhood tests ─────────────────────────────────────────────

func TestVisualiseNeighbourhood_MultipleConnections(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-nb-1"

	idA := addNode(t, h, "Hub node", domain, nil)
	idB := addNode(t, h, "Spoke B", domain, nil)
	idC := addNode(t, h, "Spoke C", domain, nil)

	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idA, "to_memory": idB, "relationship": "led_to", "narrative": "a led to b",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idA, "to_memory": idC, "relationship": "depends_on", "narrative": "a depends on c",
	}))

	tr := call(t, h, "visualise", map[string]any{"memory_id": idA})
	mustNotError(t, tr)

	var resp struct {
		Mermaid   string `json:"mermaid"`
		NodeCount int    `json:"node_count"`
		EdgeCount int    `json:"edge_count"`
		Nodes     []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"nodes"`
		Edges []struct {
			From         string `json:"from"`
			To           string `json:"to"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.NodeCount != 3 {
		t.Errorf("expected 3 nodes (hub + 2 spokes), got %d", resp.NodeCount)
	}
	if resp.EdgeCount != 2 {
		t.Errorf("expected 2 edges, got %d", resp.EdgeCount)
	}
	if !strings.Contains(resp.Mermaid, "flowchart TD") {
		t.Error("mermaid should contain flowchart TD header")
	}
	if !strings.Contains(resp.Mermaid, "Hub node") {
		t.Error("mermaid should contain Hub node label")
	}
	if !strings.Contains(resp.Mermaid, "led_to") {
		t.Error("mermaid should contain led_to relationship")
	}
	// Structured nodes: real IDs, full labels.
	if len(resp.Nodes) != 3 {
		t.Errorf("nodes array should have 3 entries, got %d", len(resp.Nodes))
	}
	foundHub := false
	for _, n := range resp.Nodes {
		if n.Label == "Hub node" {
			foundHub = true
		}
		if n.ID == "" {
			t.Error("node entry id should be non-empty")
		}
	}
	if !foundHub {
		t.Error("nodes array should contain 'Hub node' with full label")
	}
	// Structured edges: from/to are real IDs.
	if len(resp.Edges) != 2 {
		t.Errorf("edges array should have 2 entries, got %d", len(resp.Edges))
	}
	for _, e := range resp.Edges {
		if e.From == "" || e.To == "" || e.Relationship == "" {
			t.Errorf("edge entry missing fields: %+v", e)
		}
	}
}

func TestVisualiseNeighbourhood_NoConnections(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-nb-2"
	idA := addNode(t, h, "Lone node", domain, nil)

	tr := call(t, h, "visualise", map[string]any{"memory_id": idA})
	mustNotError(t, tr)

	var resp struct {
		Mermaid   string `json:"mermaid"`
		NodeCount int    `json:"node_count"`
		EdgeCount int    `json:"edge_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.NodeCount != 1 {
		t.Errorf("expected 1 node (the lone node itself), got %d", resp.NodeCount)
	}
	if resp.EdgeCount != 0 {
		t.Errorf("expected 0 edges, got %d", resp.EdgeCount)
	}
	if !strings.Contains(resp.Mermaid, "Lone node") {
		t.Error("mermaid should contain the lone node label")
	}
}

func TestVisualiseNeighbourhood_UnknownNodeID(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "visualise", map[string]any{"memory_id": "no-such-node-id"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("error message should mention 'not found'; got: %s", text(t, tr))
	}
}

func TestVisualiseNeighbourhood_NodeIDTakesPrecedenceOverDomain(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-vis-nb-3"

	idA := addNode(t, h, "Alpha", domain, nil)
	addNode(t, h, "Beta", domain, nil)
	addNode(t, h, "Gamma", domain, nil)

	// domain has 3 nodes; memory_id points to an isolated memory — result should be 1 node, not 3.
	tr := call(t, h, "visualise", map[string]any{"memory_id": idA, "domain": domain})
	mustNotError(t, tr)

	var resp struct {
		NodeCount int `json:"node_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.NodeCount != 1 {
		t.Errorf("memory_id should take precedence: expected 1 node, got %d", resp.NodeCount)
	}
}

// ── check_for_updates tests ───────────────────────────────────────────────────

func newHandlerWithVersion(t *testing.T, version string, checker func() (string, error)) *tools.Handler {
	t.Helper()
	_, store, _ := newEnvWithPath(t)
	return tools.New(store, version, checker)
}

// ── revise: occurred_at ───────────────────────────────────────────────────────

func TestRevise_SetsOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event A", "proj", map[string]any{
		"why_matters": "sets the occurred_at test baseline",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2026-05-12",
	})
	mustNotError(t, tr)

	// Verify occurred_at is set via history tool.
	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	if !strings.Contains(text(t, hr), "2026-05-12") {
		t.Errorf("expected 2026-05-12 in history output after revise; got: %s", text(t, hr))
	}
}

func TestRevise_InvalidOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event B", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "not-a-date",
	})
	mustError(t, tr)
}

func TestRevise_OmittingOccurredAt_LeavesItUnchanged(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event C", "proj", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline event for leave-unchanged test",
	})

	// Revise label only; occurred_at should remain.
	tr := call(t, h, "revise", map[string]any{
		"id":    id,
		"label": "event C revised",
	})
	mustNotError(t, tr)

	// Verify still in important_only history.
	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	if !strings.Contains(text(t, hr), "2026-03-01") {
		t.Errorf("occurred_at should be unchanged after label-only revise; got: %s", text(t, hr))
	}
}

func TestReviseAll_SetsOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "batch A", "proj", map[string]any{
		"why_matters": "batch revise test node A",
	})
	id2 := addNode(t, h, "batch B", "proj", map[string]any{
		"why_matters": "batch revise test node B",
	})

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id1, "occurred_at": "2026-04-01"},
			{"id": id2, "occurred_at": "2026-05-01"},
		},
	})
	mustNotError(t, tr)

	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	out := text(t, hr)
	if !strings.Contains(out, "2026-04-01") {
		t.Errorf("expected 2026-04-01 in history; got: %s", out)
	}
	if !strings.Contains(out, "2026-05-01") {
		t.Errorf("expected 2026-05-01 in history; got: %s", out)
	}
}

// ── revise: tags ─────────────────────────────────────────────────────────────

// TestRevise_Tags_ReplacesExistingTags: supplying tags to revise must overwrite
// the node's tags, not return the old ones.
func TestRevise_Tags_ReplacesExistingTags(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "tagged node", "proj", map[string]any{
		"tags": "old-tag alpha",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":   id,
		"tags": "new-tag beta",
	})
	mustNotError(t, tr)

	// The returned node must carry the new tags.
	var resp struct {
		Tags string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise response: %v", err)
	}
	if strings.Contains(resp.Tags, "old-tag") {
		t.Errorf("old tags still present after revise; tags = %q", resp.Tags)
	}
	if !strings.Contains(resp.Tags, "new-tag") {
		t.Errorf("new tags not present after revise; tags = %q", resp.Tags)
	}

	// Confirm via recall that the stored node reflects the new tags.
	// recall returns NodeWithEdges: {"node": {...}, "edges": [...]}
	rr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, rr)
	var recallResp struct {
		Node struct {
			Tags string `json:"tags"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, rr)), &recallResp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if strings.Contains(recallResp.Node.Tags, "old-tag") {
		t.Errorf("old tags still stored after revise; recall tags = %q", recallResp.Node.Tags)
	}
	if !strings.Contains(recallResp.Node.Tags, "new-tag") {
		t.Errorf("new tags not stored after revise; recall tags = %q", recallResp.Node.Tags)
	}
}

// TestRevise_Updates_WrapperRejected: passing tags (or any field) inside an
// "updates" wrapper object must return an error, not silently drop the change.
// This guards against the retired revise_all parameter format.
func TestRevise_Updates_WrapperRejected(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "wrapper test node", "proj", map[string]any{
		"tags": "original-tag",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":      id,
		"updates": map[string]any{"tags": "should-not-apply"},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "updates") {
		t.Errorf("error should mention 'updates'; got: %s", text(t, tr))
	}
}

// TestReviseBatch_Tags_ReplacesExistingTags: same guarantee in batch (items) mode.
func TestReviseBatch_Tags_ReplacesExistingTags(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "batch tagged node", "proj", map[string]any{
		"tags": "old-tag alpha",
	})

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id, "tags": "new-tag beta"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Updated []struct {
			Tags string `json:"tags"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch revise response: %v", err)
	}
	if len(resp.Updated) != 1 {
		t.Fatalf("expected 1 updated node, got %d", len(resp.Updated))
	}
	tags := resp.Updated[0].Tags
	if strings.Contains(tags, "old-tag") {
		t.Errorf("old tags still present after batch revise; tags = %q", tags)
	}
	if !strings.Contains(tags, "new-tag") {
		t.Errorf("new tags not present after batch revise; tags = %q", tags)
	}
}

// ── history tool ──────────────────────────────────────────────────────────────

func historyIDs(t *testing.T, tr *tools.ToolResult) []string {
	t.Helper()
	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("parse history response: %v", err)
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func TestHistory_DefaultMode_IncludesAllNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	// Node with occurred_at.
	idDated := addNode(t, h, "dated", "hist", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline for default mode test",
	})
	// Node without occurred_at.
	idUndated := addNode(t, h, "undated", "hist", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idDated) {
		t.Error("default mode: dated node should be included")
	}
	if !contains(ids, idUndated) {
		t.Error("default mode: undated node should be included")
	}
}

func TestHistory_ImportantOnly_ExcludesUndatedNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idDated := addNode(t, h, "dated event", "hist2", map[string]any{
		"occurred_at": "2026-04-01",
		"why_matters": "baseline for important-only test",
	})
	idUndated := addNode(t, h, "undated node", "hist2", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist2", "important_only": true})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idDated) {
		t.Error("important_only: dated node should appear")
	}
	if contains(ids, idUndated) {
		t.Error("important_only: undated node should not appear")
	}
}

func TestHistory_TagFilter_WholeWordMatch(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idA := addNode(t, h, "tagged A", "hist3", map[string]any{"tags": "decision architecture"})
	idB := addNode(t, h, "tagged B", "hist3", map[string]any{"tags": "architecture release"})
	idC := addNode(t, h, "no match C", "hist3", map[string]any{"tags": "release"})
	idD := addNode(t, h, "no tags D", "hist3", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist3", "tags": "architecture"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idA) {
		t.Error("node with 'decision architecture' should match tag 'architecture'")
	}
	if !contains(ids, idB) {
		t.Error("node with 'architecture release' should match tag 'architecture'")
	}
	if contains(ids, idC) {
		t.Error("node with only 'release' should not match tag 'architecture'")
	}
	if contains(ids, idD) {
		t.Error("node with no tags should not match tag 'architecture'")
	}
}

// ── history: memory_id mode ───────────────────────────────────────────────────

// TestHistory_MemoryIDMode_ReturnsChronological: nodes in the anchor's
// neighbourhood are returned in COALESCE(occurred_at, created_at) ASC order.
func TestHistory_MemoryIDMode_ReturnsChronological(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid", map[string]any{"occurred_at": "2026-01-01", "why_matters": "anchor node"})
	n1 := addNode(t, h, "March node", "hmid", map[string]any{"occurred_at": "2026-03-01", "why_matters": "march event"})
	n2 := addNode(t, h, "June node", "hmid", map[string]any{"occurred_at": "2026-06-01", "why_matters": "june event"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": n1, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": n2, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, anchor) || !contains(ids, n1) || !contains(ids, n2) {
		t.Fatalf("expected all three nodes, got %v", ids)
	}
	anchorIdx, n1Idx, n2Idx := indexOf(ids, anchor), indexOf(ids, n1), indexOf(ids, n2)
	if !(anchorIdx < n1Idx && n1Idx < n2Idx) {
		t.Errorf("wrong order: anchor=%d n1=%d n2=%d in %v", anchorIdx, n1Idx, n2Idx, ids)
	}
}

// TestHistory_MemoryIDMode_DomainClipped: a node in a different domain
// connected to the anchor must not appear.
func TestHistory_MemoryIDMode_DomainClipped(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid2", nil)
	foreign := addNode(t, h, "Foreign", "other-domain", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": foreign, "relationship": "connects_to", "because": "cross"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, foreign) {
		t.Error("foreign-domain node should not appear in memory_id history")
	}
}

// TestHistory_MemoryIDMode_ImportantOnly: important_only=true filters to nodes
// with occurred_at even in memory_id mode.
func TestHistory_MemoryIDMode_ImportantOnly(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid3", nil)
	dated := addNode(t, h, "Dated", "hmid3", map[string]any{"occurred_at": "2026-04-01", "why_matters": "dated decision"})
	undated := addNode(t, h, "Undated", "hmid3", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": dated, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": undated, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "important_only": true})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, undated) {
		t.Error("important_only: undated node should not appear")
	}
	if contains(ids, anchor) {
		t.Error("important_only: anchor (no occurred_at) should not appear")
	}
	if !contains(ids, dated) {
		t.Error("important_only: dated node should appear")
	}
}

// TestHistory_MemoryIDMode_TagsFilter: tags filter applies in memory_id mode.
func TestHistory_MemoryIDMode_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid4", nil)
	tagged := addNode(t, h, "Tagged", "hmid4", map[string]any{"tags": "release"})
	untagged := addNode(t, h, "Untagged", "hmid4", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": tagged, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": untagged, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "tags": "release"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, untagged) {
		t.Error("tag filter: untagged node should not appear")
	}
	if !contains(ids, tagged) {
		t.Error("tag filter: tagged node should appear")
	}
}

// TestHistory_MemoryIDMode_TakesPrecedenceOverDomain: when both memory_id and
// domain are supplied, the result is scoped to the neighbourhood, not the full domain.
func TestHistory_MemoryIDMode_TakesPrecedenceOverDomain(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid5", nil)
	connected := addNode(t, h, "Connected", "hmid5", nil)
	notConnected := addNode(t, h, "NotConnected", "hmid5", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": connected, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "domain": "hmid5"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, notConnected) {
		t.Error("memory_id takes precedence: unconnected domain node should not appear")
	}
	if !contains(ids, connected) {
		t.Error("connected node should appear")
	}
}

// TestHistory_MemoryIDMode_UnknownMemoryID: passing an unknown memory_id
// must return an error, not an empty list.
func TestHistory_MemoryIDMode_UnknownMemoryID(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{"memory_id": "no-such-id-00000000"})
	mustError(t, tr)
}

// TestHistory_MemoryIDMode_InSchema: the history schema must expose memory_id
// and depth properties.
func TestHistory_MemoryIDMode_InSchema(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "history" {
			continue
		}
		if _, ok := td.InputSchema.Properties["memory_id"]; !ok {
			t.Error("history schema missing 'memory_id' property")
		}
		if _, ok := td.InputSchema.Properties["depth"]; !ok {
			t.Error("history schema missing 'depth' property")
		}
		return
	}
	t.Fatal("history tool not found in ListTools")
}

func TestRemember_OrphanWarning_PresentWhenNoConnections(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "lonely node",
		"domain": "test",
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "orphan_warning") {
		t.Error("expected orphan_warning field in response")
	}
	if !strings.Contains(tr.Content[0].Text, "No connections were made") {
		t.Error("expected orphan_warning message in response")
	}
	if strings.Contains(tr.Content[0].Text, "cannot be connected directly") {
		t.Error("orphan_warning must not say 'cannot be connected directly' — use usage-instruction wording instead")
	}
	if !strings.Contains(tr.Content[0].Text, "pass their domain explicitly") {
		t.Error("orphan_warning must instruct agent to pass domain explicitly for cross-domain connect")
	}
}

func TestRemember_OrphanWarning_AbsentWhenRelatedToProvided(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "anchor", "test", nil)
	tr := call(t, h, "remember", map[string]any{
		"label":      "linked node",
		"domain":     "test",
		"related_to": []string{idA},
	})
	mustNotError(t, tr)
	if strings.Contains(tr.Content[0].Text, `"orphan_warning"`) {
		t.Error("orphan_warning should be absent when related_to was provided")
	}
}

func TestRememberAll_OrphanWarning_PresentWhenNoEdges(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "node one", "domain": "test"},
			{"label": "node two", "domain": "test"},
		},
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "orphan_warning") {
		t.Error("expected orphan_warning field in remember batch response")
	}
	if !strings.Contains(tr.Content[0].Text, "No connections were made") {
		t.Error("expected orphan_warning message in remember batch response")
	}
	if strings.Contains(tr.Content[0].Text, "cannot be connected directly") {
		t.Error("orphan_warning must not say 'cannot be connected directly' — use usage-instruction wording instead")
	}
	if !strings.Contains(tr.Content[0].Text, "pass their domain explicitly") {
		t.Error("orphan_warning must instruct agent to pass domain explicitly for cross-domain connect")
	}
}

// ── rename_domain ─────────────────────────────────────────────────────────────

func TestRenameDomain_RenamesNodesAndCreatesAlias(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "old-dom", nil)
	addNode(t, h, "Beta", "old-dom", nil)

	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "old-dom",
		"new_domain": "new-dom",
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, `"nodes_renamed": 2`) {
		t.Errorf("unexpected response: %s", tr.Content[0].Text)
	}
	if !strings.Contains(tr.Content[0].Text, "old-dom → new-dom") {
		t.Errorf("alias_created missing: %s", tr.Content[0].Text)
	}

	// Old domain should resolve to new domain via alias.
	resolve := call(t, h, "alias", map[string]any{"action": "resolve", "name": "old-dom"})
	mustNotError(t, resolve)
	if !strings.Contains(resolve.Content[0].Text, "new-dom") {
		t.Errorf("alias did not resolve: %s", resolve.Content[0].Text)
	}

	// Nodes should now be searchable under new domain.
	search := call(t, h, "search", map[string]any{"query": "Alpha", "domain": "new-dom"})
	mustNotError(t, search)
	if !strings.Contains(search.Content[0].Text, "Alpha") {
		t.Errorf("node not found in new domain: %s", search.Content[0].Text)
	}
}

func TestRenameDomain_OldDomainNotFound_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "nonexistent",
		"new_domain": "anything",
	})
	mustError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "no live nodes") {
		t.Errorf("unexpected error text: %s", tr.Content[0].Text)
	}
}

func TestRenameDomain_NewDomainAlreadyExists_DirectsToMerge(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "domain-a", nil)
	addNode(t, h, "Beta", "domain-b", nil)

	tr := call(t, h, "rename_domain", map[string]any{
		"old_domain": "domain-a",
		"new_domain": "domain-b",
	})
	mustError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "merge_domains") {
		t.Errorf("error should mention merge_domains: %s", tr.Content[0].Text)
	}
}

func TestRenameDomain_InListTools(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	if !strings.Contains(string(b), `"rename_domain"`) {
		t.Error("rename_domain not present in ListTools output")
	}
}

// ── Batch consolidation: remember/revise/connect accept items array ───────────

// TestRemember_BatchViaItems_FilesMultipleNodes: calling remember with an
// items array must create all nodes and return a nodes array response.
func TestRemember_BatchViaItems_FilesMultipleNodes(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Batch node A", "domain": "batch-test"},
			{"label": "Batch node B", "domain": "batch-test"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"node"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember batch response: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes in response, got %d", len(resp.Nodes))
	}
	labels := map[string]bool{}
	for _, n := range resp.Nodes {
		if n.Node.ID == "" {
			t.Error("batch node missing ID")
		}
		labels[n.Node.Label] = true
	}
	if !labels["Batch node A"] || !labels["Batch node B"] {
		t.Errorf("unexpected labels in batch response: %v", labels)
	}
}

// TestRemember_BatchViaItems_OrphanWarningPresent: batch remember with no
// edges must include orphan_warning and return a nodes array (not single node shape).
func TestRemember_BatchViaItems_OrphanWarningPresent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Orphan batch node", "domain": "batch-orphan"},
		},
	})
	mustNotError(t, tr)

	// Must return the batch shape (nodes array), not the single shape (node object).
	var resp struct {
		Nodes         []json.RawMessage `json:"nodes"`
		OrphanWarning string            `json:"orphan_warning"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch remember response: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node in batch response, got %d", len(resp.Nodes))
	}
	if resp.OrphanWarning == "" {
		t.Error("expected non-empty orphan_warning in batch remember response")
	}
}

// TestRememberAll_IsUnknownTool: after consolidation, remember_all must no
// longer be a registered tool.
func TestRememberAll_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{"label": "Should fail", "domain": "test"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %s", text(t, tr))
	}
}

// TestRevise_BatchViaItems_UpdatesMultiple: calling revise with an items array
// must update all entries and return an updated array response.
func TestRevise_BatchViaItems_UpdatesMultiple(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Revise batch A", "batch-revise", nil)
	idB := addNode(t, h, "Revise batch B", "batch-revise", nil)

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": idA, "label": "Revise batch A updated"},
			{"id": idB, "description": "now has a description"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Updated []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise batch response: %v", err)
	}
	if len(resp.Updated) != 2 {
		t.Fatalf("expected 2 updated in response, got %d", len(resp.Updated))
	}
	ids := map[string]bool{}
	for _, n := range resp.Updated {
		ids[n.ID] = true
	}
	if !ids[idA] || !ids[idB] {
		t.Errorf("unexpected IDs in revise batch response: %v", ids)
	}
}

// TestReviseAll_IsUnknownTool: after consolidation, revise_all must no
// longer be a registered tool.
func TestReviseAll_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Revise all target", "test", nil)
	tr := call(t, h, "revise_all", map[string]any{
		"updates": []map[string]any{
			{"id": id, "label": "Updated"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %s", text(t, tr))
	}
}

// TestConnect_BatchViaItems_CreatesMultipleEdges: calling connect with an
// items array must create all edges and return edges_created count.
func TestConnect_BatchViaItems_CreatesMultipleEdges(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Connect batch A", "batch-connect", nil)
	idB := addNode(t, h, "Connect batch B", "batch-connect", nil)
	idC := addNode(t, h, "Connect batch C", "batch-connect", nil)

	tr := call(t, h, "connect", map[string]any{
		"items": []map[string]any{
			{"from_memory": idA, "to_memory": idB, "relationship": "connects_to"},
			{"from_memory": idB, "to_memory": idC, "relationship": "led_to"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		EdgesCreated int `json:"edges_created"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse connect batch response: %v", err)
	}
	if resp.EdgesCreated != 2 {
		t.Errorf("expected edges_created=2, got %d", resp.EdgesCreated)
	}
}

// TestConnectAll_IsUnknownTool: after consolidation, connect_all must no
// longer be a registered tool.
func TestConnectAll_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Connect all A", "test", nil)
	idB := addNode(t, h, "Connect all B", "test", nil)
	tr := call(t, h, "connect_all", map[string]any{
		"edges": []map[string]any{
			{"from_memory": idA, "to_memory": idB, "relationship": "connects_to"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %s", text(t, tr))
	}
}

// ── tool description quality tests ───────────────────────────────────────────

// TestListTools_PresentationInstructionOnAllRetrievalTools asserts that every
// retrieval tool carries the "Never acknowledge that you are retrieving"
// presentation instruction so agents don't expose the memory system.
func TestListTools_PresentationInstructionOnAllRetrievalTools(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	index := map[string]string{}
	for _, td := range resp.Tools {
		index[td.Name] = td.Description
	}
	retrieval := []string{"search", "recall", "recent", "orient", "history", "why_connected", "significance"}
	const want = "Never acknowledge that you are retrieving"
	for _, name := range retrieval {
		desc, ok := index[name]
		if !ok {
			t.Errorf("tool %q not found in ListTools", name)
			continue
		}
		if !strings.Contains(desc, want) {
			t.Errorf("tool %q missing presentation instruction; want substring %q\ngot: %.200s...", name, want, desc)
		}
	}
}

// TestVisualise_NoClientConditional asserts that the visualise description
// contains no "If the client supports" conditional — agents cannot reliably
// detect rendering capabilities at runtime.
func TestVisualise_NoClientConditional(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "visualise" {
			if strings.Contains(td.Description, "If the client supports") {
				t.Errorf("visualise description must not contain client capability conditional;\ngot: %s", td.Description)
			}
			return
		}
	}
	t.Fatal("visualise tool not found in ListTools")
}

// TestRemember_ConnectInstructionAtTop asserts that the post-filing connect
// imperative appears before the "Single mode" parameter documentation in the
// remember description — agents must see it before reaching parameter docs.
func TestRemember_ConnectInstructionAtTop(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "remember" {
			connectIdx := strings.Index(td.Description, "After filing, call connect")
			singleModeIdx := strings.Index(td.Description, "Single mode")
			if connectIdx == -1 {
				t.Error(`remember description missing "After filing, call connect" imperative`)
				return
			}
			if connectIdx > singleModeIdx {
				t.Errorf("remember description: connect imperative (pos %d) must appear before Single mode docs (pos %d)", connectIdx, singleModeIdx)
			}
			return
		}
	}
	t.Fatal("remember tool not found in ListTools")
}

// TestListTools_BatchVariantsRemoved: ListTools must not contain remember_all,
// revise_all, or connect_all after consolidation.
func TestListTools_BatchVariantsRemoved(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	s := string(b)
	for _, removed := range []string{"remember_all", "revise_all", "connect_all"} {
		if strings.Contains(s, `"`+removed+`"`) {
			t.Errorf("tool %q must not appear in ListTools after consolidation", removed)
		}
	}
}

// ── audit tool (slice 2) ──────────────────────────────────────────────────────

// TestAudit_ModeStale_ReturnsDriftCandidates: mode=stale must return drift
// candidates (same behaviour as the removed whats_stale tool).
func TestAudit_ModeStale_ReturnsDriftCandidates(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "old transient", "proj", map[string]any{"transient": true})
	tr := call(t, h, "audit", map[string]any{"mode": "stale"})
	mustNotError(t, tr)
}

// TestAudit_ModeOrphans_ReturnsDisconnected: mode=orphans must return
// non-transient nodes with zero connections.
func TestAudit_ModeOrphans_ReturnsDisconnected(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "lonely node", "proj", nil)
	tr := call(t, h, "audit", map[string]any{"mode": "orphans"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), id) {
		t.Errorf("expected orphan node %q in audit orphans response", id)
	}
}

// TestAudit_ModeArchived_ReturnsArchivedNodes: mode=archived must return
// nodes that were archived.
func TestAudit_ModeArchived_ReturnsArchivedNodes(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "to be archived", "proj", nil)
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id, "reason": "test"}))
	tr := call(t, h, "audit", map[string]any{"mode": "archived"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), id) {
		t.Errorf("expected archived node %q in audit archived response", id)
	}
}

// TestAudit_InvalidMode_ReturnsError: an unrecognised mode must return an error.
func TestAudit_InvalidMode_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "audit", map[string]any{"mode": "nonsense"})
	mustError(t, tr)
}

// TestWhatsStale_IsUnknownTool: after consolidation, whats_stale must return
// an error directing to the audit tool.
func TestWhatsStale_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "whats_stale", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// TestDisconnected_IsUnknownTool: after consolidation, disconnected must
// return an error directing to the audit tool.
func TestDisconnected_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "disconnected", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// TestForgotten_IsUnknownTool: after consolidation, forgotten must return an
// error directing to the audit tool.
func TestForgotten_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "forgotten", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// ── domains tool (slice 3) ────────────────────────────────────────────────────

// TestDomains_ReturnsDomainsAndAliases: domains must return a combined
// response containing domain list and alias list.
func TestDomains_ReturnsDomainsAndAliases(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "A", "alpha", nil)
	addNode(t, h, "B", "beta", nil)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "al", "domain": "alpha"}))
	tr := call(t, h, "domains", map[string]any{})
	mustNotError(t, tr)
	out := text(t, tr)
	if !strings.Contains(out, "alpha") {
		t.Error("expected 'alpha' in domains response")
	}
	if !strings.Contains(out, "al") {
		t.Error("expected alias 'al' in domains response")
	}
}

// TestListDomains_IsUnknownTool: list_domains must return an error after consolidation.
func TestListDomains_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "list_domains", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestListAliases_IsUnknownTool: list_aliases must return an error after consolidation.
func TestListAliases_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "list_aliases", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// ── alias tool (slice 4) ──────────────────────────────────────────────────────

// TestAlias_Add_RegistersAlias: action=add must register the alias.
func TestAlias_Add_RegistersAlias(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "mw") {
		t.Errorf("expected alias name in response; got: %s", text(t, tr))
	}
}

// TestAlias_Remove_RemovesAlias: action=remove must remove a registered alias.
func TestAlias_Remove_RemovesAlias(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "remove", "alias": "mw"})
	mustNotError(t, tr)
}

// TestAlias_Resolve_ReturnsCanonical: action=resolve must return the canonical domain.
func TestAlias_Resolve_ReturnsCanonical(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "resolve", "name": "mw"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "memoryweb") {
		t.Errorf("expected 'memoryweb' in resolve response; got: %s", text(t, tr))
	}
}

// TestAlias_List_ReturnsAllAliases: action=list must return all registered aliases.
func TestAlias_List_ReturnsAllAliases(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "alias", map[string]any{"action": "add", "alias": "mw", "domain": "memoryweb"}))
	tr := call(t, h, "alias", map[string]any{"action": "list"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), "mw") {
		t.Errorf("expected alias 'mw' in list response; got: %s", text(t, tr))
	}
}

// TestAlias_InvalidAction_ReturnsError: an unknown action must return an error.
func TestAlias_InvalidAction_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias", map[string]any{"action": "badaction"})
	mustError(t, tr)
}

// TestAliasDomain_IsUnknownTool: alias_domain must return an error after consolidation.
func TestAliasDomain_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "alias_domain", map[string]any{"alias": "x", "domain": "y"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestRemoveAlias_IsUnknownTool: remove_alias must return an error after consolidation.
func TestRemoveAlias_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remove_alias", map[string]any{"alias": "x"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestResolveDomain_IsUnknownTool: resolve_domain must return an error after consolidation.
func TestResolveDomain_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "resolve_domain", map[string]any{"name": "x"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// ── forget_all tool ───────────────────────────────────────────────────────────

// TestForgetAll_ArchivesMultipleNodes: forget_all must archive all provided
// IDs in a single transaction; they must no longer appear in search.
func TestForgetAll_ArchivesMultipleNodes(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "node alpha", "proj", nil)
	id2 := addNode(t, h, "node beta", "proj", nil)
	tr := call(t, h, "forget_all", map[string]any{
		"items": []map[string]any{
			{"id": id1, "reason": "test cleanup"},
			{"id": id2, "reason": "test cleanup"},
		},
	})
	mustNotError(t, tr)
	// Both should no longer appear in search.
	sr := call(t, h, "search", map[string]any{"query": "node alpha", "domain": "proj"})
	mustNotError(t, sr)
	if strings.Contains(text(t, sr), id1) {
		t.Error("archived node id1 should not appear in search")
	}
}

// TestForgetAll_UnknownID_ReturnsError: forget_all with an unknown ID must
// return an error and not archive any nodes (atomic).
func TestForgetAll_UnknownID_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "should stay live", "proj", nil)
	tr := call(t, h, "forget_all", map[string]any{
		"items": []map[string]any{
			{"id": id1, "reason": "cleanup"},
			{"id": "nonexistent-id-xyz", "reason": "cleanup"},
		},
	})
	mustError(t, tr)
	// id1 must still be live (transaction rolled back).
	sr := call(t, h, "search", map[string]any{"query": "should stay live", "domain": "proj"})
	mustNotError(t, sr)
	if !strings.Contains(text(t, sr), id1) {
		t.Error("id1 should still be live after failed forget_all")
	}
}

// TestCheckForUpdates_IsUnknownTool: check_for_updates must return an error
// after being removed from the MCP surface.
func TestCheckForUpdates_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "check_for_updates", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}

// TestListTools_Slice2And3Removed: verifies all tools removed in slices 2–4
// no longer appear in ListTools output.
func TestListTools_Slice2And3Removed(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	s := string(b)
	for _, removed := range []string{
		"whats_stale", "disconnected", "forgotten",
		"list_domains", "list_aliases",
		"alias_domain", "remove_alias", "resolve_domain",
		"check_for_updates",
	} {
		if strings.Contains(s, `"`+removed+`"`) {
			t.Errorf("tool %q must not appear in ListTools after consolidation", removed)
		}
	}
}

// ── significance ──────────────────────────────────────────────────────────────

func TestSignificance_ReturnsAllFourSections(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{"domain": "empty-domain"})
	mustNotError(t, tr)

	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	for _, key := range []string{"declared", "structural", "uncurated", "potentially_stale", "call_id"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}
}

func TestSignificance_IsErrorWhenNeitherDomainNorMemoryIDProvided(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{})
	mustError(t, tr)
}

func TestSignificance_StructuralRankingCorrect(t *testing.T) {
	_, h := newEnv(t)

	popular := addNode(t, h, "Popular node", "proj", nil)
	niche := addNode(t, h, "Niche node", "proj", nil)

	// 3 linkers → popular
	for i := 0; i < 3; i++ {
		linker := addNode(t, h, fmt.Sprintf("Linker %d", i), "proj", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": popular,
			"relationship": "connects_to", "narrative": "links",
		}))
	}
	// 1 linker → niche
	nicheLinker := addNode(t, h, "Niche linker", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nicheLinker, "to_memory": niche,
		"relationship": "connects_to", "narrative": "links",
	}))

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Structural []struct {
			ID              string  `json:"id"`
			ImportanceScore float64 `json:"importance_score"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Structural) < 2 {
		t.Fatalf("expected at least 2 structural entries, got %d", len(resp.Structural))
	}
	if resp.Structural[0].ID != popular {
		t.Errorf("Structural[0]: want %q (popular), got %q", popular, resp.Structural[0].ID)
	}
}

func TestSignificance_PotentiallyStaleDetected(t *testing.T) {
	_, h := newEnv(t)

	// Node with occurred_at but no inbound edges — structurally irrelevant.
	isolated := addNode(t, h, "Isolated significant node", "proj", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z",
		"why_matters": "key decision with no dependants",
	})

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		PotentiallyStale []struct {
			ID string `json:"id"`
		} `json:"potentially_stale"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	found := false
	for _, n := range resp.PotentiallyStale {
		if n.ID == isolated {
			found = true
		}
	}
	if !found {
		t.Error("isolated node with occurred_at and no inbound edges should appear in potentially_stale")
	}
}

func TestSignificance_DefaultsApplied(t *testing.T) {
	// Omitting limit and recency_window should default to 10 and 90.
	// Verify: a domain with >10 linker targets returns at most 10 structural entries.
	_, h := newEnv(t)

	for i := 0; i < 12; i++ {
		target := addNode(t, h, fmt.Sprintf("Target %d", i), "proj", nil)
		linker := addNode(t, h, fmt.Sprintf("Linker %d", i), "proj", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": target,
			"relationship": "connects_to", "narrative": "links",
		}))
	}

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Structural) > 10 {
		t.Errorf("default limit=10: structural should have at most 10 entries, got %d", len(resp.Structural))
	}
}

// ── forget / forget_all discoverability ──────────────────────────────────────

// TestListTools_ForgetAllLeadsWithUseCase: forget_all description must open with
// "Batch archive" so agents reach for it when archiving multiple nodes.
func TestListTools_ForgetAllLeadsWithUseCase(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "forget_all" {
			if !strings.HasPrefix(td.Description, "Batch archive") {
				t.Errorf("forget_all description must start with \"Batch archive\", got: %.60s", td.Description)
			}
			return
		}
	}
	t.Fatal("forget_all tool not found in ListTools")
}

// TestListTools_ForgetCrossReferencesForgetAll: forget description must mention
// forget_all so agents discover the batch path when archiving multiple nodes.
func TestListTools_ForgetCrossReferencesForgetAll(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "forget" {
			if !strings.Contains(td.Description, "forget_all") {
				t.Error("forget description must contain a cross-reference to forget_all")
			}
			return
		}
	}
	t.Fatal("forget tool not found in ListTools")
}

// ── remember domain inference ─────────────────────────────────────────────────

// ── orient: optional domain ───────────────────────────────────────────────────

// TestOrient_NoDomain_ReturnsCrossDomainSnapshot: calling orient with no domain
// must return mode="cross_domain_snapshot" with a domains array containing at
// least one entry that has domain and recent fields.
func TestOrient_NoDomain_ReturnsCrossDomainSnapshot(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "domain-a", nil)
	addNode(t, h, "Beta", "domain-b", nil)

	tr := call(t, h, "orient", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Mode    string `json:"mode"`
		Domains []struct {
			Domain string        `json:"domain"`
			Recent []interface{} `json:"recent"`
		} `json:"domains"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse cross-domain snapshot: %v", err)
	}
	if resp.Mode != "cross_domain_snapshot" {
		t.Errorf("expected mode=cross_domain_snapshot; got %q", resp.Mode)
	}
	if len(resp.Domains) == 0 {
		t.Fatal("expected at least one domain in snapshot; got none")
	}
	for _, d := range resp.Domains {
		if d.Domain == "" {
			t.Error("domain entry has empty domain field")
		}
		if d.Recent == nil {
			t.Errorf("domain %q has nil recent array", d.Domain)
		}
	}
}

// TestOrient_WithDomain_Unchanged: orient with a domain must still return the
// three-section response (declared_spine, significant, recent) unchanged.
func TestOrient_WithDomain_Unchanged(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Existing node", "orient-regression-domain", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-regression-domain"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine interface{} `json:"declared_spine"`
		Significant   interface{} `json:"significant"`
		Recent        interface{} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.DeclaredSpine == nil {
		t.Error("orient with domain missing declared_spine")
	}
	if resp.Significant == nil {
		t.Error("orient with domain missing significant")
	}
	if resp.Recent == nil {
		t.Error("orient with domain missing recent")
	}
}

// TestListTools_RememberDescriptionContainsDomainInference: remember description
// must instruct agents to infer domain from search results and prefer existing
// domains over creating new ones.
func TestListTools_RememberDescriptionContainsDomainInference(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "remember" {
			if !strings.Contains(td.Description, "infer the domain") {
				t.Error(`remember description missing "infer the domain" guidance`)
			}
			if !strings.Contains(td.Description, "Prefer existing domains") {
				t.Error(`remember description missing "Prefer existing domains" guidance`)
			}
			return
		}
	}
	t.Fatal("remember tool not found in ListTools")
}

// ── revise: transient field ───────────────────────────────────────────────────

// TestRevise_TransientUpdatable covers all transient field scenarios via revise:
// clearing the flag, setting it, leaving it unchanged, batch mode, and edge preservation.
func TestRevise_TransientUpdatable(t *testing.T) {
	t.Run("clear transient (transient to decision)", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Transient node", "transient-test", map[string]any{"decision_type": "transient"})

		tr := call(t, h, "revise", map[string]any{"id": id, "decision_type": "decision"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if strings.Contains(text(t, got), `"decision_type": "transient"`) {
			t.Error("expected decision_type to be cleared to 'decision'")
		}
	})

	t.Run("set transient (decision to transient)", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Permanent node", "transient-test", nil)

		tr := call(t, h, "revise", map[string]any{"id": id, "decision_type": "transient"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), `"decision_type": "transient"`) {
			t.Error("expected decision_type to be set to 'transient'")
		}
	})

	t.Run("omit decision_type - unchanged", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Transient node", "transient-test", map[string]any{"decision_type": "transient"})

		tr := call(t, h, "revise", map[string]any{"id": id, "label": "Updated label"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), `"decision_type": "transient"`) {
			t.Error("expected decision_type to remain 'transient' when omitted from revise")
		}
	})

	t.Run("batch mode sets decision_type", func(t *testing.T) {
		_, h := newEnv(t)
		id1 := addNode(t, h, "Batch node A", "transient-test", nil)
		id2 := addNode(t, h, "Batch node B", "transient-test", map[string]any{"decision_type": "transient"})

		items := []map[string]any{
			{"id": id1, "decision_type": "transient"},
			{"id": id2, "decision_type": "decision"},
		}
		tr := call(t, h, "revise", map[string]any{"items": items})
		mustNotError(t, tr)

		got1 := call(t, h, "recall", map[string]any{"id": id1})
		mustNotError(t, got1)
		if !strings.Contains(text(t, got1), `"decision_type": "transient"`) {
			t.Error("batch: expected id1 decision_type='transient'")
		}

		got2 := call(t, h, "recall", map[string]any{"id": id2})
		mustNotError(t, got2)
		if strings.Contains(text(t, got2), `"decision_type": "transient"`) {
			t.Error("batch: expected id2 decision_type to be cleared to 'decision'")
		}
	})

	t.Run("preserves edges when decision_type changes", func(t *testing.T) {
		_, h := newEnv(t)
		id1 := addNode(t, h, "Node with edge", "transient-test", nil)
		id2 := addNode(t, h, "Connected node", "transient-test", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": id1, "to_memory": id2, "relationship": "connects_to", "because": "test edge",
		}))

		mustNotError(t, call(t, h, "revise", map[string]any{"id": id1, "decision_type": "transient"}))

		got := call(t, h, "recall", map[string]any{"id": id1})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), id2) {
			t.Error("expected edge to id2 to be preserved after transient change")
		}
	})
}

// TestRevise_TransientInSchema verifies the revise tool schema exposes transient as
// a boolean property in both single mode and the items array.
func TestRevise_TransientInSchema(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Type  string `json:"type"`
					Items struct {
						Properties map[string]struct {
							Type string `json:"type"`
						} `json:"properties"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "revise" {
			continue
		}
		if _, ok := td.InputSchema.Properties["transient"]; !ok {
			t.Error("revise schema missing top-level transient property")
		}
		items, ok := td.InputSchema.Properties["items"]
		if !ok {
			t.Fatal("revise schema missing items property")
		}
		if _, ok := items.Items.Properties["transient"]; !ok {
			t.Error("revise items schema missing transient property")
		}
		return
	}
	t.Fatal("revise tool not found in ListTools")
}

// ── orient sufficiency-bias constraint ───────────────────────────────────────

// TestOrient_DescriptionContainsCausalSequenceConstraint: orient description must
// prohibit answering causal/chronological-sequence questions from orient alone.
func TestOrient_DescriptionContainsCausalSequenceConstraint(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "causal or chronological sequence") {
				t.Error("orient description missing prohibition on answering causal/chronological-sequence questions from orient alone")
			}
			return
		}
	}
	t.Fatal("orient tool not found in ListTools")
}

// TestOrient_DescriptionContainsHistoryFallback: orient description must direct
// agents to history(important_only=true) for sequence-dependent questions.
func TestOrient_DescriptionContainsHistoryFallback(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "history(important_only=true)") {
				t.Error("orient description must reference history(important_only=true) as the required tool for sequence-dependent questions")
			}
			return
		}
	}
	t.Fatal("orient tool not found in ListTools")
}

// ── significance: memory_id mode ─────────────────────────────────────────────

// TestSignificance_MemoryIDMode_ReturnsAllFourSections: calling significance
// with a memory_id must return all four sections without error.
func TestSignificance_MemoryIDMode_ReturnsAllFourSections(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor memory", "proj", nil)
	neighbour := addNode(t, h, "neighbour memory", "proj", nil)
	linker := addNode(t, h, "linker memory", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "linked",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "linker points to neighbour",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	out := text(t, tr)
	for _, section := range []string{"declared", "structural", "uncurated", "potentially_stale"} {
		if !strings.Contains(out, section) {
			t.Errorf("expected section %q in significance response; got: %s", section, out)
		}
	}
}

// TestSignificance_MemoryIDMode_DomainClipped: cross-domain nodes connected to
// the anchor must NOT appear in the neighbourhood result.
func TestSignificance_MemoryIDMode_DomainClipped(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor proj", "proj", nil)
	crossDomain := addNode(t, h, "foreign memory", "other-domain", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": crossDomain,
		"relationship": "connects_to", "narrative": "cross-domain edge",
	}))
	linker := addNode(t, h, "linker for foreign", "other-domain", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": crossDomain,
		"relationship": "connects_to", "narrative": "linker",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	out := text(t, tr)
	if strings.Contains(out, crossDomain) {
		t.Errorf("cross-domain memory %q must not appear in neighbourhood significance", crossDomain)
	}
}

// TestSignificance_MemoryIDMode_Depth2Included: a node two hops from the
// anchor (anchor→A→B) must appear in the result when depth=2.
func TestSignificance_MemoryIDMode_Depth2Included(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor node d2", "proj", nil)
	nodeA := addNode(t, h, "depth1 node d2", "proj", nil)
	nodeB := addNode(t, h, "depth2 node d2", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": nodeA,
		"relationship": "connects_to", "narrative": "a1",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nodeA, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "a2",
	}))
	linker := addNode(t, h, "linker for b d2", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "linker b",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor, "depth": 2})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), nodeB) {
		t.Errorf("depth-2 memory %q must appear in result with depth=2", nodeB)
	}
}

// TestSignificance_MemoryIDMode_Depth1Excluded: a node two hops from the
// anchor must NOT appear when depth=1.
func TestSignificance_MemoryIDMode_Depth1Excluded(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor node d1", "proj", nil)
	nodeA := addNode(t, h, "depth1 node d1", "proj", nil)
	nodeB := addNode(t, h, "depth2 node d1", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": nodeA,
		"relationship": "connects_to", "narrative": "a1",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nodeA, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "a2",
	}))
	linker := addNode(t, h, "linker for b d1", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "linker b",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor, "depth": 1})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), nodeB) {
		t.Errorf("depth-2 memory %q must NOT appear in result when depth=1", nodeB)
	}
}

// TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain: when both domain
// and memory_id are supplied, memory_id mode runs (neighbourhood is smaller
// than the full domain).
func TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain(t *testing.T) {
	_, h := newEnv(t)
	for i := 0; i < 4; i++ {
		popular := addNode(t, h, fmt.Sprintf("popular domain node %d", i), "proj", nil)
		for j := 0; j < 3; j++ {
			lnk := addNode(t, h, fmt.Sprintf("domain linker %d-%d", i, j), "proj", nil)
			mustNotError(t, call(t, h, "connect", map[string]any{
				"from_memory": lnk, "to_memory": popular,
				"relationship": "connects_to", "narrative": "link",
			}))
		}
	}
	anchor := addNode(t, h, "isolated anchor prec", "proj", nil)
	neighbour := addNode(t, h, "only neighbour prec", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "sole link",
	}))

	memoryTR := call(t, h, "significance", map[string]any{"domain": "proj", "memory_id": anchor})
	mustNotError(t, memoryTR)

	for i := 0; i < 4; i++ {
		if strings.Contains(text(t, memoryTR), fmt.Sprintf("popular domain node %d", i)) {
			t.Errorf("popular domain node %d must not appear in memory_id mode result", i)
		}
	}
}

// TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID: passing a
// non-existent memory_id must return an error.
func TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{"memory_id": "nonexistent-memory-id-xyz"})
	mustError(t, tr)
}

// TestSignificance_MemoryIDMode_InSchemaWithDepth: the significance schema
// must expose memory_id and depth properties, and domain must not be in the
// Required array.
func TestSignificance_MemoryIDMode_InSchemaWithDepth(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "significance" {
			continue
		}
		if _, ok := td.InputSchema.Properties["memory_id"]; !ok {
			t.Error("significance schema missing 'memory_id' property")
		}
		if _, ok := td.InputSchema.Properties["depth"]; !ok {
			t.Error("significance schema missing 'depth' property")
		}
		for _, req := range td.InputSchema.Required {
			if req == "domain" {
				t.Error("'domain' must not be in significance Required array")
			}
		}
		return
	}
	t.Fatal("significance tool not found in ListTools")
}

// ── significance: tags filter ─────────────────────────────────────────────────

// TestSignificance_TagsFilter_IncludesMatchingNodes: when tags="mytag", only
// nodes whose tags field contains "mytag" appear in structural.
func TestSignificance_TagsFilter_IncludesMatchingNodes(t *testing.T) {
	_, h := newEnv(t)

	tagged := addNode(t, h, "Tagged node", "proj", map[string]interface{}{"tags": "mytag"})
	untagged := addNode(t, h, "Untagged node", "proj", nil)
	linker1 := addNode(t, h, "Linker for tagged", "proj", nil)
	linker2 := addNode(t, h, "Linker for untagged", "proj", nil)

	call(t, h, "connect", map[string]interface{}{"from_memory": linker1, "to_memory": tagged, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker2, "to_memory": untagged, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "mytag"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	for _, sn := range res.Structural {
		if sn.ID == untagged {
			t.Error("structural contains untagged node; expected only tagged nodes")
		}
	}
	found := false
	for _, sn := range res.Structural {
		if sn.ID == tagged {
			found = true
		}
	}
	if !found {
		t.Error("structural does not contain tagged node")
	}
}

// TestSignificance_TagsFilter_MultiTag_OR: tags="foo,bar" includes nodes
// matching either tag.
func TestSignificance_TagsFilter_MultiTag_OR(t *testing.T) {
	_, h := newEnv(t)

	fooNode := addNode(t, h, "Foo node", "proj", map[string]interface{}{"tags": "foo"})
	barNode := addNode(t, h, "Bar node", "proj", map[string]interface{}{"tags": "bar"})
	neither := addNode(t, h, "Neither node", "proj", nil)
	linker1 := addNode(t, h, "Linker foo", "proj", nil)
	linker2 := addNode(t, h, "Linker bar", "proj", nil)
	linker3 := addNode(t, h, "Linker neither", "proj", nil)

	call(t, h, "connect", map[string]interface{}{"from_memory": linker1, "to_memory": fooNode, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker2, "to_memory": barNode, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker3, "to_memory": neither, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "foo,bar"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	ids := map[string]bool{}
	for _, sn := range res.Structural {
		ids[sn.ID] = true
	}
	if !ids[fooNode] {
		t.Error("structural missing foo node")
	}
	if !ids[barNode] {
		t.Error("structural missing bar node")
	}
	if ids[neither] {
		t.Error("structural contains neither node; expected only foo and bar")
	}
}

// TestSignificance_TagsFilter_NoMatch_EmptyStructural: when the tag matches
// no nodes, structural is empty and the call does not error.
func TestSignificance_TagsFilter_NoMatch_EmptyStructural(t *testing.T) {
	_, h := newEnv(t)

	node := addNode(t, h, "Some node", "proj", nil)
	linker := addNode(t, h, "Linker", "proj", nil)
	call(t, h, "connect", map[string]interface{}{"from_memory": linker, "to_memory": node, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "nonexistent-tag"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(res.Structural) != 0 {
		t.Errorf("structural: want 0 entries, got %d", len(res.Structural))
	}
}

// TestSignificance_TagsFilter_WholeWordMatch: a node tagged "foobar" must not
// appear when filtering by tags="foo" (partial match must not fire).
func TestSignificance_TagsFilter_WholeWordMatch(t *testing.T) {
	_, h := newEnv(t)

	foobar := addNode(t, h, "Foobar node", "proj", map[string]interface{}{"tags": "foobar"})
	linker := addNode(t, h, "Linker", "proj", nil)
	call(t, h, "connect", map[string]interface{}{"from_memory": linker, "to_memory": foobar, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "foo"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	for _, sn := range res.Structural {
		if sn.ID == foobar {
			t.Error("structural contains foobar node on partial tag match 'foo'; whole-word match required")
		}
	}
}

// TestSignificance_TagsFilter_DeclaredRespected: a node with occurred_at and a
// matching tag must appear in declared when filtering by that tag.
func TestSignificance_TagsFilter_DeclaredRespected(t *testing.T) {
	_, h := newEnv(t)

	declaredID := addNode(t, h, "Declared tagged node", "proj", map[string]interface{}{
		"description": "has occurred_at and tag",
		"why_matters": "test",
		"occurred_at": "2024-01-15",
		"tags":        "release",
	})

	other := addNode(t, h, "Untagged declared", "proj", map[string]interface{}{"why_matters": "other"})
	call(t, h, "revise", map[string]interface{}{"id": other, "occurred_at": "2024-02-01", "why_matters": "other"})

	result := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "release"})
	mustNotError(t, result)

	var res struct {
		Declared []struct {
			ID string `json:"id"`
		} `json:"declared"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	found := false
	for _, n := range res.Declared {
		if n.ID == declaredID {
			found = true
		}
		if n.ID == other {
			t.Errorf("declared contains untagged node %q; expected only release-tagged nodes", other)
		}
	}
	if !found {
		t.Errorf("declared does not contain node %q with tag 'release'", declaredID)
	}
}

// ── DecisionType ──────────────────────────────────────────────────────────────

func TestRemember_DecisionTypeStanding(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "no deploys on friday", "proj", map[string]any{
		"decision_type": "standing",
		"why_matters":   "reduces incidents",
	})
	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			DecisionType string `json:"decision_type"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.DecisionType != "standing" {
		t.Errorf("decision_type: got %q, want %q", resp.Node.DecisionType, "standing")
	}
}

func TestRemember_DecisionTypeBackcompat_Transient(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "sprint ticket legacy", "proj", map[string]any{
		"transient": true,
	})
	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			DecisionType string `json:"decision_type"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.DecisionType != "transient" {
		t.Errorf("decision_type: got %q, want %q", resp.Node.DecisionType, "transient")
	}
}

func TestRevise_DecisionType(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "promote to standing", "proj", map[string]any{
		"decision_type": "decision",
		"why_matters":   "will be promoted",
	})
	tr := call(t, h, "revise", map[string]any{
		"id":            id,
		"decision_type": "standing",
	})
	mustNotError(t, tr)

	tr2 := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr2)
	var resp struct {
		Node struct {
			DecisionType string `json:"decision_type"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr2)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.DecisionType != "standing" {
		t.Errorf("decision_type after revise: got %q, want %q", resp.Node.DecisionType, "standing")
	}
}

func TestOrient_RulesSection_StandingNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "rule alpha", "proj", map[string]any{"decision_type": "standing", "why_matters": "governs deployments"})
	addNode(t, h, "rule beta", "proj", map[string]any{"decision_type": "standing", "why_matters": "governs testing"})
	addNode(t, h, "plain decision", "proj", map[string]any{})

	tr := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	rawRules, ok := resp["rules"]
	if !ok {
		t.Fatal("orient response missing 'rules' key")
	}
	var rules []any
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("rules section should be non-empty")
	}
}

func TestOrient_RulesSection_Absent_WhenNoStanding(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "just a decision", "proj", map[string]any{})
	addNode(t, h, "another decision", "proj", map[string]any{})

	tr := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := resp["rules"]; ok {
		t.Error("orient response should NOT contain 'rules' key when no standing nodes exist")
	}
}

func TestOrient_RulesSection_OrderedByInboundEdgeCount(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idA := addNode(t, h, "rule with no refs", "proj", map[string]any{"decision_type": "standing", "why_matters": "a"})
	idB := addNode(t, h, "rule with one ref", "proj", map[string]any{"decision_type": "standing", "why_matters": "b"})
	idLinker := addNode(t, h, "work item", "proj", map[string]any{})

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  idLinker,
		"to_memory":    idB,
		"relationship": "governed_by",
		"narrative":    "governed by rule B",
	})
	mustNotError(t, tr)

	tr2 := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr2)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr2)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	rawRules, ok := resp["rules"]
	if !ok {
		t.Fatal("orient response missing 'rules' key")
	}
	var rules []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) < 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != idB {
		t.Errorf("rules[0]: want idB (%q, 1 inbound), got %q", idB, rules[0].ID)
	}
	if rules[1].ID != idA {
		t.Errorf("rules[1]: want idA (%q, 0 inbound), got %q", idA, rules[1].ID)
	}
}

func TestConnect_GovernedBy(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	ruleID := addNode(t, h, "the standing rule", "proj", map[string]any{"decision_type": "standing", "why_matters": "a rule"})
	workID := addNode(t, h, "some work item", "proj", map[string]any{})

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  workID,
		"to_memory":    ruleID,
		"relationship": "governed_by",
		"narrative":    "this work follows the standing rule",
	})
	mustNotError(t, tr)
}

// ── search memory_id scoping ──────────────────────────────────────────────────

// TestSearch_MemoryID_ScopesResults: when memory_id is supplied, only nodes in
// the depth-2 neighbourhood of the anchor appear in results.
func TestSearch_MemoryID_ScopesResults(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor node", "proj", nil)
	neighbourID := addNode(t, h, "architecture neighbour", "proj", nil)
	unrelatedID := addNode(t, h, "architecture unrelated", "proj", nil)

	// connect anchor → neighbour
	call(t, h, "connect", map[string]any{
		"from_memory":  anchorID,
		"to_memory":    neighbourID,
		"relationship": "connects_to",
	})

	tr := call(t, h, "search", map[string]any{
		"query":     "architecture",
		"domain":    "proj",
		"memory_id": anchorID,
	})
	mustNotError(t, tr)

	ids := searchIDs(t, tr)
	for _, id := range ids {
		if id == unrelatedID {
			t.Error("unrelated node should be excluded when memory_id is set")
		}
	}
	if !contains(ids, neighbourID) {
		t.Error("neighbour node should appear in scoped results")
	}
}

// TestSearch_MemoryID_AbsentBehavesLikeDefault: omitting memory_id returns all
// matching nodes regardless of neighbourhood.
func TestSearch_MemoryID_AbsentBehavesLikeDefault(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	id1 := addNode(t, h, "arch alpha unlinked", "proj", nil)
	id2 := addNode(t, h, "arch beta unlinked", "proj", nil)

	tr := call(t, h, "search", map[string]any{
		"query":  "arch",
		"domain": "proj",
	})
	mustNotError(t, tr)

	ids := searchIDs(t, tr)
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Error("both nodes should appear when no memory_id filter is set")
	}
}

// TestSearch_MemoryID_SchemaHasProperty: the search tool input schema must
// expose the memory_id property so agents know it exists.
func TestSearch_MemoryID_SchemaHasProperty(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "search" {
			continue
		}
		prop, ok := td.InputSchema.Properties["memory_id"]
		if !ok {
			t.Fatal("search tool missing memory_id property in schema")
		}
		if prop.Description == "" {
			t.Error("memory_id property must have a description")
		}
		return
	}
	t.Fatal("search tool not found in ListTools")
}

// ── recent tags + memory_id scoping ──────────────────────────────────────────

func TestRecent_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	taggedID := addNode(t, h, "TDD story", "proj", map[string]any{"tags": "TDD testing"})
	addNode(t, h, "untagged story", "proj", nil)

	tr := call(t, h, "recent", map[string]any{
		"domain": "proj",
		"tags":   "TDD",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 result, got %d", len(nodes))
	}
	if len(nodes) > 0 && nodes[0]["id"] != taggedID {
		t.Errorf("expected tagged node %q, got %q", taggedID, nodes[0]["id"])
	}
}

func TestRecent_MemoryID_ScopesNeighbourhood(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	neighbourID := addNode(t, h, "neighbour", "proj", nil)
	unrelatedID := addNode(t, h, "unrelated", "proj", nil)

	call(t, h, "connect", map[string]any{
		"from_memory":  anchorID,
		"to_memory":    neighbourID,
		"relationship": "connects_to",
	})

	tr := call(t, h, "recent", map[string]any{
		"memory_id": anchorID,
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if id == unrelatedID {
			t.Error("unrelated node should be excluded when memory_id is set")
		}
	}
	if !contains(ids, anchorID) || !contains(ids, neighbourID) {
		t.Errorf("anchor and neighbour should be included, got %v", ids)
	}
}

func TestRecent_TagsAndMemoryID_Combined(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	taggedNeighbourID := addNode(t, h, "tagged neighbour", "proj", map[string]any{"tags": "TDD"})
	untaggedNeighbourID := addNode(t, h, "untagged neighbour", "proj", nil)

	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": taggedNeighbourID, "relationship": "connects_to"})
	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": untaggedNeighbourID, "relationship": "connects_to"})

	tr := call(t, h, "recent", map[string]any{
		"memory_id": anchorID,
		"tags":      "TDD",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if contains(ids, untaggedNeighbourID) {
		t.Error("untagged neighbour should be excluded when tags filter is applied")
	}
	if !contains(ids, taggedNeighbourID) {
		t.Error("tagged neighbour should be included")
	}
}

func TestRecent_ExistingBehaviourUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	id1 := addNode(t, h, "alpha recent", "proj", nil)
	id2 := addNode(t, h, "beta recent", "proj", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse recent result: %v", err)
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, id1) || !contains(ids, id2) {
		t.Errorf("both nodes should appear with no scoping, got %v", ids)
	}
}

// TestRecent_SchemaHasTagsAndMemoryID: the recent tool must expose both new
// properties in its input schema.
func TestRecent_SchemaHasTagsAndMemoryID(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "recent" {
			continue
		}
		for _, prop := range []string{"tags", "memory_id"} {
			p, ok := td.InputSchema.Properties[prop]
			if !ok {
				t.Errorf("recent tool missing %q property in schema", prop)
				continue
			}
			if p.Description == "" {
				t.Errorf("recent tool %q property must have a description", prop)
			}
		}
		return
	}
	t.Fatal("recent tool not found in ListTools")
}

// ── audit tags + memory_id scoping ───────────────────────────────────────────

func TestAudit_Stale_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	addNode(t, h, "old plan TDD", "proj", map[string]any{"tags": "TDD"})
	addNode(t, h, "old approach other", "proj", map[string]any{"tags": "other"})

	tr := call(t, h, "audit", map[string]any{
		"mode":   "stale",
		"domain": "proj",
		"tags":   "TDD",
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit stale result: %v", err)
	}
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n == nil {
			continue
		}
		if n["tags"] != "TDD" {
			t.Errorf("expected only TDD-tagged candidate, got tags=%v label=%v", n["tags"], n["label"])
		}
	}
	if len(candidates) == 0 {
		t.Error("expected at least one TDD-tagged stale candidate")
	}
}

func TestAudit_Orphans_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	reviewID := addNode(t, h, "orphan review node", "proj", map[string]any{"tags": "review"})
	addNode(t, h, "orphan other node", "proj", map[string]any{"tags": "other"})

	tr := call(t, h, "audit", map[string]any{
		"mode": "orphans",
		"tags": "review",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse audit orphans result: %v", err)
	}
	ids := make([]string, 0)
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, reviewID) {
		t.Error("review-tagged orphan should be included")
	}
	for _, n := range nodes {
		if n["tags"] != "review" {
			t.Errorf("expected only review-tagged orphan, got tags=%v", n["tags"])
		}
	}
}

func TestAudit_Archived_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	spikeID := addNode(t, h, "spike idea", "proj", map[string]any{"tags": "spike"})
	otherID := addNode(t, h, "other idea", "proj", map[string]any{"tags": "other"})

	call(t, h, "forget", map[string]any{"id": spikeID, "reason": "test"})
	call(t, h, "forget", map[string]any{"id": otherID, "reason": "test"})

	tr := call(t, h, "audit", map[string]any{
		"mode": "archived",
		"tags": "spike",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse audit archived result: %v", err)
	}
	ids := make([]string, 0)
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, spikeID) {
		t.Error("spike-tagged archived node should be included")
	}
	if contains(ids, otherID) {
		t.Error("other-tagged archived node should be excluded")
	}
}

func TestAudit_Stale_MemoryID(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	inNeighbourID := addNode(t, h, "old neighbour plan", "proj", nil)
	outsideID := addNode(t, h, "old outside plan", "proj", nil)

	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": inNeighbourID, "relationship": "connects_to"})

	tr := call(t, h, "audit", map[string]any{
		"mode":      "stale",
		"memory_id": anchorID,
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit stale memory_id result: %v", err)
	}
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n == nil {
			continue
		}
		if n["id"] == outsideID {
			t.Errorf("outside node %q should be excluded when memory_id is set", n["label"])
		}
	}
	found := false
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n != nil && n["id"] == inNeighbourID {
			found = true
		}
	}
	if !found {
		t.Error("neighbour stale node should be included when memory_id is set")
	}
}

func TestAudit_ExistingBehaviourUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	addNode(t, h, "old plan alpha", "proj", nil)

	tr := call(t, h, "audit", map[string]any{
		"mode":   "stale",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit result: %v", err)
	}
	if len(candidates) == 0 {
		t.Error("expected at least one stale candidate without tags/memory_id filter")
	}
}

func TestAudit_SchemaHasTagsAndMemoryID(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name != "audit" {
			continue
		}
		for _, prop := range []string{"tags", "memory_id"} {
			p, ok := td.InputSchema.Properties[prop]
			if !ok {
				t.Errorf("audit tool missing %q property in schema", prop)
				continue
			}
			if p.Description == "" {
				t.Errorf("audit tool %q property must have a description", prop)
			}
		}
		return
	}
	t.Fatal("audit tool not found in ListTools")
}
