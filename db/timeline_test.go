package db_test

import (
	"testing"
	"time"
)

func TestRecentChanges_MostRecentFirst(t *testing.T) {
	s := newStore(t)
	n1 := mustAddNode(t, s, "First", "proj")
	n2 := mustAddNode(t, s, "Second", "proj")

	nodes, err := s.RecentChanges("proj", 10, nil)
	if err != nil {
		t.Fatalf("RecentChanges: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatal("expected at least 2 nodes")
	}
	// updated_at DESC — second added is most recent
	if nodes[0].ID != n2.ID {
		t.Errorf("most recent should be %q, got %q", n2.ID, nodes[0].ID)
	}
	_ = n1
}

func TestRecentChanges_ExcludesArchived(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "will be archived", "proj")
	mustAddNode(t, s, "stays live", "proj")
	s.ArchiveNode(n.ID, "reason")

	nodes, err := s.RecentChanges("proj", 10, nil)
	if err != nil {
		t.Fatalf("RecentChanges: %v", err)
	}
	for _, node := range nodes {
		if node.ID == n.ID {
			t.Error("archived node in recent_changes")
		}
	}
}

func TestRecentChanges_NoDomain_AllLiveNodes(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "A", "domain-a")
	mustAddNode(t, s, "B", "domain-b")

	nodes, err := s.RecentChanges("", 10, nil)
	if err != nil {
		t.Fatalf("RecentChanges: %v", err)
	}
	if len(nodes) < 2 {
		t.Errorf("expected >= 2 nodes across domains, got %d", len(nodes))
	}
}

func TestTimeline_AscendingOrder(t *testing.T) {
	s := newStore(t)
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	n1, _ := s.AddNode("Early", "d", "w", "proj", ptr(early), "", "")
	n2, _ := s.AddNode("Late", "d", "w", "proj", ptr(late), "", "")

	nodes, err := s.Timeline("proj", false, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(nodes) < 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].ID != n1.ID || nodes[1].ID != n2.ID {
		t.Errorf("order wrong: got [%s, %s]", nodes[0].ID, nodes[1].ID)
	}
}

