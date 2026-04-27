package db_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newStore(t *testing.T) *db.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := db.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustAddNode(t *testing.T, s *db.Store, label, domain string) *db.Node {
	t.Helper()
	n, err := s.AddNode(label, "desc", "why", domain, nil, "", false)
	if err != nil {
		t.Fatalf("AddNode(%q): %v", label, err)
	}
	return n
}

func ptr(t time.Time) *time.Time { return &t }

// ── AddNode ───────────────────────────────────────────────────────────────────

func TestAddNode_IDContainsSlug(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("RST Boot Crash", "desc", "why", "deep-game", nil, "", false)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if len(n.ID) == 0 {
		t.Fatal("empty ID")
	}
	// slug should appear at the start
	if n.ID[:3] != "rst" {
		t.Errorf("ID should start with slug 'rst', got %q", n.ID)
	}
}

func TestAddNode_WithOccurredAt(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	n, err := s.AddNode("dated node", "d", "w", "proj", &ts, "", false)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if n.OccurredAt == nil {
		t.Fatal("OccurredAt should not be nil")
	}
	if !n.OccurredAt.Equal(ts) {
		t.Errorf("OccurredAt: got %v, want %v", n.OccurredAt, ts)
	}
}

func TestAddNode_ArchivedAtIsNilByDefault(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "fresh node", "proj")
	if n.ArchivedAt != nil {
		t.Errorf("new node should have nil ArchivedAt, got %v", n.ArchivedAt)
	}
}

// ── GetNode ───────────────────────────────────────────────────────────────────

func TestGetNode_HappyPath(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Known fact", "proj")

	nwe, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if nwe.Node.ID != n.ID {
		t.Errorf("got ID %q, want %q", nwe.Node.ID, n.ID)
	}
}

func TestGetNode_NotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.GetNode("does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestGetNode_ArchivedReturnsNotFound(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Soon archived", "proj")

	s.ArchiveNode(n.ID, "reason")
	_, err := s.GetNode(n.ID)
	if err == nil {
		t.Fatal("GetNode should return error for archived node")
	}
}

// ── SearchNodes ───────────────────────────────────────────────────────────────

func TestSearchNodes_MatchesLabel(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "ULA memory write fix", "deep-game")

	res, err := s.SearchNodes("ULA", "deep-game", 10)
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

	res, err := s.SearchNodes("searchable", "proj", 10)
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

	res, err := s.SearchNodes("shared label", "domain-a", 10)
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

	res, err := s.SearchNodes("", "proj", 10)
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
	res, err := s.SearchNodes("limit test", "proj", 3)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Nodes) > 3 {
		t.Errorf("limit 3 exceeded: got %d", len(res.Nodes))
	}
}

func TestSearchNodes_includesEdgesBetweenResults(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "alpha edge test", "proj")
	b := mustAddNode(t, s, "beta edge test", "proj")
	s.AddEdge(a.ID, b.ID, "connects_to", "they relate")

	res, err := s.SearchNodes("edge test", "proj", 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(res.Edges) == 0 {
		t.Error("edges between result nodes should be included")
	}
}

// ── RecentChanges ─────────────────────────────────────────────────────────────

func TestRecentChanges_MostRecentFirst(t *testing.T) {
	s := newStore(t)
	n1 := mustAddNode(t, s, "First", "proj")
	n2 := mustAddNode(t, s, "Second", "proj")

	nodes, err := s.RecentChanges("proj", 10)
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

	nodes, err := s.RecentChanges("proj", 10)
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

	nodes, err := s.RecentChanges("", 10)
	if err != nil {
		t.Fatalf("RecentChanges: %v", err)
	}
	if len(nodes) < 2 {
		t.Errorf("expected >= 2 nodes across domains, got %d", len(nodes))
	}
}

// ── Timeline ──────────────────────────────────────────────────────────────────

