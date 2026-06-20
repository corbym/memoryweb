package db_test

import "testing"

func TestSearchNodes_MatchesLabel(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "ULA memory write fix", "deep-game")

	res, err := s.SearchNodes("ULA", "deep-game", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) == 0 {
		t.Fatal("expected at least one result")
	}
	if res.Nodes[0].ID != n.ID {
		t.Errorf("got node %q, want %q", res.Nodes[0].ID, n.ID)
	}
}

func TestSearchNodes_ExcludesArchivedNodes(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "searchable node", "proj")
	s.ArchiveNode(n.ID, "reason")

	res, err := s.SearchNodes("searchable", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			t.Error("archived node should not appear in search results")
		}
	}
}

func TestSearchNodes_DomainFilter(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "shared label", "domain-a")
	mustAddNode(t, s, "shared label", "domain-b")

	res, err := s.SearchNodes("shared label", "domain-a", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) != 1 || res.Nodes[0].ID != nA.ID {
		t.Errorf("domain filter: got %+v", res.Nodes)
	}
}

func TestSearchNodes_EmptyQueryReturnsAll(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "proj")
	mustAddNode(t, s, "Beta", "proj")
	mustAddNode(t, s, "Gamma", "proj")

	res, err := s.SearchNodes("", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) != 3 {
		t.Errorf("expected 3 results for empty query, got %d", len(res.Nodes))
	}
}

func TestSearchNodes_LimitIsRespected(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 5; i++ {
		mustAddNode(t, s, "limit test", "proj")
	}
	res, err := s.SearchNodes("limit test", "proj", 3, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) > 3 {
		t.Errorf("limit 3 exceeded: got %d", len(res.Nodes))
	}
}

func TestSearchNodes_TruncatedFlagSetWhenLimitExceeded(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 5; i++ {
		mustAddNode(t, s, "truncation test", "proj")
	}
	res, err := s.SearchNodes("truncation test", "proj", 3, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) != 3 {
		t.Errorf("expected 3 results, got %d", len(res.Nodes))
	}
	if !res.Truncated {
		t.Error("Truncated should be true when results are capped by limit")
	}
}

func TestSearchNodes_TruncatedFlagNotSetWhenUnderLimit(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 3; i++ {
		mustAddNode(t, s, "truncation under", "proj")
	}
	res, err := s.SearchNodes("truncation under", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if res.Truncated {
		t.Error("Truncated should be false when results are under the limit")
	}
}

func TestSearchNodes_includesEdgesBetweenResults(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "alpha edge test", "proj")
	b := mustAddNode(t, s, "beta edge test", "proj")
	s.AddEdge(a.ID, b.ID, "connects_to", "they relate")

	res, err := s.SearchNodes("edge test", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Edges) == 0 {
		t.Error("edges between result nodes should be included")
	}
}

// TestSearchNodes_MultiWordFallback: when the full phrase isn't a substring of
// any field but each individual word IS, the fallback should find the node.
func TestSearchNodes_MultiWordFallback_WordsSpreadAcrossFields(t *testing.T) {
	s := newStore(t)
	// Full phrase "testing approval parameterised" does not appear contiguously
	// in any single field, but each word appears in a different field.
	n, err := s.AddNode(
		"testing scaffold",  // label:       contains "testing"
		"approval required", // description: contains "approval"
		"why it matters",    // why_matters: no match
		"proj",
		nil,
		"parameterised kotlin", // tags: contains "parameterised"
		"",
	)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	res, err := s.SearchNodes("testing approval parameterised", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	found := false
	for _, nd := range res.Nodes {
		if nd.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("node not found via multi-word fallback search")
	}
}

// TestSearchNodes_SingleWord_BehaviourUnchanged: a single-word query that
// directly matches still returns results — fallback does not interfere.
func TestSearchNodes_SingleWord_BehaviourUnchanged(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "ULA memory write fix", "proj")

	res, err := s.SearchNodes("ULA", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	found := false
	for _, nd := range res.Nodes {
		if nd.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("single-word query should still find node via primary search")
	}
}

// TestSearchNodes_MultiWordFallback_NoDomain: fallback also works without a
// domain filter (cross-domain search).
func TestSearchNodes_MultiWordFallback_NoDomain(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode(
		"kotlin testing",    // label
		"approval workflow", // description
		"why",
		"proj-a",
		nil,
		"parameterised",
		"",
	)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// No domain filter — should still hit fallback path.
	res, err := s.SearchNodes("testing approval parameterised", "", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	found := false
	for _, nd := range res.Nodes {
		if nd.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("multi-word fallback should work without domain filter")
	}
}

func TestSearchNodesLike_MemoryID_NeighbourhoodOnly(t *testing.T) {
	s := newStore(t)

	// anchor — connected to neighbour
	anchor := mustAddNode(t, s, "anchor node", "proj")
	neighbour := mustAddNode(t, s, "arch neighbour", "proj")
	unrelated := mustAddNode(t, s, "arch unrelated", "proj")
	s.AddEdge(anchor.ID, neighbour.ID, "connects_to", "")

	res, err := s.SearchNodes("arch", "proj", 10, anchor.ID)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	for _, nr := range res.Nodes {
		if nr.ID == unrelated.ID {
			t.Error("unrelated node (not in neighbourhood) should be excluded")
		}
	}
	found := false
	for _, nr := range res.Nodes {
		if nr.ID == neighbour.ID {
			found = true
		}
	}
	if !found {
		t.Error("neighbour node should be included in scoped results")
	}
}

func TestSearchNodes_MemoryID_EmptyFallsBackToNormal(t *testing.T) {
	s := newStore(t)

	mustAddNode(t, s, "arch alpha", "proj")
	mustAddNode(t, s, "arch beta", "proj")

	res, err := s.SearchNodes("arch", "proj", 10, "")
	if err != nil {
		t.Fatalf("SearchNodes with empty memoryID: %v", err)
	}
	if len(res.Nodes) != 2 {
		t.Errorf("expected 2 results with no memory_id filter, got %d", len(res.Nodes))
	}
}
