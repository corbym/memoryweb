package db_test

import "testing"

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
