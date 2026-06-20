package db_test

import (
	"path/filepath"
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
	n, err := s.AddNode(label, "desc", "why", domain, nil, "", "")
	if err != nil {
		t.Fatalf("AddNode(%q): %v", label, err)
	}
	return n
}

func mustAddNodeWithTags(t *testing.T, s *db.Store, label, domain, tags string) *db.Node {
	t.Helper()
	n, err := s.AddNode(label, "desc", "why", domain, nil, tags, "")
	if err != nil {
		t.Fatalf("AddNode(%q): %v", label, err)
	}
	return n
}

func nodeIDs(nodes []db.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func ptr(t time.Time) *time.Time { return &t }

func ptrStr(s string) *string { return &s }
