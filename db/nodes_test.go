package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

func TestAddNode_IDContainsSlug(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("RST Boot Crash", "desc", "why", "deep-game", nil, "", "")
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
	n, err := s.AddNode("dated node", "d", "w", "proj", &ts, "", "")
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

func TestArchiveNode_SetsArchivedAt(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "to be archived", "proj")

	if err := s.ArchiveNode(n.ID, "outdated"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	// ListArchived should now include this node
	archived, err := s.ListArchived("", nil)
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

func TestRestoreNode_ReappearsInSearch(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "restore me", "proj")
	s.ArchiveNode(n.ID, "reason")

	// hidden
	res, _ := s.SearchNodes("restore me", "proj", 10, "")
	for _, node := range res.Nodes {
		if node.ID == n.ID {
			t.Fatal("should be hidden before restore")
		}
	}

	if err := s.RestoreNode(n.ID); err != nil {
		t.Fatalf("RestoreNode: %v", err)
	}

	// visible again
	res, _ = s.SearchNodes("restore me", "proj", 10, "")
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

	archived, err := s.ListArchived("", nil)
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

func TestListArchived_DomainFilter(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "archived in A", "domain-a")
	nB := mustAddNode(t, s, "archived in B", "domain-b")
	s.ArchiveNode(nA.ID, "")
	s.ArchiveNode(nB.ID, "")

	archived, err := s.ListArchived("domain-a", nil)
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

	archived, err := s.ListArchived("", nil)
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

	archived, err := s.ListArchived("", nil)
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

	listed, err := s.ListArchived("", nil)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	for _, n := range listed {
		if n.ID == live.ID {
			t.Error("live node should not appear in ListArchived")
		}
	}
}

func TestUpdateNode_UpdatesDescription(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "update target", "proj")

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("new description"), nil, nil, nil, nil)
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

	updated, err := s.UpdateNode(n.ID, ptrStr("new label"), nil, nil, nil, nil, nil)
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

	updated, err := s.UpdateNode(n.ID, nil, nil, nil, ptrStr("kotlin gradle testing"), nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.Tags != "kotlin gradle testing" {
		t.Errorf("tags: got %q, want %q", updated.Tags, "kotlin gradle testing")
	}
}

func TestUpdateNode_OnlyUpdatesProvidedFields(t *testing.T) {
	s := newStore(t)
	n, _ := s.AddNode("stable label", "original desc", "original why", "proj", nil, "original tags", "")

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("new desc only"), nil, nil, nil, nil)
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

	updated, err := s.UpdateNode(n.ID, nil, ptrStr("changed"), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if !updated.UpdatedAt.After(before) {
		t.Errorf("updated_at not bumped: before=%v after=%v", before, updated.UpdatedAt)
	}
}

func TestUpdateNode_NotFoundReturnsError(t *testing.T) {
	s := newStore(t)

	_, err := s.UpdateNode("nonexistent-id-xxxx", ptrStr("x"), nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error for missing node, got nil")
	}
}

func TestUpdateNode_ArchivedNodeReturnsError(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "soon archived", "proj")
	s.ArchiveNode(n.ID, "test")

	_, err := s.UpdateNode(n.ID, ptrStr("new label"), nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error updating archived node, got nil")
	}
}

