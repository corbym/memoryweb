package tools_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/stats"
	"github.com/corbym/memoryweb/tools"
)

func TestInstructions_NonEmpty(t *testing.T) {
	if tools.Instructions == "" {
		t.Fatal("Instructions must be non-empty")
	}
}

// TestInstructions_CredentialsAdvisory: Instructions must tell agents never to
// file credentials or API keys in memories.

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

// listToolsDesc holds parsed ListTools output for description assertions.
type listToolsDesc struct {
	Name        string
	Description string
	Properties  map[string]struct {
		Description string `json:"description"`
	}
}

// parseListToolsForDescriptions unmarshals ListTools into a slice suitable for
// description and property-description assertions.
func parseListToolsForDescriptions(t *testing.T, h *tools.Handler) []listToolsDesc {
	t.Helper()
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
	out := make([]listToolsDesc, len(resp.Tools))
	for i, td := range resp.Tools {
		out[i] = listToolsDesc{
			Name:        td.Name,
			Description: td.Description,
			Properties:  td.InputSchema.Properties,
		}
	}
	return out
}

func toolDescription(t *testing.T, tools []listToolsDesc, name string) string {
	t.Helper()
	for _, td := range tools {
		if td.Name == name {
			return td.Description
		}
	}
	t.Fatalf("tool %q not found in ListTools", name)
	return ""
}

func toolPropertyDescription(t *testing.T, tools []listToolsDesc, toolName, propName string) string {
	t.Helper()
	for _, td := range tools {
		if td.Name != toolName {
			continue
		}
		prop, ok := td.Properties[propName]
		if !ok {
			t.Fatalf("tool %q has no property %q", toolName, propName)
		}
		return prop.Description
	}
	t.Fatalf("tool %q not found in ListTools", toolName)
	return ""
}

// TestOrient_NoDomain_ReturnsCrossDomainSnapshot: calling orient with no domain
// must return mode="cross_domain_snapshot" with a domains array containing at
// least one entry that has domain and recent fields.

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

func TestListTools_RememberDescriptionContainsNewDomainWarning(t *testing.T) {
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
			if !strings.Contains(td.Description, "Creating a new domain hides this memory") {
				t.Error(`remember description missing new-domain discoverability warning`)
			}
			return
		}
	}
	t.Fatal("remember tool not found in ListTools")
}

func TestListTools_RememberDescriptionContainsConflictFraming(t *testing.T) {
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
			if !strings.Contains(td.Description, "contradiction as well as relevance") {
				t.Error(`remember description missing suggested_connections conflict framing`)
			}
			if !strings.Contains(td.Description, "audit(mode=conflicts) is a separate domain-wide sweep") {
				t.Error(`remember description must distinguish filing-time check from audit(mode=conflicts)`)
			}
			return
		}
	}
	t.Fatal("remember tool not found in ListTools")
}

func TestListTools_RememberDescriptionContainsFindingLinkback(t *testing.T) {
	_, h := newEnv(t)
	tools := parseListToolsForDescriptions(t, h)
	desc := toolDescription(t, tools, "remember")
	if !strings.Contains(desc, "file that evidence separately as node_kind='finding'") {
		t.Error(`remember description missing decision→finding linkback instruction`)
	}
	if !strings.Contains(desc, "depends_on or caused_by") {
		t.Error(`remember description missing depends_on or caused_by linkback instruction`)
	}
	linkbackIdx := strings.Index(desc, "file that evidence separately as node_kind='finding'")
	singleModeIdx := strings.Index(desc, "Single mode")
	if linkbackIdx == -1 || singleModeIdx == -1 || linkbackIdx > singleModeIdx {
		t.Error(`remember decision→finding linkback must appear before Single mode section`)
	}
}

func TestListTools_RememberItemsPropertyContainsFindingLinkback(t *testing.T) {
	_, h := newEnv(t)
	tools := parseListToolsForDescriptions(t, h)
	desc := toolPropertyDescription(t, tools, "remember", "items")
	if !strings.Contains(desc, "depends_on or caused_by") {
		t.Error(`remember items property missing decision→finding linkback instruction`)
	}
}

func TestListTools_ReviseItemsPropertyWarnsAgainstEvidenceFold(t *testing.T) {
	_, h := newEnv(t)
	tools := parseListToolsForDescriptions(t, h)
	desc := toolPropertyDescription(t, tools, "revise", "items")
	if !strings.Contains(desc, "Do not paste new source material into description") {
		t.Error(`revise items property missing warning against folding evidence into description`)
	}
}

func TestListTools_ReviseDescriptionWarnsAgainstEvidenceFold(t *testing.T) {
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
		if td.Name == "revise" {
			if !strings.Contains(td.Description, "do not paste new source material") {
				t.Error(`revise description missing warning against folding evidence into description`)
			}
			return
		}
	}
	t.Fatal("revise tool not found in ListTools")
}

// ── revise: transient field ───────────────────────────────────────────────────

// TestRevise_TransientUpdatable covers all transient field scenarios via revise:
// clearing the flag, setting it, leaving it unchanged, batch mode, and edge preservation.

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

// ── lean-format retrieval tools (search, recent, significance, history) ───────
//
// Extends orient's lean-entry pattern (id, label, truncated why_matters,
// occurred_at where set; description and tags omitted) to the four other
// list-shaped retrieval tools. See stories/lean-format-retrieval-tools.md.
