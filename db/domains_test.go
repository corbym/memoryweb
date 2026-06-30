package db_test

import (
	"strings"
	"testing"
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
