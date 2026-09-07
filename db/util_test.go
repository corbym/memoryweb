package db

import (
	"path/filepath"
	"testing"
)

// ── nodeMatchesTags ───────────────────────────────────────────────────────────

func TestNodeMatchesTags_ExactMatch(t *testing.T) {
	if !nodeMatchesTags("foo bar", []string{"foo"}) {
		t.Error("expected match for exact tag 'foo'")
	}
}

func TestNodeMatchesTags_CaseInsensitive(t *testing.T) {
	if !nodeMatchesTags("SCH foo", []string{"sch"}) {
		t.Error("expected case-insensitive match: stored 'SCH', queried 'sch'")
	}
	if !nodeMatchesTags("sch foo", []string{"SCH"}) {
		t.Error("expected case-insensitive match: stored 'sch', queried 'SCH'")
	}
}

func TestNodeMatchesTags_NoPartialMatch(t *testing.T) {
	if nodeMatchesTags("architecture foo", []string{"arch"}) {
		t.Error("expected no match: 'arch' must not partially match 'architecture'")
	}
}

func TestNodeMatchesTags_EmptyInputs(t *testing.T) {
	if nodeMatchesTags("", []string{"foo"}) {
		t.Error("expected no match for empty tag string")
	}
	if nodeMatchesTags("foo", nil) {
		t.Error("expected no match for nil want tags")
	}
}

// ── tagFilter via store (SQL path) ───────────────────────────────────────────

func TestTagFilter_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	// Add a node tagged with uppercase "SCH".
	n, err := s.AddNode("SCH decision", "desc", "why", "test-domain", nil, "SCH", "")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	// Make it disconnected so FindDisconnected returns it.
	nodes, err := s.FindDisconnected("test-domain", []string{"sch"}, nil, 10)
	if err != nil {
		t.Fatalf("FindDisconnected: %v", err)
	}
	found := false
	for _, node := range nodes {
		if node.ID == n.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FindDisconnected with tags=['sch'] to match node tagged 'SCH'; got %d results", len(nodes))
	}
}

func TestTagFilter_CaseInsensitiveExcludesOtherTags(t *testing.T) {
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	_, err = s.AddNode("unrelated node", "desc", "why", "tf-domain", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode unrelated: %v", err)
	}
	nodes, err := s.FindDisconnected("tf-domain", []string{"sch"}, nil, 10)
	if err != nil {
		t.Fatalf("FindDisconnected: %v", err)
	}
	if len(nodes) > 0 {
		t.Errorf("expected no results for tag 'sch' when no node has that tag; got %d", len(nodes))
	}
}
