package db_test

import (
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
)

func TestAddAlias_AffectsSearch(t *testing.T) {
	s := newStore(t)
	n := mustAddNode(t, s, "Engine fact", "deep-engine")
	s.AddAlias("engine", "deep-engine")

	res, err := s.SearchNodes("Engine fact", "engine", 10, "", nil)
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

func TestAddNode_ResolvesAliasOnWrite(t *testing.T) {
	s := newStore(t)
	s.AddAlias("engine", "deep-engine")

	n, err := s.AddNode("Engine write path", "desc", "why", "engine", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode via alias: %v", err)
	}
	if n.Domain != "deep-engine" {
		t.Errorf("AddNode stored domain %q, want canonical %q", n.Domain, "deep-engine")
	}
	count, err := s.CountNodes("deep-engine")
	if err != nil {
		t.Fatalf("CountNodes: %v", err)
	}
	if count != 1 {
		t.Errorf("canonical domain count: got %d, want 1", count)
	}
	got, err := s.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Node.Domain != "deep-engine" {
		t.Errorf("stored row domain %q, want %q", got.Node.Domain, "deep-engine")
	}
}

func TestAddNodesBatch_ResolvesAliasOnWrite(t *testing.T) {
	s := newStore(t)
	s.AddAlias("engine", "deep-engine")

	nodes, err := s.AddNodesBatch([]db.NodeInput{{
		Label:  "Batch via alias",
		Domain: "engine",
	}})
	if err != nil {
		t.Fatalf("AddNodesBatch via alias: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Domain != "deep-engine" {
		t.Errorf("batch stored domain %q, want %q", nodes[0].Domain, "deep-engine")
	}
}

func TestUpdateNode_ResolvesAliasOnWrite(t *testing.T) {
	s := newStore(t)
	s.AddAlias("target", "canonical-target")
	n := mustAddNode(t, s, "Move me", "source-domain")

	reason := "test move"
	target := "target"
	updated, err := s.UpdateNode(n.ID, nil, nil, nil, nil, nil, nil, &target, &reason)
	if err != nil {
		t.Fatalf("UpdateNode via alias: %v", err)
	}
	if updated.Domain != "canonical-target" {
		t.Errorf("UpdateNode stored domain %q, want %q", updated.Domain, "canonical-target")
	}
}

func TestUpdateNode_AliasMatchingCurrentDomain_IsNoOp(t *testing.T) {
	s := newStore(t)
	s.AddAlias("engine", "deep-engine")
	n := mustAddNode(t, s, "Already canonical", "deep-engine")

	aliasDomain := "engine"
	updated, err := s.UpdateNode(n.ID, nil, nil, nil, nil, nil, nil, &aliasDomain, nil)
	if err != nil {
		t.Fatalf("UpdateNode with alias matching current domain: %v", err)
	}
	if updated.Domain != "deep-engine" {
		t.Errorf("domain: got %q, want %q", updated.Domain, "deep-engine")
	}
}

func TestUpdateNodesBatch_ResolvesAliasOnWrite(t *testing.T) {
	s := newStore(t)
	s.AddAlias("target", "canonical-target")
	n := mustAddNode(t, s, "Batch move", "source-domain")

	reason := "batch move"
	target := "target"
	nodes, err := s.UpdateNodesBatch([]db.NodeUpdateInput{{
		ID:     n.ID,
		Domain: &target,
		Reason: &reason,
	}})
	if err != nil {
		t.Fatalf("UpdateNodesBatch via alias: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Domain != "canonical-target" {
		t.Errorf("batch stored domain %q, want %q", nodes[0].Domain, "canonical-target")
	}
}

func TestUpdateNodesBatch_AliasMatchingCurrentDomain_IsNoOp(t *testing.T) {
	s := newStore(t)
	s.AddAlias("engine", "deep-engine")
	n := mustAddNode(t, s, "Batch canonical", "deep-engine")

	aliasDomain := "engine"
	nodes, err := s.UpdateNodesBatch([]db.NodeUpdateInput{{
		ID:     n.ID,
		Domain: &aliasDomain,
	}})
	if err != nil {
		t.Fatalf("UpdateNodesBatch with alias matching current domain: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Domain != "deep-engine" {
		t.Errorf("domain: got %q, want %q", nodes[0].Domain, "deep-engine")
	}
}

func TestAddAlias_RejectsWhenLiveRowsExistUnderAliasName(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Existing row", "engine")

	err := s.AddAlias("engine", "deep-engine")
	if err == nil {
		t.Fatal("expected error when alias name already has live nodes")
	}
	if !strings.Contains(err.Error(), "live node") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveAlias_UnknownReturnsInput(t *testing.T) {
	s := newStore(t)
	canonical := s.ResolveAlias("unknown-alias")
	if canonical != "unknown-alias" {
		t.Errorf("unknown alias should return itself, got %q", canonical)
	}
}

func TestRenameDomain_MovesNodesAndCreatesAlias(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "old-domain")
	mustAddNode(t, s, "Beta", "old-domain")

	result, err := s.RenameDomain("old-domain", "new-domain")
	if err != nil {
		t.Fatalf("RenameDomain: %v", err)
	}
	if result.NodesRenamed != 2 {
		t.Errorf("NodesRenamed: got %d, want 2", result.NodesRenamed)
	}

	// Nodes should now be in new-domain.
	domains, _ := s.ListDomains()
	found := false
	for _, d := range domains {
		if d == "new-domain" {
			found = true
		}
		if d == "old-domain" {
			t.Error("old-domain still present in ListDomains")
		}
	}
	if !found {
		t.Error("new-domain not present in ListDomains")
	}

	// Alias old → new should resolve.
	if canonical := s.ResolveAlias("old-domain"); canonical != "new-domain" {
		t.Errorf("ResolveAlias: got %q, want %q", canonical, "new-domain")
	}
}

func TestRenameDomain_OldDomainNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.RenameDomain("nonexistent", "anything")
	if err == nil {
		t.Fatal("expected error for nonexistent source domain")
	}
	if !strings.Contains(err.Error(), "no live nodes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRenameDomain_NewDomainAlreadyExists(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "domain-a")
	mustAddNode(t, s, "Beta", "domain-b")

	_, err := s.RenameDomain("domain-a", "domain-b")
	if err == nil {
		t.Fatal("expected error when target domain already has nodes")
	}
	if !strings.Contains(err.Error(), "merge_domains") {
		t.Errorf("error should mention merge_domains: %v", err)
	}
}

func TestMergeDomains_MovesNodesAndCreatesAlias(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "source")
	mustAddNode(t, s, "Beta", "source")
	mustAddNode(t, s, "Gamma", "target")

	result, err := s.MergeDomains("source", "target", false)
	if err != nil {
		t.Fatalf("MergeDomains: %v", err)
	}
	if result.NodesMoved != 2 {
		t.Errorf("NodesMoved: got %d, want 2", result.NodesMoved)
	}

	// source should no longer appear as a domain.
	domains, _ := s.ListDomains()
	for _, d := range domains {
		if d == "source" {
			t.Error("source domain still present after merge")
		}
	}

	// Alias source → target should resolve.
	if canonical := s.ResolveAlias("source"); canonical != "target" {
		t.Errorf("ResolveAlias: got %q, want %q", canonical, "target")
	}
}

func TestMergeDomains_DryRun_NoChanges(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "source")
	mustAddNode(t, s, "Gamma", "target")

	result, err := s.MergeDomains("source", "target", true)
	if err != nil {
		t.Fatalf("MergeDomains dry-run: %v", err)
	}
	if result.NodesMoved != 1 {
		t.Errorf("NodesMoved: got %d, want 1", result.NodesMoved)
	}

	// No changes should have been made.
	domains, _ := s.ListDomains()
	found := map[string]bool{}
	for _, d := range domains {
		found[d] = true
	}
	if !found["source"] {
		t.Error("source domain disappeared during dry-run")
	}
	if s.ResolveAlias("source") != "source" {
		t.Error("alias created during dry-run")
	}
}

func TestMergeDomains_LabelCollisionsDetected(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Shared Label", "source")
	mustAddNode(t, s, "shared label", "target") // same after LOWER()

	result, err := s.MergeDomains("source", "target", true)
	if err != nil {
		t.Fatalf("MergeDomains: %v", err)
	}
	if len(result.LabelCollisions) == 0 {
		t.Error("expected label collision to be detected")
	}
}

func TestMergeDomains_SourceNoNodes_Error(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Gamma", "target")

	_, err := s.MergeDomains("nonexistent", "target", false)
	if err == nil {
		t.Fatal("expected error for nonexistent source domain")
	}
	if !strings.Contains(err.Error(), "no live nodes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMergeDomains_TargetNoNodes_Error(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "Alpha", "source")

	_, err := s.MergeDomains("source", "nonexistent", false)
	if err == nil {
		t.Fatal("expected error for nonexistent target domain")
	}
	if !strings.Contains(err.Error(), "rename_domain") {
		t.Errorf("error should mention rename_domain: %v", err)
	}
}
