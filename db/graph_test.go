package db_test

import (
	"os"
	"strings"
	"testing"
)

// disableOllamaDB sets MEMORYWEB_OLLAMA_ENDPOINT=disabled for the duration of
// the test. Use this in db-layer tests that exercise keyword-only paths in
// SuggestEdges and SearchNodes so that a running Ollama instance does not
// activate the embedding path and interfere with expected results.
func disableOllamaDB(t *testing.T) {
	t.Helper()
	prev := os.Getenv("MEMORYWEB_OLLAMA_ENDPOINT")
	os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
	t.Cleanup(func() { os.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", prev) })
}

func TestFindConnections_ReturnsBidirectionalEdge(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "Boot crash", "proj")
	b := mustAddNode(t, s, "ULA fix", "proj")
	s.AddEdge(a.ID, b.ID, "unblocks", "direct writes")

	res, err := s.FindConnections("Boot crash", "ULA fix", "proj")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.From == nil || res.To == nil {
		t.Fatal("expected non-nil From and To")
	}
	if len(res.Edges) == 0 {
		t.Error("expected at least one edge")
	}
}

func TestFindConnections_NoMatchReturnsNilNodes(t *testing.T) {
	s := newStore(t)
	res, err := s.FindConnections("ghost-a", "ghost-b", "")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.From != nil || res.To != nil {
		t.Error("no match should give nil From/To")
	}
}

func TestFindConnections_ArchivedNodeNotMatched(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Visible node", "proj")
	archived := mustAddNode(t, s, "Archived node", "proj")
	s.ArchiveNode(archived.ID, "reason")
	s.AddEdge(n.ID, archived.ID, "connects_to", "link")

	res, err := s.FindConnections("Visible node", "Archived node", "proj")
	if err != nil {
		t.Fatalf("FindConnections: %v", err)
	}
	if res.To != nil {
		t.Error("archived node should not be matched by FindConnections")
	}
}

func TestSuggestEdges_ReturnsOverlappingTagsNode(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("sprint ticket alpha", "d", "w", "proj", nil, "kotlin testing", "")
	nB, _ := s.AddNode("sprint ticket beta", "d", "w", "proj", nil, "kotlin approval", "")
	mustAddNode(t, s, "completely unrelated thing", "proj") // no overlap

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	found := false
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			found = true
			if !strings.Contains(sg.Reason, "kotlin") {
				t.Errorf("reason should mention 'kotlin'; got %q", sg.Reason)
			}
		}
	}
	if !found {
		t.Errorf("node B (%s) should appear in suggestions for overlapping tag 'kotlin'", nB.ID)
	}
}

func TestSuggestEdges_ExcludesSelf(t *testing.T) {
	s := newStore(t)
	n, _ := s.AddNode("self test node kotlin", "d", "w", "proj", nil, "kotlin", "")
	mustAddNode(t, s, "kotlin partner node", "proj") // gives a keyword match

	suggestions, err := s.SuggestEdges(n.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == n.ID {
			t.Error("SuggestEdges should not include the source node itself")
		}
	}
}

func TestSuggestEdges_DomainScoping_DB(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("kotlin build system", "d", "w", "domain-a", nil, "kotlin gradle", "")
	s.AddNode("kotlin build tool", "d", "w", "domain-b", nil, "kotlin gradle", "") // different domain
	nC, _ := s.AddNode("kotlin runner", "d", "w", "domain-a", nil, "kotlin testing", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	ids := make([]string, len(suggestions))
	for i, sg := range suggestions {
		ids[i] = sg.ID
	}
	// domain-a node should appear
	foundC := false
	for _, id := range ids {
		if id == nC.ID {
			foundC = true
		}
	}
	if !foundC {
		t.Errorf("same-domain node (%s) should appear in suggestions; got %v", nC.ID, ids)
	}
}

// TestSuggestEdges_EmDashNotMatchedAsSharedWord: two labels whose only common
// character is an em-dash must not be suggested as connected — the em-dash is
// not a word. Ollama is disabled so only the keyword path runs.
func TestSuggestEdges_EmDashNotMatchedAsSharedWord(t *testing.T) {
	disableOllamaDB(t)
	s := newStore(t)
	nA, _ := s.AddNode("Alpha — gizmo", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta — widget", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			t.Errorf("em-dash must not be treated as a shared word; got suggestion %+v", sg)
		}
	}
}

// TestSuggestEdges_EnDashAndEllipsisNotMatchedAsSharedWord: same shape as the
// em-dash case, using en-dash and ellipsis — any standalone punctuation/symbol
// token must be excluded, not just the em-dash. Ollama is disabled so only the
// keyword path runs.
func TestSuggestEdges_EnDashAndEllipsisNotMatchedAsSharedWord(t *testing.T) {
	disableOllamaDB(t)
	s := newStore(t)
	nA, _ := s.AddNode("Alpha – gizmo…", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta – widget…", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			t.Errorf("en-dash/ellipsis must not be treated as shared words; got suggestion %+v", sg)
		}
	}
}

// TestSuggestEdges_RealWordsStillMatch: regression guard — a genuine shared
// label word must still produce a suggestion with that word in the reason.
func TestSuggestEdges_RealWordsStillMatch(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("Alpha gizmo widget", "d", "w", "proj", nil, "", "")
	nB, _ := s.AddNode("Beta gizmo thing", "d", "w", "proj", nil, "", "")

	suggestions, err := s.SuggestEdges(nA.ID, 5)
	if err != nil {
		t.Fatalf("SuggestEdges: %v", err)
	}
	found := false
	for _, sg := range suggestions {
		if sg.ID == nB.ID {
			found = true
			if !strings.Contains(sg.Reason, "gizmo") {
				t.Errorf("reason should mention 'gizmo'; got %q", sg.Reason)
			}
		}
	}
	if !found {
		t.Errorf("node B (%s) sharing a real word should appear in suggestions", nB.ID)
	}
}
