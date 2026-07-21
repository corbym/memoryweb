package tools_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

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

func TestWhyConnected_FromIDToID_ReturnsDirectEdges(t *testing.T) {
	_, h := newEnv(t)
	to := addNode(t, h, "Target memory", "deep-game", nil)
	from := addNode(t, h, "Source memory", "deep-game", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": from, "to_memory": to, "relationship": "depends_on",
		"narrative": "source depends on target",
	}))

	tr := call(t, h, "why_connected", map[string]any{"from_id": from, "to_id": to})
	mustNotError(t, tr)

	var result struct {
		From  map[string]any   `json:"from"`
		To    map[string]any   `json:"to"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.From["id"] != from || result.To["id"] != to {
		t.Errorf("expected exact nodes %q -> %q; got %v -> %v", from, to, result.From["id"], result.To["id"])
	}
	if len(result.Edges) == 0 {
		t.Error("expected at least one direct edge")
	}
}

func TestWhyConnected_UnknownIDErrors(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Known node", "deep-game", nil)

	tr := call(t, h, "why_connected", map[string]any{
		"from_id": id, "to_id": "no-such-id-abcdef12",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "no-such-id-abcdef12") {
		t.Errorf("error should name missing id; got: %s", text(t, tr))
	}
}

func TestWhyConnected_IDAndLabelConflictErrors(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Some node", "deep-game", nil)

	tr := call(t, h, "why_connected", map[string]any{
		"from_id": id, "from_label": "Some node", "to_label": "other",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "from_id") || !strings.Contains(text(t, tr), "from_label") {
		t.Errorf("expected from_id/from_label conflict error; got: %s", text(t, tr))
	}
}

func TestWhyConnected_MixedIDAndLabel(t *testing.T) {
	_, h := newEnv(t)
	to := addNode(t, h, "Mixed target", "deep-game", nil)
	from := addNode(t, h, "Mixed source", "deep-game", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": from, "to_memory": to, "relationship": "connects_to",
	}))

	tr := call(t, h, "why_connected", map[string]any{
		"from_id": from, "to_label": "Mixed target", "domain": "deep-game",
	})
	mustNotError(t, tr)

	var result struct {
		From map[string]any `json:"from"`
		To   map[string]any `json:"to"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.From["id"] != from || result.To["id"] != to {
		t.Errorf("mixed id/label resolution failed: got %v -> %v", result.From["id"], result.To["id"])
	}
}

func TestWhyConnected_IDDoesNotFallbackToLabel(t *testing.T) {
	_, h := newEnv(t)
	labelMatch := addNode(t, h, "Label match node", "deep-game", nil)
	addNode(t, h, "Other node", "deep-game", nil)

	tr := call(t, h, "why_connected", map[string]any{
		"from_id": "wrong-id-12345678", "to_id": labelMatch,
	})
	mustError(t, tr)
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

func TestWhyConnected_StrictDecode_RejectsUnknownFields(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "why_connected", map[string]any{
		"from_label": "a", "to_label": "b", "bogus": true,
	})
	mustError(t, tr)
}

func TestOrient_ResultsTruncatedWhenRecentExceedsCap(t *testing.T) {
	_, h := newEnv(t)
	domain := "orient-trunc-recent"
	for i := 0; i < 7; i++ {
		addNode(t, h, fmt.Sprintf("Recent node %d", i), domain, nil)
	}
	tr := call(t, h, "orient", map[string]any{"domain": domain})
	mustNotError(t, tr)
	var resp struct {
		RecentResultsTruncated bool `json:"recent_results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient: %v", err)
	}
	if !resp.RecentResultsTruncated {
		t.Error("recent_results_truncated should be true when domain has more than 5 recent updates")
	}
}

func TestSignificance_DeclaredResultsTruncated(t *testing.T) {
	_, h := newEnv(t)
	domain := "sig-trunc-declared"
	for i := 0; i < 3; i++ {
		addNode(t, h, fmt.Sprintf("Declared %d", i), domain, map[string]any{
			"occurred_at": fmt.Sprintf("2026-01-%02d", i+1),
			"why_matters": "declared spine entry",
		})
	}
	tr := call(t, h, "significance", map[string]any{"domain": domain, "declared_limit": 2})
	mustNotError(t, tr)
	var resp struct {
		Declared                 []any `json:"declared"`
		DeclaredResultsTruncated bool  `json:"declared_results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse significance: %v", err)
	}
	if len(resp.Declared) != 2 {
		t.Errorf("declared_limit 2: want 2 entries, got %d", len(resp.Declared))
	}
	if !resp.DeclaredResultsTruncated {
		t.Error("declared_results_truncated should be true")
	}
}