func TestTimeline_DefaultModeIncludesNullOccurredAt(t *testing.T) {
	// Default mode (importantOnly=false) includes nodes with no occurred_at.
	s := newStore(t)
	noDate := mustAddNode(t, s, "no date", "proj")
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dated, _ := s.AddNode("dated", "d", "w", "proj", ptr(ts), "", "")

	nodes, err := s.Timeline("proj", false, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var foundNoDate, foundDated bool
	for _, n := range nodes {
		if n.ID == noDate.ID {
			foundNoDate = true
		}
		if n.ID == dated.ID {
			foundDated = true
		}
	}
	if !foundNoDate {
		t.Error("default mode: node without occurred_at should appear in timeline")
	}
	if !foundDated {
		t.Error("default mode: node with occurred_at should appear in timeline")
	}
}

func TestTimeline_ImportantOnlyExcludesNullOccurredAt(t *testing.T) {
	// importantOnly=true excludes nodes without occurred_at.
	s := newStore(t)
	noDate := mustAddNode(t, s, "no date", "proj")
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dated, _ := s.AddNode("dated", "d", "w", "proj", ptr(ts), "", "")

	nodes, err := s.Timeline("proj", true, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	for _, n := range nodes {
		if n.ID == noDate.ID {
			t.Error("important_only mode: node without occurred_at should not appear")
		}
	}
	found := false
	for _, n := range nodes {
		if n.ID == dated.ID {
			found = true
		}
	}
	if !found {
		t.Error("important_only mode: node with occurred_at should appear")
	}
}

func TestTimeline_ExcludesArchived(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	n, _ := s.AddNode("archived event", "d", "w", "proj", ptr(ts), "", "")
	s.ArchiveNode(n.ID, "reason")

	nodes, err := s.Timeline("proj", false, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	for _, node := range nodes {
		if node.ID == n.ID {
			t.Error("archived node should not appear in timeline")
		}
	}
}

func TestTimeline_DateRangeFilter(t *testing.T) {
	s := newStore(t)
	jan := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	s.AddNode("Jan", "d", "w", "proj", ptr(jan), "", "")
	nMar, _ := s.AddNode("Mar", "d", "w", "proj", ptr(mar), "", "")
	s.AddNode("Jun", "d", "w", "proj", ptr(jun), "", "")

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)

	nodes, err := s.Timeline("proj", false, nil, nil, &from, &to, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != nMar.ID {
		t.Errorf("date range filter: got %+v", nodes)
	}
}

func TestTimeline_FromToFiltersByCoalesceDate(t *testing.T) {
	// from/to now uses COALESCE(occurred_at, created_at).
	// A node with no occurred_at should be included if its created_at is in range.
	s := newStore(t)
	// Add a node with no occurred_at (relies on created_at for ordering / filtering).
	undated, _ := s.AddNode("undated recent", "d", "w", "proj", nil, "", "")

	// Use a wide open range to ensure created_at falls inside it.
	from := time.Now().UTC().Add(-time.Hour)
	to := time.Now().UTC().Add(time.Hour)

	nodes, err := s.Timeline("proj", false, nil, nil, &from, &to, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.ID == undated.ID {
			found = true
		}
	}
	if !found {
		t.Error("undated node should appear when its created_at falls within from/to range")
	}
}

func TestTimeline_TagFilter_WholeWordMatch(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	taggedA, _ := s.AddNode("node A", "d", "w", "proj", ptr(ts), "decision architecture", "")
	taggedB, _ := s.AddNode("node B", "d", "w", "proj", ptr(ts), "architecture release", "")
	_, _ = s.AddNode("node C", "d", "w", "proj", ptr(ts), "release", "")
	_, _ = s.AddNode("node D", "d", "w", "proj", ptr(ts), "", "")

	nodes, err := s.Timeline("proj", false, []string{"architecture"}, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	ids := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		ids[n.ID] = true
	}
	if !ids[taggedA.ID] {
		t.Error("node A with 'decision architecture' should match tag 'architecture'")
	}
	if !ids[taggedB.ID] {
		t.Error("node B with 'architecture release' should match tag 'architecture'")
	}
	if ids[taggedA.ID] && ids[taggedB.ID] && len(nodes) > 2 {
		// node C has "release" not "architecture"; node D has no tags — neither should appear
		for _, n := range nodes {
			if n.ID != taggedA.ID && n.ID != taggedB.ID {
				t.Errorf("unexpected node in tag-filtered results: %s (tags: %q)", n.ID, n.Tags)
			}
		}
	}
}

func TestGetHistoryForMemoryID_ReturnsChronological(t *testing.T) {
	s := newStore(t)
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	anchor, _ := s.AddNode("Anchor", "d", "w", "proj", ptr(jan), "", "")
	n1, _ := s.AddNode("March", "d", "w", "proj", ptr(mar), "", "")
	n2, _ := s.AddNode("June", "d", "w", "proj", ptr(jun), "", "")
	s.AddEdge(anchor.ID, n1.ID, "connects_to", "")
	s.AddEdge(anchor.ID, n2.ID, "connects_to", "")

	nodes, err := s.GetHistoryForMemoryID(anchor.ID, 2, false, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("GetHistoryForMemoryID: %v", err)
	}
	if len(nodes) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(nodes))
	}
	// Verify anchor comes first (jan), then n1 (mar), then n2 (jun).
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	anchorIdx, n1Idx, n2Idx := -1, -1, -1
	for i, id := range ids {
		switch id {
		case anchor.ID:
			anchorIdx = i
		case n1.ID:
			n1Idx = i
		case n2.ID:
			n2Idx = i
		}
	}
	if anchorIdx < 0 || n1Idx < 0 || n2Idx < 0 {
		t.Fatalf("not all expected nodes in result: %v", ids)
	}
	if !(anchorIdx < n1Idx && n1Idx < n2Idx) {
		t.Errorf("wrong order: anchor=%d n1=%d n2=%d in %v", anchorIdx, n1Idx, n2Idx, ids)
	}
}

func TestGetHistoryForMemoryID_DomainClipped(t *testing.T) {
	s := newStore(t)
	anchor := mustAddNode(t, s, "Anchor", "proj")
	foreign := mustAddNode(t, s, "Foreign", "other")
	s.AddEdge(anchor.ID, foreign.ID, "connects_to", "")

	nodes, err := s.GetHistoryForMemoryID(anchor.ID, 2, false, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("GetHistoryForMemoryID: %v", err)
	}
	for _, n := range nodes {
		if n.ID == foreign.ID {
			t.Error("foreign-domain node should not appear in memory_id history")
		}
	}
}

func TestGetHistoryForMemoryID_ImportantOnly(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	anchor := mustAddNode(t, s, "Anchor", "proj")
	dated, _ := s.AddNode("Dated", "d", "w", "proj", ptr(ts), "", "")
	undated := mustAddNode(t, s, "Undated", "proj")
	s.AddEdge(anchor.ID, dated.ID, "connects_to", "")
	s.AddEdge(anchor.ID, undated.ID, "connects_to", "")

	nodes, err := s.GetHistoryForMemoryID(anchor.ID, 2, true, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("GetHistoryForMemoryID: %v", err)
	}
	for _, n := range nodes {
		if n.ID == undated.ID {
			t.Error("important_only: undated node should not appear")
		}
		if n.ID == anchor.ID {
			t.Error("important_only: anchor (no occurred_at) should not appear")
		}
	}
	found := false
	for _, n := range nodes {
		if n.ID == dated.ID {
			found = true
		}
	}
	if !found {
		t.Error("important_only: dated node should appear")
	}
}

func TestGetHistoryForMemoryID_TagsFilter(t *testing.T) {
	s := newStore(t)
	anchor := mustAddNode(t, s, "Anchor", "proj")
	tagged, _ := s.AddNode("Tagged", "d", "w", "proj", nil, "mytag", "")
	untagged := mustAddNode(t, s, "Untagged", "proj")
	s.AddEdge(anchor.ID, tagged.ID, "connects_to", "")
	s.AddEdge(anchor.ID, untagged.ID, "connects_to", "")

	nodes, err := s.GetHistoryForMemoryID(anchor.ID, 2, false, []string{"mytag"}, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("GetHistoryForMemoryID: %v", err)
	}
	for _, n := range nodes {
		if n.ID == untagged.ID {
			t.Error("tag filter: untagged node should not appear")
		}
		if n.ID == anchor.ID {
			t.Error("tag filter: anchor (no tag) should not appear")
		}
	}
	found := false
	for _, n := range nodes {
		if n.ID == tagged.ID {
			found = true
		}
	}
	if !found {
		t.Error("tag filter: tagged node should appear")
	}
}

func TestGetHistoryForMemoryID_UnknownMemoryID(t *testing.T) {
	s := newStore(t)
	_, err := s.GetHistoryForMemoryID("nonexistent-id", 2, false, nil, nil, nil, nil, 10)
	if err == nil {
		t.Error("expected error for unknown memory_id, got nil")
	}
}

func TestRecentChangesByTags_MatchesOneTag(t *testing.T) {
	s := newStore(t)
	tagged := mustAddNodeWithTags(t, s, "TDD story", "proj", "TDD testing")
	mustAddNode(t, s, "untagged story", "proj")

	nodes, err := s.RecentChangesScoped("", 2, "proj", []string{"TDD"}, nil, 10)
	if err != nil {
		t.Fatalf("RecentChangesScoped: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != tagged.ID {
		t.Errorf("expected only tagged node, got %+v", nodes)
	}
}

func TestRecentChangesByTags_OR_Semantics(t *testing.T) {
	s := newStore(t)
	a := mustAddNodeWithTags(t, s, "alpha", "proj", "TDD")
	b := mustAddNodeWithTags(t, s, "beta", "proj", "refactor")

	nodes, err := s.RecentChangesScoped("", 2, "proj", []string{"TDD", "refactor"}, nil, 10)
	if err != nil {
		t.Fatalf("RecentChangesScoped: %v", err)
	}
	ids := nodeIDs(nodes)
	if !contains(ids, a.ID) || !contains(ids, b.ID) {
		t.Errorf("expected both nodes, got IDs %v", ids)
	}
}

func TestRecentChangesByTags_DomainScoped(t *testing.T) {
	s := newStore(t)
	inDomain := mustAddNodeWithTags(t, s, "in domain", "proj-a", "TDD")
	mustAddNodeWithTags(t, s, "other domain", "proj-b", "TDD")

	nodes, err := s.RecentChangesScoped("", 2, "proj-a", []string{"TDD"}, nil, 10)
	if err != nil {
		t.Fatalf("RecentChangesScoped: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != inDomain.ID {
		t.Errorf("expected only proj-a node, got %+v", nodes)
	}
}

func TestRecentChangesForMemoryID_NeighbourhoodOnly(t *testing.T) {
	s := newStore(t)
	anchor := mustAddNode(t, s, "anchor", "proj")
	neighbour := mustAddNode(t, s, "neighbour", "proj")
	unrelated := mustAddNode(t, s, "unrelated", "proj")
	s.AddEdge(anchor.ID, neighbour.ID, "connects_to", "")

	nodes, err := s.RecentChangesScoped(anchor.ID, 2, "", nil, nil, 10)
	if err != nil {
		t.Fatalf("RecentChangesScoped: %v", err)
	}
	ids := nodeIDs(nodes)
	for _, id := range ids {
		if id == unrelated.ID {
			t.Error("unrelated node should be excluded")
		}
	}
	if !contains(ids, anchor.ID) || !contains(ids, neighbour.ID) {
		t.Errorf("anchor and neighbour should be included, got %v", ids)
	}
}
