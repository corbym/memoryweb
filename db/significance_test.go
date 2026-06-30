package db_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

func TestGetSignificance_Empty(t *testing.T) {
	s := newStore(t)
	res, err := s.GetSignificance("empty-domain", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

	res, err := s.GetSignificance("proj", 10, 90, nil, nil)
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

// ── GetTrust ──────────────────────────────────────────────────────────────────

func TestGetTrust_FindingBackedScoresHigherThanAssumptionBacked(t *testing.T) {
	s := newStore(t)
	findingBacked, _ := s.AddNode("Finding-backed decision", "d", "w", "proj", nil, "", "decision")
	assumptionBacked, _ := s.AddNode("Assumption-backed decision", "d", "w", "proj", nil, "", "decision")

	for i := 0; i < 3; i++ {
		f, _ := s.AddNode(fmt.Sprintf("Finding %d", i), "d", "w", "proj", nil, "", "finding")
		if _, err := s.AddEdge(f.ID, findingBacked.ID, "connects_to", ""); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		a, _ := s.AddNode(fmt.Sprintf("Assumption %d", i), "d", "w", "proj", nil, "", "assumption")
		if _, err := s.AddEdge(a.ID, assumptionBacked.ID, "connects_to", ""); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	var findingScore, assumptionScore float64
	for _, n := range res.Nodes {
		if n.ID == findingBacked.ID {
			findingScore = n.TrustScore
		}
		if n.ID == assumptionBacked.ID {
			assumptionScore = n.TrustScore
		}
	}
	if findingScore <= assumptionScore {
		t.Errorf("finding-backed score %f should be higher than assumption-backed score %f", findingScore, assumptionScore)
	}
}

func TestGetTrust_ContradictsPenalty(t *testing.T) {
	s := newStore(t)
	plain, _ := s.AddNode("Plain standing rule", "d", "w", "proj", nil, "", "standing")
	contradicted, _ := s.AddNode("Contradicted standing rule", "d", "w", "proj", nil, "", "standing")
	finding, _ := s.AddNode("Contradicting finding", "d", "w", "proj", nil, "", "finding")
	if _, err := s.AddEdge(finding.ID, contradicted.ID, "contradicts", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	var plainScore, contradictedScore float64
	for _, n := range res.Nodes {
		if n.ID == plain.ID {
			plainScore = n.TrustScore
		}
		if n.ID == contradicted.ID {
			contradictedScore = n.TrustScore
		}
	}
	if contradictedScore >= plainScore {
		t.Errorf("contradicted score %f should be lower than plain score %f", contradictedScore, plainScore)
	}
}

func TestGetTrust_ReferenceExcludedFromOutput(t *testing.T) {
	s := newStore(t)
	ref, _ := s.AddNode("A person", "d", "w", "proj", nil, "", "reference")

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	for _, n := range res.Nodes {
		if n.ID == ref.ID {
			t.Error("reference node should not appear in trust output")
		}
	}
}

func TestGetTrust_ReferenceCountsAsZeroWeightNeighbour(t *testing.T) {
	s := newStore(t)
	isolated, _ := s.AddNode("Isolated decision", "d", "w", "proj", nil, "", "decision")
	refBacked, _ := s.AddNode("Reference-backed decision", "d", "w", "proj", nil, "", "decision")
	ref, _ := s.AddNode("A system", "d", "w", "proj", nil, "", "reference")
	if _, err := s.AddEdge(ref.ID, refBacked.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	var isolatedScore, refBackedScore float64
	for _, n := range res.Nodes {
		if n.ID == isolated.ID {
			isolatedScore = n.TrustScore
		}
		if n.ID == refBacked.ID {
			refBackedScore = n.TrustScore
		}
	}
	if isolatedScore != refBackedScore {
		t.Errorf("reference-backed score %f should equal isolated score %f (reference contributes zero weight)", refBackedScore, isolatedScore)
	}
}

func TestGetTrust_TransientExcludedFromOutput(t *testing.T) {
	s := newStore(t)
	tn, _ := s.AddNode("A short-lived note", "d", "w", "proj", nil, "", "transient")

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	for _, n := range res.Nodes {
		if n.ID == tn.ID {
			t.Error("transient node should not appear in trust output")
		}
	}
}

func TestGetTrust_TrustBasisNonEmptyWithNoNeighbours(t *testing.T) {
	s := newStore(t)
	isolated, _ := s.AddNode("Isolated decision", "d", "w", "proj", nil, "", "decision")

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	for _, n := range res.Nodes {
		if n.ID == isolated.ID {
			if n.TrustBasis == "" || !strings.Contains(n.TrustBasis, "self:") {
				t.Errorf("trust_basis should be non-empty and contain 'self:', got %q", n.TrustBasis)
			}
			return
		}
	}
	t.Fatal("isolated node not found in result")
}

func TestGetTrust_ScoresNormalisedToZeroOne(t *testing.T) {
	s := newStore(t)
	s.AddNode("Low", "d", "w", "proj", nil, "", "assumption")
	s.AddNode("High", "d", "w", "proj", nil, "", "finding")

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	if len(res.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	for _, n := range res.Nodes {
		if n.TrustScore < 0 || n.TrustScore > 1 {
			t.Errorf("node %s: trust_score %f out of [0,1]", n.ID, n.TrustScore)
		}
	}
}

func TestGetTrust_RecencyWindowExcludesStaleNeighbours(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	target, _ := s.AddNode("Target decision", "d", "w", "proj", nil, "", "decision")
	staleFinding, _ := s.AddNode("Stale finding", "d", "w", "proj", nil, "", "finding")
	if _, err := s.AddEdge(staleFinding.ID, target.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	baseline, _ := s.AddNode("No neighbours decision", "d", "w", "proj", nil, "", "decision")

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(`UPDATE nodes SET updated_at = datetime('now', '-200 days') WHERE id = ?`, staleFinding.ID); err != nil {
		rawDB.Close()
		t.Fatalf("backdate updated_at: %v", err)
	}
	rawDB.Close()

	res, err := s.GetTrust("proj", 10, 90, nil, nil)
	if err != nil {
		t.Fatalf("GetTrust: %v", err)
	}
	var targetScore, baselineScore float64
	for _, n := range res.Nodes {
		if n.ID == target.ID {
			targetScore = n.TrustScore
		}
		if n.ID == baseline.ID {
			baselineScore = n.TrustScore
		}
	}
	if targetScore != baselineScore {
		t.Errorf("target's only neighbour is outside the recency window, so its score (%f) should equal a neighbourless node's score (%f)", targetScore, baselineScore)
	}
}

func TestGetTrustForMemoryID_ScopesToNeighbourhood(t *testing.T) {
	s := newStore(t)
	anchor, _ := s.AddNode("Anchor decision", "d", "w", "proj", nil, "", "decision")
	neighbourFinding, _ := s.AddNode("Neighbour finding", "d", "w", "proj", nil, "", "finding")
	outside, _ := s.AddNode("Outside finding", "d", "w", "other-domain", nil, "", "finding")
	if _, err := s.AddEdge(neighbourFinding.ID, anchor.ID, "connects_to", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res, err := s.GetTrustForMemoryID(anchor.ID, 2, 90, nil)
	if err != nil {
		t.Fatalf("GetTrustForMemoryID: %v", err)
	}
	found := false
	for _, n := range res.Nodes {
		if n.ID == anchor.ID {
			found = true
		}
		if n.ID == outside.ID {
			t.Error("cross-domain node should not appear (domain-clipped)")
		}
	}
	if !found {
		t.Error("anchor should appear in its own neighbourhood trust result")
	}
}