func TestAudit_Stale_ResultsTruncated(t *testing.T) {
	_, h := newEnv(t)
	domain := "audit-trunc-stale"
	for i := 0; i < 3; i++ {
		addNode(t, h, fmt.Sprintf("duplicate label %d", i), domain, nil)
	}
	addNode(t, h, "duplicate label", domain, nil)
	addNode(t, h, "duplicate label", domain, nil)

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": domain, "limit": 1})
	mustNotError(t, tr)
	var resp struct {
		Candidates       []any `json:"candidates"`
		ResultsTruncated bool  `json:"results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse audit stale: %v", err)
	}
	if len(resp.Candidates) != 1 {
		t.Errorf("limit 1: want 1 candidate, got %d", len(resp.Candidates))
	}
	if !resp.ResultsTruncated {
		t.Error("results_truncated should be true")
	}
}

func TestSignificance_RaiseDeclaredLimitReturnsMore(t *testing.T) {
	_, h := newEnv(t)
	domain := "sig-raise-declared"
	for i := 0; i < 4; i++ {
		addNode(t, h, fmt.Sprintf("Declared raise %d", i), domain, map[string]any{
			"occurred_at": fmt.Sprintf("2026-02-%02d", i+1),
			"why_matters": "declared entry",
		})
	}

	trLow := call(t, h, "significance", map[string]any{"domain": domain, "declared_limit": 2})
	mustNotError(t, trLow)
	var low struct {
		Declared                 []any `json:"declared"`
		DeclaredResultsTruncated bool  `json:"declared_results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, trLow)), &low); err != nil {
		t.Fatalf("parse low: %v", err)
	}
	if len(low.Declared) != 2 || !low.DeclaredResultsTruncated {
		t.Fatalf("declared_limit 2: want 2 truncated entries, got %d truncated=%v", len(low.Declared), low.DeclaredResultsTruncated)
	}

	trHigh := call(t, h, "significance", map[string]any{"domain": domain, "declared_limit": 4})
	mustNotError(t, trHigh)
	var high struct {
		Declared                 []any `json:"declared"`
		DeclaredResultsTruncated bool  `json:"declared_results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, trHigh)), &high); err != nil {
		t.Fatalf("parse high: %v", err)
	}
	if len(high.Declared) <= len(low.Declared) {
		t.Errorf("raising declared_limit should return more entries: low=%d high=%d", len(low.Declared), len(high.Declared))
	}
	if high.DeclaredResultsTruncated {
		t.Error("declared_results_truncated should be false when limit covers all entries")
	}
}

func TestSignificance_RaiseStructuralLimitReturnsMore(t *testing.T) {
	_, h := newEnv(t)
	domain := "sig-raise-structural"
	for i := 0; i < 4; i++ {
		target := addNode(t, h, fmt.Sprintf("Structural target %d", i), domain, nil)
		source := addNode(t, h, fmt.Sprintf("Structural source %d", i), domain, nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": source, "to_memory": target, "relationship": "depends_on",
		}))
	}

	trLow := call(t, h, "significance", map[string]any{"domain": domain, "limit": 1})
	mustNotError(t, trLow)
	var low struct {
		Structural                 []any `json:"structural"`
		StructuralResultsTruncated bool  `json:"structural_results_truncated"`
	}
	json.Unmarshal([]byte(text(t, trLow)), &low)
	if len(low.Structural) != 1 || !low.StructuralResultsTruncated {
		t.Fatalf("limit 1: want 1 truncated structural entry, got %d truncated=%v", len(low.Structural), low.StructuralResultsTruncated)
	}

	trHigh := call(t, h, "significance", map[string]any{"domain": domain, "limit": 5})
	mustNotError(t, trHigh)
	var high struct {
		Structural                 []any `json:"structural"`
		StructuralResultsTruncated bool  `json:"structural_results_truncated"`
	}
	json.Unmarshal([]byte(text(t, trHigh)), &high)
	if len(high.Structural) <= len(low.Structural) {
		t.Errorf("raising limit should return more structural entries: low=%d high=%d", len(low.Structural), len(high.Structural))
	}
}

func TestSignificance_UncuratedResultsTruncatedMirrorsStructural(t *testing.T) {
	_, h := newEnv(t)
	domain := "sig-uncurated-trunc"
	for i := 0; i < 4; i++ {
		target := addNode(t, h, fmt.Sprintf("Uncurated target %d", i), domain, nil)
		source := addNode(t, h, fmt.Sprintf("Uncurated source %d", i), domain, nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": source, "to_memory": target, "relationship": "depends_on",
		}))
	}

	tr := call(t, h, "significance", map[string]any{"domain": domain, "limit": 1})
	mustNotError(t, tr)
	var resp struct {
		StructuralResultsTruncated bool `json:"structural_results_truncated"`
		UncuratedResultsTruncated  bool `json:"uncurated_results_truncated"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if !resp.StructuralResultsTruncated {
		t.Fatal("structural_results_truncated should be true")
	}
	if resp.UncuratedResultsTruncated != resp.StructuralResultsTruncated {
		t.Errorf("uncurated_results_truncated should mirror structural: uncurated=%v structural=%v",
			resp.UncuratedResultsTruncated, resp.StructuralResultsTruncated)
	}
}

func TestOrient_CrossDomain_ResultsTruncated(t *testing.T) {
	_, h := newEnv(t)
	domain := "orient-cross-trunc"
	for i := 0; i < 8; i++ {
		addNode(t, h, fmt.Sprintf("Cross domain node %d", i), domain, nil)
	}

	tr := call(t, h, "orient", map[string]any{})
	mustNotError(t, tr)
	var resp struct {
		ResultsTruncated bool `json:"results_truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse cross-domain orient: %v", err)
	}
	if !resp.ResultsTruncated {
		t.Error("results_truncated should be true when a domain has more than 5 recent updates")
	}
}

func TestOrient_RulesResultsTruncatedFalseIsPresent(t *testing.T) {
	_, h := newEnv(t)
	domain := "orient-rules-false"
	addNode(t, h, "Only node", domain, nil)

	tr := call(t, h, "orient", map[string]any{"domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, `"rules_results_truncated": false`) {
		t.Errorf("rules_results_truncated:false should be present in JSON; got:\n%s", body)
	}
}

// ── disconnect ────────────────────────────────────────────────────────────────

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