func TestTimeline_AscendingOrder(t *testing.T) {
	s := newStore(t)
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	n1, _ := s.AddNode("Early", "d", "w", "proj", ptr(early), "", false)
	n2, _ := s.AddNode("Late", "d", "w", "proj", ptr(late), "", false)

	nodes, err := s.Timeline("proj", nil, nil, 10)
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

func TestTimeline_ExcludesNullOccurredAt(t *testing.T) {
	s := newStore(t)
	noDate := mustAddNode(t, s, "no date", "proj")
	ts := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dated, _ := s.AddNode("dated", "d", "w", "proj", ptr(ts), "", false)

	nodes, err := s.Timeline("proj", nil, nil, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	for _, n := range nodes {
		if n.ID == noDate.ID {
			t.Error("node without occurred_at should not appear in timeline")
		}
	}
	found := false
	for _, n := range nodes {
		if n.ID == dated.ID {
			found = true
		}
	}
	if !found {
		t.Error("dated node should appear in timeline")
	}
}

func TestTimeline_ExcludesArchived(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	n, _ := s.AddNode("archived event", "d", "w", "proj", ptr(ts), "", false)
	s.ArchiveNode(n.ID, "reason")

	nodes, err := s.Timeline("proj", nil, nil, 10)
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
	s.AddNode("Jan", "d", "w", "proj", ptr(jan), "", false)
	nMar, _ := s.AddNode("Mar", "d", "w", "proj", ptr(mar), "", false)
	s.AddNode("Jun", "d", "w", "proj", ptr(jun), "", false)

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)

	nodes, err := s.Timeline("proj", &from, &to, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != nMar.ID {
		t.Errorf("date range filter: got %+v", nodes)
	}
}

// ── FindConnections ───────────────────────────────────────────────────────────

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

// ── ArchiveNode ───────────────────────────────────────────────────────────────

func TestArchiveNode_SetsArchivedAt(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "to be archived", "proj")

	if err := s.ArchiveNode(n.ID, "outdated"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	// ListArchived should now include this node
	archived, err := s.ListArchived("")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	found := false
	for _, a := range archived {
		if a.ID == n.ID {
			found = true
			if a.ArchivedAt == nil {
				t.Error("archived_at should be set")
			}
		}
	}
	if !found {
		t.Error("archived node not returned by ListArchived")
	}
}

func TestArchiveNode_NotFound(t *testing.T) {
	s := newStore(t)
	err := s.ArchiveNode("ghost-id", "reason")
	if err == nil {
		t.Fatal("ArchiveNode on non-existent node should error")
	}
}

func TestArchiveNode_DoubleArchive_IsIdempotent(t *testing.T) {
	// Archiving an already-archived node should not error — it just updates the timestamp.
	s := newStore(t)
	n := mustAddNode(t, s, "double archive", "proj")
	s.ArchiveNode(n.ID, "first time")
	err := s.ArchiveNode(n.ID, "second time")
	if err != nil {
		t.Errorf("double archive should not error: %v", err)
	}
}

// ── RestoreNode ───────────────────────────────────────────────────────────────

func TestRestoreNode_ReappearsInSearch(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "restore me", "proj")
	s.ArchiveNode(n.ID, "reason")

	// hidden
	res, _ := s.SearchNodes("restore me", "proj", 10)
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			t.Fatal("should be hidden before restore")
		}
	}

	if err := s.RestoreNode(n.ID); err != nil {
		t.Fatalf("RestoreNode: %v", err)
	}

	// visible again
	res, _ = s.SearchNodes("restore me", "proj", 10)
	found := false
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("restored node should reappear in search")
	}
}

func TestRestoreNode_RemovedFromListArchived(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "temporarily archived", "proj")
	s.ArchiveNode(n.ID, "reason")
	s.RestoreNode(n.ID)

	archived, err := s.ListArchived("")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	for _, a := range archived {
		if a.ID == n.ID {
			t.Error("restored node should not be in ListArchived")
		}
	}
}

func TestRestoreNode_NotFound(t *testing.T) {
	s := newStore(t)
	err := s.RestoreNode("ghost-id")
	if err == nil {
		t.Fatal("RestoreNode on non-existent node should error")
	}
}

// ── ListArchived ──────────────────────────────────────────────────────────────

func TestListArchived_DomainFilter(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "archived in A", "domain-a")
	nB := mustAddNode(t, s, "archived in B", "domain-b")
	s.ArchiveNode(nA.ID, "")
	s.ArchiveNode(nB.ID, "")

	archived, err := s.ListArchived("domain-a")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != nA.ID {
		t.Errorf("domain filter: expected [%s], got %+v", nA.ID, archived)
	}
}

func TestListArchived_Empty(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "live node", "proj")

	archived, err := s.ListArchived("")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archived) != 0 {
		t.Errorf("expected 0 archived nodes, got %d", len(archived))
	}
}

func TestListArchived_NoDomainReturnsAll(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "A", "domain-a")
	nB := mustAddNode(t, s, "B", "domain-b")
	s.ArchiveNode(nA.ID, "")
	s.ArchiveNode(nB.ID, "")

	archived, err := s.ListArchived("")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archived) != 2 {
		t.Errorf("expected 2 archived nodes across domains, got %d", len(archived))
	}
}

