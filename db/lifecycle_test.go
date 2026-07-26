package db_test

import (
	"testing"

	"github.com/corbym/memoryweb/db"
)

func TestLifecycleStates_IssueWithResolvedEdge(t *testing.T) {
	s := newStore(t)
	issue := mustAddNodeWithKind(t, s, "open bug", "lc-domain", "issue")
	other := mustAddNode(t, s, "fix shipped", "lc-domain")
	if _, err := s.AddEdge(issue.ID, other.ID, "resolved", "fixed"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	states, err := s.LifecycleStates([]string{issue.ID, other.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[issue.ID] != db.LifecycleResolved {
		t.Errorf("issue with outbound resolved: got %q, want resolved", states[issue.ID])
	}
	if states[other.ID] != "" {
		t.Errorf("uninvolved partner should have no lifecycle state; got %q", states[other.ID])
	}
}

func TestLifecycleStates_UnresolvedContradicts(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "claim A", "lc-domain")
	b := mustAddNode(t, s, "claim B", "lc-domain")
	if _, err := s.AddEdge(a.ID, b.ID, "contradicts", "conflict"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	states, err := s.LifecycleStates([]string{a.ID, b.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[a.ID] != db.LifecycleContested {
		t.Errorf("node A: got %q, want contested", states[a.ID])
	}
	if states[b.ID] != db.LifecycleContested {
		t.Errorf("node B: got %q, want contested", states[b.ID])
	}
}

func TestLifecycleStates_ResolvedContradictsPair(t *testing.T) {
	s := newStore(t)
	a := mustAddNode(t, s, "old policy", "lc-domain")
	b := mustAddNode(t, s, "new policy", "lc-domain")
	if _, err := s.AddEdge(a.ID, b.ID, "contradicts", "conflict"); err != nil {
		t.Fatalf("AddEdge contradicts: %v", err)
	}
	if _, err := s.AddEdge(b.ID, a.ID, "supersedes", "new wins"); err != nil {
		t.Fatalf("AddEdge supersedes: %v", err)
	}

	states, err := s.LifecycleStates([]string{a.ID, b.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[a.ID] == db.LifecycleContested {
		t.Errorf("resolved pair node A should not be contested")
	}
	if states[b.ID] == db.LifecycleContested {
		t.Errorf("resolved pair node B should not be contested")
	}
}

func TestLifecycleStates_SupersededInbound(t *testing.T) {
	s := newStore(t)
	old := mustAddNode(t, s, "old approach", "lc-domain")
	newer := mustAddNode(t, s, "new approach", "lc-domain")
	if _, err := s.AddEdge(newer.ID, old.ID, "supersedes", "replaced"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	states, err := s.LifecycleStates([]string{old.ID, newer.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[old.ID] != db.LifecycleSuperseded {
		t.Errorf("superseded node: got %q, want superseded", states[old.ID])
	}
}

func TestLifecycleStates_OrdinaryDecision(t *testing.T) {
	s := newStore(t)
	n := mustAddNodeWithKind(t, s, "normal decision", "lc-domain", "decision")

	states, err := s.LifecycleStates([]string{n.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[n.ID] != "" {
		t.Errorf("ordinary decision: got %q, want empty", states[n.ID])
	}
}

func TestLifecycleStates_LegacyResolvedLabel(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("RESOLVED: old workaround", "desc", "why", "lc-domain", nil, "", "issue")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	states, err := s.LifecycleStates([]string{n.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[n.ID] != db.LifecycleResolved {
		t.Errorf("legacy RESOLVED label prefix: got %q, want resolved", states[n.ID])
	}
}

func TestLifecycleStates_ContestedPartnerArchived(t *testing.T) {
	s := newStore(t)
	live := mustAddNode(t, s, "live claim", "lc-domain")
	archived := mustAddNode(t, s, "archived claim", "lc-domain")
	if _, err := s.AddEdge(live.ID, archived.ID, "contradicts", "conflict"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := s.ArchiveNode(archived.ID, "stale counterpart"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	states, err := s.LifecycleStates([]string{live.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[live.ID] != "" {
		t.Errorf("live node with archived contradict partner: got %q, want empty", states[live.ID])
	}
}

func TestLifecycleStates_SupersededSupersederArchived(t *testing.T) {
	s := newStore(t)
	old := mustAddNode(t, s, "old approach", "lc-domain")
	newer := mustAddNode(t, s, "new approach", "lc-domain")
	if _, err := s.AddEdge(newer.ID, old.ID, "supersedes", "replaced"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := s.ArchiveNode(newer.ID, "superseder archived"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	states, err := s.LifecycleStates([]string{old.ID})
	if err != nil {
		t.Fatalf("LifecycleStates: %v", err)
	}
	if states[old.ID] != "" {
		t.Errorf("live node with archived superseder: got %q, want empty", states[old.ID])
	}
}
