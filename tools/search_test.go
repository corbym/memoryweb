package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestSearchNodes_EmptyQueryRejects(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node Alpha", "project-x", nil)

	tr := call(t, h, "search", map[string]any{
		"query": "", "domain": "project-x", "limit": 10,
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "query is required") {
		t.Errorf("expected query required error, got: %s", text(t, tr))
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