func TestListArchived_LiveNodesNotIncluded(t *testing.T) {
	s := newStore(t)
	live := mustAddNode(t, s, "live", "proj")
	archived := mustAddNode(t, s, "archived", "proj")
	s.ArchiveNode(archived.ID, "reason")

	listed, err := s.ListArchived("")
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	for _, n := range listed {
		if n.ID == live.ID {
			t.Error("live node should not appear in ListArchived")
		}
	}
}

// ── Alias resolution ──────────────────────────────────────────────────────────

func TestAddAlias_AffectsSearch(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Engine fact", "deep-engine")
	s.AddAlias("engine", "deep-engine")

	res, err := s.SearchNodes("Engine fact", "engine", 10)
	if err != nil {
		t.Fatalf("SearchNodes via alias: %v", err)
	}
	found := false
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("alias should resolve to canonical domain in search")
	}
}

func TestResolveAlias_UnknownReturnsInput(t *testing.T) {
	s := newStore(t)
	canonical := s.ResolveAlias("unknown-alias")
	if canonical != "unknown-alias" {
		t.Errorf("unknown alias should return itself, got %q", canonical)
	}
}

// ── AddEdge ───────────────────────────────────────────────────────────────────

func TestAddEdge_NonExistentNode_Errors(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "real node", "proj")

	_, err := s.AddEdge("ghost", n.ID, "connects_to", "")
	if err == nil {
		t.Error("AddEdge with non-existent from_node should error")
	}

	_, err = s.AddEdge(n.ID, "ghost", "connects_to", "")
	if err == nil {
		t.Error("AddEdge with non-existent to_node should error")
	}
}

func TestAddEdge_AppearsInGetNode(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "node-a", "proj")
	b := mustAddNode(t, s, "node-b", "proj")
	e, err := s.AddEdge(a.ID, b.ID, "led_to", "a led to b")
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	nwe, _ := s.GetNode(a.ID)
	found := false
	for _, edge := range nwe.Edges {
		if edge.ID == e.ID {
			found = true
		}
	}
	if !found {
		t.Error("edge not found in GetNode result")
	}
}

// ── UpdateNode ────────────────────────────────────────────────────────────────

func ptrStr(s string) *string { return &s }

func TestUpdateNode_UpdatesDescription(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "update target", "proj")

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("new description"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.Description != "new description" {
		t.Errorf("description: got %q, want %q", updated.Description, "new description")
	}
	// Label should be unchanged
	if updated.Label != n.Label {
		t.Errorf("label changed unexpectedly: %q", updated.Label)
	}
}

func TestUpdateNode_UpdatesLabel(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "old label", "proj")

	updated, err := s.UpdateNode(n.ID, ptrStr("new label"), nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.Label != "new label" {
		t.Errorf("label: got %q, want %q", updated.Label, "new label")
	}
}

func TestUpdateNode_UpdatesTags(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "tagged node", "proj")

	updated, err := s.UpdateNode(n.ID, nil, nil, nil, ptrStr("kotlin gradle testing"))
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.Tags != "kotlin gradle testing" {
		t.Errorf("tags: got %q, want %q", updated.Tags, "kotlin gradle testing")
	}
}

func TestUpdateNode_OnlyUpdatesProvidedFields(t *testing.T) {
	s := newStore(t)
	n, _ := s.AddNode("stable label", "original desc", "original why", "proj", nil, "original tags", false)

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("new desc only"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.Description != "new desc only" {
		t.Errorf("description: got %q", updated.Description)
	}
	if updated.Label != "stable label" {
		t.Errorf("label changed: %q", updated.Label)
	}
	if updated.WhyMatters != "original why" {
		t.Errorf("why_matters changed: %q", updated.WhyMatters)
	}
	if updated.Tags != "original tags" {
		t.Errorf("tags changed: %q", updated.Tags)
	}
}

func TestUpdateNode_BumpsUpdatedAt(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "timestamp test", "proj")
	before := n.UpdatedAt

	// Sleep briefly to ensure time advances.
	time.Sleep(2 * time.Millisecond)

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("changed"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if !updated.UpdatedAt.After(before) {
		t.Errorf("updated_at not bumped: before=%v after=%v", before, updated.UpdatedAt)
	}
}

func TestUpdateNode_NotFoundReturnsError(t *testing.T) {
	s := newStore(t)

	_, err := s.UpdateNode("nonexistent-id-xxxx", ptrStr("x"), nil, nil, nil)
	if err == nil {
		t.Error("expected error for missing node, got nil")
	}
}

func TestUpdateNode_ArchivedNodeReturnsError(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "soon archived", "proj")
	s.ArchiveNode(n.ID, "test")

	_, err := s.UpdateNode(n.ID, ptrStr("new label"), nil, nil, nil)
	if err == nil {
		t.Error("expected error updating archived node, got nil")
	}
}

