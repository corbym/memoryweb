package db_test

import (
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
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

func mustAddNodeWithKind(t *testing.T, s *db.Store, label, domain, kind string) *db.Node {
	t.Helper()
	n, err := s.AddNode(label, "desc", "why", domain, nil, "", kind)
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

// insertEmbeddingsDirect writes embeddings directly into node_embeddings using
// raw SQL, bypassing the Ollama HTTP path entirely. This is orders of magnitude
// faster than withFakeEmbeddings+BackfillEmbeddings for large node counts.
// vectors maps node IDs (not label markers) to float32 slices.
// Uses vec.SerializeFloat32 so the encoding stays in sync with BackfillEmbeddings.
func insertEmbeddingsDirect(t *testing.T, s *db.Store, vectors map[string][]float32) {
	t.Helper()
	if !s.VecAvailable() {
		t.Skip("sqlite-vec not available")
	}
	for nodeID, v := range vectors {
		blob, err := vec.SerializeFloat32(v)
		if err != nil {
			t.Fatalf("insertEmbeddingsDirect: serialize %s: %v", nodeID, err)
		}
		if _, err := s.DB().Exec(`DELETE FROM node_embeddings WHERE node_id = ?`, nodeID); err != nil {
			t.Fatalf("insertEmbeddingsDirect: DELETE %s: %v", nodeID, err)
		}
		if _, err := s.DB().Exec(`INSERT INTO node_embeddings(node_id, embedding) VALUES (?, ?)`, nodeID, blob); err != nil {
			t.Fatalf("insertEmbeddingsDirect: INSERT %s: %v", nodeID, err)
		}
	}
}
