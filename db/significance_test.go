package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

func TestGetSignificance_Empty(t *testing.T) {
	s := newStore(t)
	res, err := s.GetSignificance("empty-domain", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	if len(res.Declared) != 0 {
		t.Errorf("Declared: want 0, got %d", len(res.Declared))
	}
	if len(res.Structural) != 0 {
		t.Errorf("Structural: want 0, got %d", len(res.Structural))
	}
	if len(res.Uncurated) != 0 {
		t.Errorf("Uncurated: want 0, got %d", len(res.Uncurated))
	}
	if len(res.PotentiallyStale) != 0 {
		t.Errorf("PotentiallyStale: want 0, got %d", len(res.PotentiallyStale))
	}
}

func TestGetSignificance_Declared(t *testing.T) {
	s := newStore(t)
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	n1, _ := s.AddNode("Early decision", "d", "w", "proj", ptr(early), "", "")
	n2, _ := s.AddNode("Late decision", "d", "w", "proj", ptr(late), "", "")
	mustAddNode(t, s, "Undated node", "proj") // no occurred_at — should not appear in Declared

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	if len(res.Declared) != 2 {
		t.Fatalf("Declared: want 2, got %d", len(res.Declared))
	}
	// Ordered by occurred_at ASC
	if res.Declared[0].ID != n1.ID {
		t.Errorf("Declared[0]: want %q (early), got %q", n1.ID, res.Declared[0].ID)
	}
	if res.Declared[1].ID != n2.ID {
		t.Errorf("Declared[1]: want %q (late), got %q", n2.ID, res.Declared[1].ID)
	}
}

func TestGetSignificance_Structural(t *testing.T) {
	s := newStore(t)
	popular := mustAddNode(t, s, "Popular node", "proj")
	niche := mustAddNode(t, s, "Niche node", "proj")

	// 3 linkers → popular
	for _, label := range []string{"Linker A", "Linker B", "Linker C"} {
		linker := mustAddNode(t, s, label, "proj")
		if _, err := s.AddEdge(linker.ID, popular.ID, "connects_to", ""); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}
	// 1 linker → niche
	nicheLinker := mustAddNode(t, s, "Niche linker", "proj")
	if _, err := s.AddEdge(nicheLinker.ID, niche.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	if len(res.Structural) == 0 {
		t.Fatal("Structural: expected at least one entry")
	}
	if res.Structural[0].ID != popular.ID {
		t.Errorf("Structural[0]: want %q (popular), got %q", popular.ID, res.Structural[0].ID)
	}
	if res.Structural[0].ImportanceScore <= 0 {
		t.Errorf("ImportanceScore should be > 0, got %f", res.Structural[0].ImportanceScore)
	}
}

func TestGetSignificance_RecencyWindow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	target := mustAddNode(t, s, "Target node", "proj")
	linker, _ := s.AddNode("Stale linker", "d", "w", "proj", nil, "", "")
	if _, err := s.AddEdge(linker.ID, target.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Backdate the linker's updated_at to 200 days ago so it falls outside a 90-day window.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(`UPDATE nodes SET updated_at = datetime('now', '-200 days') WHERE id = ?`, linker.ID); err != nil {
		rawDB.Close()
		t.Fatalf("backdate updated_at: %v", err)
	}
	rawDB.Close()

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	for _, sn := range res.Structural {
		if sn.ID == target.ID {
			t.Error("target should not appear in structural: its only linker is outside the recency window")
		}
	}
}

func TestGetSignificance_Uncurated(t *testing.T) {
	s := newStore(t)
	// Target node has no occurred_at but has inbound edges — should appear in uncurated.
	target := mustAddNode(t, s, "Undated hub", "proj")
	linker := mustAddNode(t, s, "Active linker", "proj")
	if _, err := s.AddEdge(linker.ID, target.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}

	foundStructural := false
	for _, sn := range res.Structural {
		if sn.ID == target.ID {
			foundStructural = true
		}
	}
	if !foundStructural {
		t.Fatal("target should appear in structural (has inbound edges)")
	}

	foundUncurated := false
	for _, sn := range res.Uncurated {
		if sn.ID == target.ID {
			foundUncurated = true
		}
	}
	if !foundUncurated {
		t.Error("target should appear in uncurated (in structural top-N but has no occurred_at)")
	}
}

func TestGetSignificance_PotentiallyStale(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Node with occurred_at but no inbound edges — structurally irrelevant.
	isolated, _ := s.AddNode("Isolated declared node", "d", "w", "proj", ptr(ts), "", "")

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}

	foundDeclared := false
	for _, n := range res.Declared {
		if n.ID == isolated.ID {
			foundDeclared = true
		}
	}
	if !foundDeclared {
		t.Error("isolated node with occurred_at should appear in declared")
	}

	foundPotentiallyStale := false
	for _, n := range res.PotentiallyStale {
		if n.ID == isolated.ID {
			foundPotentiallyStale = true
		}
	}
	if !foundPotentiallyStale {
		t.Error("isolated declared node with no inbound edges should appear in potentially_stale")
	}

	for _, sn := range res.Structural {
		if sn.ID == isolated.ID {
			t.Error("isolated node should not appear in structural (no inbound edges)")
		}
	}
}

func TestGetSignificance_Logging(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	target := mustAddNode(t, s, "Logged target", "proj")
	linker := mustAddNode(t, s, "Logged linker", "proj")
	if _, err := s.AddEdge(linker.ID, target.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	if len(res.Structural) == 0 {
		t.Fatal("need at least one structural result for logging test")
	}

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(`SELECT call_id, rank_type FROM significance_log`)
	if err != nil {
		t.Fatalf("query significance_log: %v", err)
	}
	defer rows.Close()

	callIDs := map[string]bool{}
	rankTypes := map[string]bool{}
	rowCount := 0
	for rows.Next() {
		var callID, rankType string
		if err := rows.Scan(&callID, &rankType); err != nil {
			t.Fatalf("scan significance_log: %v", err)
		}
		callIDs[callID] = true
		rankTypes[rankType] = true
		rowCount++
	}
	if rowCount == 0 {
		t.Error("significance_log should have rows after GetSignificance call")
	}
	if len(callIDs) != 1 {
		t.Errorf("all rows should share a single call_id, got %d distinct call IDs", len(callIDs))
	}
	if !rankTypes["structural"] {
		t.Error("significance_log should contain at least one 'structural' rank_type entry")
	}
}

func TestGetSignificance_ArchivedExcluded(t *testing.T) {
	s := newStore(t)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Archived node with occurred_at — should not appear in declared.
	n1, _ := s.AddNode("Archived declared", "d", "w", "proj", ptr(ts), "", "")
	s.ArchiveNode(n1.ID, "testing")

	// Archived target node with an inbound edge — should not appear in structural.
	n2 := mustAddNode(t, s, "Archived structural target", "proj")
	linker := mustAddNode(t, s, "Active linker", "proj")
	if _, err := s.AddEdge(linker.ID, n2.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	s.ArchiveNode(n2.ID, "testing")

	res, err := s.GetSignificance("proj", 10, 90, nil)
	if err != nil {
		t.Fatalf("GetSignificance: %v", err)
	}
	for _, n := range res.Declared {
		if n.ID == n1.ID {
			t.Error("archived node should not appear in declared")
		}
	}
	for _, sn := range res.Structural {
		if sn.ID == n2.ID {
			t.Error("archived node should not appear in structural")
		}
	}
}
