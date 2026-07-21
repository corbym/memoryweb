package db_test

import (
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
)

func TestAssessTrustForNodeIDs_LowTrustUnsupported(t *testing.T) {
	s := newStore(t)
	assumption := mustAddNodeKind(t, s, "unverified premise", "proj", "assumption")
	target := mustAddNodeKind(t, s, "shaky decision", "proj", "decision")
	if _, err := s.AddEdge(assumption.ID, target.ID, "depends_on", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	assessments, err := s.AssessTrustForNodeIDs([]string{target.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	a, ok := assessments[target.ID]
	if !ok {
		t.Fatal("expected assessment for target")
	}
	if !a.IsLowTrust {
		t.Errorf("expected low trust for assumption-backed decision, basis=%q", a.TrustBasis)
	}
	if !strings.Contains(a.TrustBasis, "self:decision") {
		t.Errorf("trust_basis should include self kind; got %q", a.TrustBasis)
	}
}

func TestAssessTrustForNodeIDs_HighTrustFindingBacked(t *testing.T) {
	s := newStore(t)
	finding := mustAddNodeKind(t, s, "verified fact", "proj", "finding")
	target := mustAddNodeKind(t, s, "solid decision", "proj", "decision")
	if _, err := s.AddEdge(finding.ID, target.ID, "depends_on", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	assessments, err := s.AssessTrustForNodeIDs([]string{target.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	a := assessments[target.ID]
	if a.IsLowTrust {
		t.Errorf("finding-backed decision should not be low trust; basis=%q", a.TrustBasis)
	}
}

func TestAssessTrustForNodeIDs_NetContested(t *testing.T) {
	s := newStore(t)
	finding := mustAddNodeKind(t, s, "counter evidence", "proj", "finding")
	target := mustAddNodeKind(t, s, "contested decision", "proj", "decision")
	if _, err := s.AddEdge(finding.ID, target.ID, "contradicts", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	assessments, err := s.AssessTrustForNodeIDs([]string{target.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	if !assessments[target.ID].IsLowTrust {
		t.Error("net-contested node should be low trust")
	}
}

func TestAssessTrustForNodeIDs_SingleAssumptionInboundIsLowTrust(t *testing.T) {
	s := newStore(t)
	support := mustAddNodeKind(t, s, "support", "proj", "assumption")
	assumption := mustAddNodeKind(t, s, "premise", "proj", "assumption")
	if _, err := s.AddEdge(support.ID, assumption.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	assessments, err := s.AssessTrustForNodeIDs([]string{assumption.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	if !assessments[assumption.ID].IsLowTrust {
		t.Errorf("single assumption inbound should still be low trust; basis=%q", assessments[assumption.ID].TrustBasis)
	}
}

func TestAssessTrustForNodeIDs_ExcludeDependentNode(t *testing.T) {
	s := newStore(t)
	support := mustAddNodeKind(t, s, "support", "proj", "assumption")
	assumption := mustAddNodeKind(t, s, "premise", "proj", "assumption")
	decision := mustAddNodeKind(t, s, "dependent", "proj", "decision")
	if _, err := s.AddEdge(support.ID, assumption.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge support: %v", err)
	}
	if _, err := s.AddEdge(decision.ID, assumption.ID, "depends_on", ""); err != nil {
		t.Fatalf("AddEdge depends_on: %v", err)
	}

	withDecision, err := s.AssessTrustForNodeIDs([]string{assumption.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	if withDecision[assumption.ID].IsLowTrust {
		t.Fatal("depends_on from decision should provide high-tier support when not excluded")
	}

	excluded, err := s.AssessTrustForNodeIDs([]string{assumption.ID}, 90, decision.ID)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs exclude: %v", err)
	}
	if !excluded[assumption.ID].IsLowTrust {
		t.Errorf("excluding dependent should restore low-trust; basis=%q", excluded[assumption.ID].TrustBasis)
	}
}

func TestAssessTrustForNodeIDs_NoInboundNotLowTrust(t *testing.T) {
	s := newStore(t)
	target := mustAddNodeKind(t, s, "isolated decision", "proj", "decision")

	assessments, err := s.AssessTrustForNodeIDs([]string{target.ID}, 90)
	if err != nil {
		t.Fatalf("AssessTrustForNodeIDs: %v", err)
	}
	if assessments[target.ID].IsLowTrust {
		t.Error("node with no inbound edges should not be flagged low-trust")
	}
}

func mustAddNodeKind(t *testing.T, s *db.Store, label, domain, kind string) db.Node {
	t.Helper()
	n, err := s.AddNode(label, "", "why", domain, nil, "", kind)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	return *n
}