// ── Tags search ───────────────────────────────────────────────────────────────

func TestAddNode_WithTags_SearchableByTag(t *testing.T) {
	s := newStore(t)
	// The label won't match; a tag synonym will.
	n, err := s.AddNode("Parameterised test approval files need withNameSuffix", "some description", "why", "proj", nil, "testing approval parameterised withNamesuffix", false)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	res, err := s.SearchNodes("testing approval parameterised", "proj", 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	found := false
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("node not found via tag search")
	}
}

func TestUpdateNode_WritesAuditLog(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	n := mustAddNode(t, s, "audit target", "proj")

	_, err = s.UpdateNode(n.ID, nil, ptrStr("new description"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	// Open second connection to inspect audit_log.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open rawDB: %v", err)
	}
	defer rawDB.Close()

	var action, reason string
	err = rawDB.QueryRow(
		`SELECT action, reason FROM audit_log WHERE node_id = ?`, n.ID,
	).Scan(&action, &reason)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if action != "update" {
		t.Errorf("action: got %q, want %q", action, "update")
	}
	if reason == "" {
		t.Error("reason should be non-empty")
	}
	if !strings.Contains(reason, "description") {
		t.Errorf("reason should mention 'description'; got %q", reason)
	}
}

// ── SearchNodes multi-word fallback ───────────────────────────────────────────

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
		false,
	)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	res, err := s.SearchNodes("testing approval parameterised", "proj", 10)
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

	res, err := s.SearchNodes("ULA", "proj", 10)
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
		false,
	)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// No domain filter — should still hit fallback path.
	res, err := s.SearchNodes("testing approval parameterised", "", 10)
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

// ── Transient field ───────────────────────────────────────────────────────────

func TestAddNode_Transient_Persists(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("sprint ticket XYZ", "d", "w", "proj", nil, "", true)
	if err != nil {
		t.Fatalf("AddNode transient: %v", err)
	}
	if !n.Transient {
		t.Error("Transient should be true on returned node")
	}

	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if !got.Node.Transient {
		t.Error("Transient should be true when fetched via GetNode")
	}
}

func TestAddNode_Transient_DefaultsFalse(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("regular node", "d", "w", "proj", nil, "", false)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if n.Transient {
		t.Error("Transient should default to false")
	}
}

func TestFindDrift_TransientOlderThan7Days_IsDriftCandidate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	n, err := s.AddNode("sprint ticket stale", "d", "w", "transient-drift", nil, "", true)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Backdate created_at to 8 days ago via raw SQL.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	stale := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, stale, n.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	candidates, err := s.FindDrift("transient-drift", 10)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			found = true
			if !strings.Contains(c.Reason, "transient") {
				t.Errorf("reason should mention 'transient'; got %q", c.Reason)
			}
		}
	}
	if !found {
		t.Errorf("stale transient node (%s) should appear in drift candidates", n.ID)
	}
}

func TestFindDrift_TransientNewerThan7Days_NotDriftCandidate(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("recent sprint ticket", "d", "w", "transient-new", nil, "", true)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	candidates, err := s.FindDrift("transient-new", 10)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			t.Errorf("recent transient node should NOT appear in drift; got reason: %q", c.Reason)
		}
	}
}

// ── SuggestEdges ──────────────────────────────────────────────────────────────

func TestSuggestEdges_ReturnsOverlappingTagsNode(t *testing.T) {
	s := newStore(t)
	nA, _ := s.AddNode("sprint ticket alpha", "d", "w", "proj", nil, "kotlin testing", false)
	nB, _ := s.AddNode("sprint ticket beta", "d", "w", "proj", nil, "kotlin approval", false)
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
	n, _ := s.AddNode("self test node kotlin", "d", "w", "proj", nil, "kotlin", false)
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
	nA, _ := s.AddNode("kotlin build system", "d", "w", "domain-a", nil, "kotlin gradle", false)
	s.AddNode("kotlin build tool", "d", "w", "domain-b", nil, "kotlin gradle", false) // different domain
	nC, _ := s.AddNode("kotlin runner", "d", "w", "domain-a", nil, "kotlin testing", false)

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

func TestAddNode_Tags_RoundTrip(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("my node", "desc", "why", "proj", nil, "alpha beta gamma", false)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.Tags != "alpha beta gamma" {
		t.Errorf("tags round-trip: got %q", got.Node.Tags)
	}
}
