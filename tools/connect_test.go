package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestConnect_GovernedBy(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	ruleID := addNode(t, h, "the standing rule", "proj", map[string]any{"node_kind": "standing", "why_matters": "a rule"})
	workID := addNode(t, h, "some work item", "proj", map[string]any{})

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  workID,
		"to_memory":    ruleID,
		"relationship": "governed_by",
		"narrative":    "this work follows the standing rule",
	})
	mustNotError(t, tr)
}

// ── connect error message quality ────────────────────────────────────────────

// TestConnect_CrossDomain_ValidIDs_Succeeds is the regression test ensuring
// that cross-domain connect works when both IDs are valid.
func TestConnect_CrossDomain_ValidIDs_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "alpha node", "domain-alpha", nil)
	to := addNode(t, h, "beta node", "domain-beta", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
		"relationship": "depends_on",
		"narrative":    "alpha depends on beta across domains",
	})
	mustNotError(t, tr)
}

// TestConnect_MissingToMemory_NoMentionOfCrossDomain asserts that when
// to_memory is not found, the error does NOT say cross-domain is unsupported.
func TestConnect_MissingToMemory_NoMentionOfCrossDomain(t *testing.T) {
	_, h := newEnv(t)
	from := addNode(t, h, "source node", "src-domain", nil)

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    "totally-nonexistent-id",
		"relationship": "connects_to",
	})
	mustError(t, tr)
	msg := text(t, tr)
	if strings.Contains(msg, "cross-domain") {
		t.Errorf("error message must not mention cross-domain for a missing ID;\ngot: %s", msg)
	}
	if strings.Contains(msg, "not yet supported") {
		t.Errorf("error message must not say 'not yet supported';\ngot: %s", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("error message should mention 'not found';\ngot: %s", msg)
	}
}

// TestConnect_ArchivedToMemory_MentionsRestore asserts that when to_memory is
// archived, the error specifically mentions "restore" so the agent can recover.
func TestConnect_ArchivedToMemory_MentionsRestore(t *testing.T) {
	s, h := newEnv(t)
	from := addNode(t, h, "live node", "proj", nil)
	to := addNode(t, h, "archived node", "proj", nil)

	// Archive the target node via the DB layer.
	if err := s.ArchiveNode(to, "test archival"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  from,
		"to_memory":    to,
		"relationship": "connects_to",
	})
	mustError(t, tr)
	msg := text(t, tr)
	if !strings.Contains(msg, "restore") {
		t.Errorf("error message should mention 'restore' for archived target;\ngot: %s", msg)
	}
}

// ── search memory_id scoping ──────────────────────────────────────────────────

// TestSearch_MemoryID_ScopesResults: when memory_id is supplied, only nodes in
// the depth-2 neighbourhood of the anchor appear in results.
