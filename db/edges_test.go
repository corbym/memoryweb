package db_test

import (
	"strings"
	"testing"
)

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

// TestAddEdge_MissingToNode_NoCrossDomainMessage asserts that when toID is
// missing, the error does not blame cross-domain limitations.
func TestAddEdge_MissingToNode_NoCrossDomainMessage(t *testing.T) {
	s := newStore(t)
	from := mustAddNode(t, s, "source", "src")

	_, err := s.AddEdge(from.ID, "totally-missing-id", "connects_to", "")
	if err == nil {
		t.Fatal("AddEdge with missing toID should return an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "cross-domain") {
		t.Errorf("error must not mention cross-domain for a missing ID; got: %s", msg)
	}
	if strings.Contains(msg, "not yet supported") {
		t.Errorf("error must not say 'not yet supported'; got: %s", msg)
	}
}

// TestAddEdge_ArchivedToNode_ReturnsRestoreHint asserts that when toID refers
// to an archived node, the error mentions "restore".
func TestAddEdge_ArchivedToNode_ReturnsRestoreHint(t *testing.T) {
	s := newStore(t)
	from := mustAddNode(t, s, "live source", "proj")
	to := mustAddNode(t, s, "archived target", "proj")

	if err := s.ArchiveNode(to.ID, "test"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	_, err := s.AddEdge(from.ID, to.ID, "connects_to", "")
	if err == nil {
		t.Fatal("AddEdge to archived node should return an error")
	}
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("error should mention 'restore'; got: %s", err.Error())
	}
}

// TestAddEdge_CrossDomain_ValidIDs_Succeeds is the regression test: cross-
// domain connect must succeed when both nodes are live.
func TestAddEdge_CrossDomain_ValidIDs_Succeeds(t *testing.T) {
	s := newStore(t)
	from := mustAddNode(t, s, "domain-a node", "domain-a")
	to := mustAddNode(t, s, "domain-b node", "domain-b")

	edge, err := s.AddEdge(from.ID, to.ID, "depends_on", "a depends on b")
	if err != nil {
		t.Fatalf("AddEdge cross-domain should succeed; got: %v", err)
	}
	if edge.ID == "" {
		t.Error("expected a non-empty edge ID")
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