func TestAddNode_WithTags_SearchableByTag(t *testing.T) {
	s := newStore(t)
	// The label won't match; a tag synonym will.
	n, err := s.AddNode("Parameterised test approval files need withNameSuffix", "some description", "why", "proj", nil, "testing approval parameterised withNamesuffix", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	res, err := s.SearchNodes("testing approval parameterised", "proj", 10, "")
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

	_, err = s.UpdateNode(n.ID, nil, ptrStr("new description"), nil, nil, nil, nil)
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

func TestAddNode_NodeKindTransient_Persists(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("sprint ticket XYZ", "d", "w", "proj", nil, "", "transient")
	if err != nil {
		t.Fatalf("AddNode transient: %v", err)
	}
	if n.NodeKind != "transient" {
		t.Errorf("NodeKind should be 'transient', got %q", n.NodeKind)
	}

	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.NodeKind != "transient" {
		t.Errorf("NodeKind should be 'transient' when fetched via GetNode, got %q", got.Node.NodeKind)
	}
}

func TestAddNode_NodeKindDefaultsToDecision_Legacy(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("regular node", "d", "w", "proj", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if n.NodeKind != "decision" {
		t.Errorf("NodeKind should default to 'decision', got %q", n.NodeKind)
	}
}

func TestAddNode_Tags_RoundTrip(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("my node", "desc", "why", "proj", nil, "alpha beta gamma", "")
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

func TestCountArchived_Empty(t *testing.T) {
	s := newStore(t)
	count, err := s.CountArchived("no-such-domain")
	if err != nil {
		t.Fatalf("CountArchived: %v", err)
	}
	if count != 0 {
		t.Errorf("want 0, got %d", count)
	}
}

func TestCountArchived_AfterArchive(t *testing.T) {
	s := newStore(t)
	nodes := make([]*db.Node, 5)
	for i := 0; i < 5; i++ {
		nodes[i] = mustAddNode(t, s, fmt.Sprintf("Node %d", i), "arch-count")
	}
	if err := s.ArchiveNode(nodes[0].ID, "test"); err != nil {
		t.Fatalf("ArchiveNode 0: %v", err)
	}
	if err := s.ArchiveNode(nodes[1].ID, "test"); err != nil {
		t.Fatalf("ArchiveNode 1: %v", err)
	}

	count, err := s.CountArchived("arch-count")
	if err != nil {
		t.Fatalf("CountArchived: %v", err)
	}
	if count != 2 {
		t.Errorf("want 2, got %d", count)
	}
}

func TestAddNode_NodeKindDefaultsToDecision(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("default type node", "d", "w", "proj", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if n.NodeKind != "decision" {
		t.Errorf("NodeKind: got %q, want %q", n.NodeKind, "decision")
	}
}

func TestAddNode_NodeKindStanding(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("a standing rule", "d", "why", "proj", nil, "", "standing")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if n.NodeKind != "standing" {
		t.Errorf("NodeKind: got %q, want %q", n.NodeKind, "standing")
	}
	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.NodeKind != "standing" {
		t.Errorf("GetNode NodeKind: got %q, want %q", got.Node.NodeKind, "standing")
	}
}

func TestAddNode_NodeKind_AllValues(t *testing.T) {
	s := newStore(t)
	kinds := []string{"transient", "reference", "issue", "decision", "option", "assumption", "finding", "standing", "goal"}
	for _, kind := range kinds {
		n, err := s.AddNode("node of kind "+kind, "d", "w", "proj", nil, "", kind)
		if err != nil {
			t.Fatalf("AddNode(%q): %v", kind, err)
		}
		if n.NodeKind != kind {
			t.Errorf("NodeKind: got %q, want %q", n.NodeKind, kind)
		}
		got, err := s.GetNode(n.ID)
		if err != nil {
			t.Fatalf("GetNode(%q): %v", kind, err)
		}
		if got.Node.NodeKind != kind {
			t.Errorf("GetNode NodeKind: got %q, want %q", got.Node.NodeKind, kind)
		}
	}
}

func TestGetStandingNodes_Empty(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "just a decision", "proj")
	nodes, err := s.GetStandingNodes("proj")
	if err != nil {
		t.Fatalf("GetStandingNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 standing nodes, got %d", len(nodes))
	}
}

func TestGetStandingNodes_OrderedByInboundEdgeCount(t *testing.T) {
	s := newStore(t)
	// 3 standing nodes; add inbound edges to give them 0, 1, 2 inbound counts.
	zero, _ := s.AddNode("standing zero", "d", "w", "proj", nil, "", "standing")
	one, _ := s.AddNode("standing one", "d", "w", "proj", nil, "", "standing")
	two, _ := s.AddNode("standing two", "d", "w", "proj", nil, "", "standing")

	linkerA := mustAddNode(t, s, "linker A", "proj")
	linkerB := mustAddNode(t, s, "linker B", "proj")
	linkerC := mustAddNode(t, s, "linker C", "proj")

	s.AddEdge(linkerA.ID, one.ID, "governed_by", "")
	s.AddEdge(linkerB.ID, two.ID, "governed_by", "")
	s.AddEdge(linkerC.ID, two.ID, "governed_by", "")

	nodes, err := s.GetStandingNodes("proj")
	if err != nil {
		t.Fatalf("GetStandingNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 standing nodes, got %d", len(nodes))
	}
	// Descending order: two (2), one (1), zero (0).
	if nodes[0].ID != two.ID {
		t.Errorf("nodes[0]: want two (%q), got %q", two.ID, nodes[0].ID)
	}
	if nodes[1].ID != one.ID {
		t.Errorf("nodes[1]: want one (%q), got %q", one.ID, nodes[1].ID)
	}
	if nodes[2].ID != zero.ID {
		t.Errorf("nodes[2]: want zero (%q), got %q", zero.ID, nodes[2].ID)
	}
}

func TestUpdateNode_NodeKind(t *testing.T) {
	s := newStore(t)
	n, _ := s.AddNode("will become standing", "d", "w", "proj", nil, "", "decision")

	updated, err := s.UpdateNode(n.ID, nil, nil, nil, nil, nil, ptrStr("standing"))
	if err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	if updated.NodeKind != "standing" {
		t.Errorf("NodeKind: got %q, want %q", updated.NodeKind, "standing")
	}
}

func TestUpdateNode_NodeKind_InvalidValue(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "any node", "proj")

	_, err := s.UpdateNode(n.ID, nil, nil, nil, nil, nil, ptrStr("nonsense"))
	if err == nil {
		t.Error("expected error for invalid node_kind, got nil")
	}
	if !strings.Contains(err.Error(), "transient") || !strings.Contains(err.Error(), "goal") {
		t.Errorf("expected error to list valid kinds, got: %v", err)
	}
}

func TestListArchived_TagsFilter(t *testing.T) {
	s := newStore(t)
	n1 := mustAddNodeWithTags(t, s, "spike idea", "proj", "spike")
	n2 := mustAddNodeWithTags(t, s, "other idea", "proj", "other")
	s.ArchiveNode(n1.ID, "test archive")
	s.ArchiveNode(n2.ID, "test archive")

	nodes, err := s.ListArchived("", []string{"spike"})
	if err != nil {
		t.Fatalf("ListArchived with tags: %v", err)
	}
	ids := nodeIDs(nodes)
	if !contains(ids, n1.ID) {
		t.Error("spike-tagged archived node should be included")
	}
	if contains(ids, n2.ID) {
		t.Error("other-tagged archived node should be excluded")
	}
}
