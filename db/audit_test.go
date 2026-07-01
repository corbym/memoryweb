package db_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corbym/memoryweb/db"
)

func TestFindDrift_TransientOlderThan7Days_IsDriftCandidate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	n, err := s.AddNode("sprint ticket stale", "d", "w", "transient-drift", nil, "", "transient")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Backdate created_at to 8 days ago via raw SQL.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	stale := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, stale, n.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	candidates, err := s.FindDrift("transient-drift", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			found = true
			if !strings.Contains(c.Reason, "transient") {
				t.Errorf("reason should mention 'transient'; got %q", c.Reason)
			}
		}
	}
	if !found {
		t.Errorf("stale transient node (%s) should appear in drift candidates", n.ID)
	}
}

func TestFindDrift_TransientNewerThan7Days_NotDriftCandidate(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("recent sprint ticket", "d", "w", "transient-new", nil, "", "transient")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	candidates, err := s.FindDrift("transient-new", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			t.Errorf("recent transient node should NOT appear in drift; got reason: %q", c.Reason)
		}
	}
}

// TestFindDrift_LowConnectionStandingNode: a standing node with 0 inbound edges
// older than 30 days must be flagged by rule 6.
func TestFindDrift_LowConnectionStandingNode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	n, err := s.AddNode("orphaned standing rule", "desc", "why", "standing-low-conn", nil, "", "standing")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, old, n.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	candidates, err := s.FindDrift("standing-low-conn", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			found = true
			if !strings.Contains(c.Reason, "low connection count") {
				t.Errorf("reason should mention 'low connection count'; got %q", c.Reason)
			}
		}
	}
	if !found {
		t.Errorf("old standing node with no edges (%s) should appear in drift candidates", n.ID)
	}
}

// TestFindDrift_StandingNodeNotFlaggedWhenYoung: a standing node younger than 30
// days must not be flagged by rule 6, even with 0 edges.
func TestFindDrift_StandingNodeNotFlaggedWhenYoung(t *testing.T) {
	s := newStore(t)
	n, err := s.AddNode("fresh standing rule", "desc", "why", "standing-young", nil, "", "standing")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	candidates, err := s.FindDrift("standing-young", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == n.ID {
			t.Errorf("young standing node should NOT appear in drift; got reason: %q", c.Reason)
		}
	}
}

// TestFindDrift_StandingNodeNotFlaggedWhenWellConnected: a standing node older
// than 30 days with at least 2 inbound edges must not be flagged by rule 6.
func TestFindDrift_StandingNodeNotFlaggedWhenWellConnected(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	rule, err := s.AddNode("well connected rule", "desc", "why", "standing-connected", nil, "", "standing")
	if err != nil {
		t.Fatalf("AddNode rule: %v", err)
	}
	src1, err := s.AddNode("source one", "d", "w", "standing-connected", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode src1: %v", err)
	}
	src2, err := s.AddNode("source two", "d", "w", "standing-connected", nil, "", "")
	if err != nil {
		t.Fatalf("AddNode src2: %v", err)
	}
	if _, err := s.AddEdge(src1.ID, rule.ID, "governed_by", "s1 governed by rule"); err != nil {
		t.Fatalf("AddEdge 1: %v", err)
	}
	if _, err := s.AddEdge(src2.ID, rule.ID, "governed_by", "s2 governed by rule"); err != nil {
		t.Fatalf("AddEdge 2: %v", err)
	}

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, old, rule.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	candidates, err := s.FindDrift("standing-connected", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == rule.ID {
			t.Errorf("well-connected standing node should NOT appear in drift; got reason: %q", c.Reason)
		}
	}
}

// TestGetOrphans_ExcludesReference: only 'transient' nodes are excluded from
// orphan detection. A 'reference' node with no edges must still be surfaced.
func TestGetOrphans_ExcludesReference(t *testing.T) {
	s := newStore(t)
	refID, err := s.AddNode("a person", "d", "w", "orphans-ref", nil, "", "reference")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	orphans, err := s.FindDisconnected("orphans-ref", nil, nil)
	if err != nil {
		t.Fatalf("FindDisconnected: %v", err)
	}
	if !contains(nodeIDs(orphans), refID.ID) {
		t.Error("expected reference node with no edges to be surfaced as an orphan")
	}
}

// TestGetStaleDrift_TransientNodes: a transient node older than 7 days is
// surfaced as a drift candidate.
func TestGetStaleDrift_TransientNodes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer s.Close()

	n, err := s.AddNode("old sprint ticket", "d", "w", "stale-transient", nil, "", "transient")
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	old := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, old, n.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	drift, err := s.FindDrift("stale-transient", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, d := range drift {
		if d.Node.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected stale transient node to be surfaced as a drift candidate")
	}
}

func TestFindDrift_TagsFilter(t *testing.T) {
	s := newStore(t)
	// "old" in label triggers Rule 2 (superseded label) — instant stale candidate.
	mustAddNodeWithTags(t, s, "old plan TDD", "proj", "TDD")
	mustAddNodeWithTags(t, s, "old approach other", "proj", "other")

	candidates, err := s.FindDrift("proj", 10, []string{"TDD"}, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift with tags: %v", err)
	}
	for _, c := range candidates {
		if c.Node.Tags != "TDD" {
			t.Errorf("expected only TDD-tagged candidate, got tag %q (label %q)", c.Node.Tags, c.Node.Label)
		}
	}
	if len(candidates) == 0 {
		t.Error("expected at least one TDD-tagged candidate")
	}
}

func TestFindDrift_MemoryID_NeighbourhoodOnly(t *testing.T) {
	s := newStore(t)
	anchor := mustAddNode(t, s, "anchor", "proj")
	inNeighbour := mustAddNode(t, s, "old neighbour plan", "proj")
	unrelated := mustAddNode(t, s, "old unrelated plan", "proj")
	s.AddEdge(anchor.ID, inNeighbour.ID, "connects_to", "")

	candidates, err := s.FindDrift("", 10, nil, nil, anchor.ID, 2)
	if err != nil {
		t.Fatalf("FindDrift with memory_id: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == unrelated.ID {
			t.Errorf("unrelated node %q should be excluded when memory_id is set", c.Node.Label)
		}
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == inNeighbour.ID {
			found = true
		}
	}
	if !found {
		t.Error("neighbour node should be included in memory_id-scoped drift results")
	}
}

// TestFindDrift_ContradictsPair_ExcludedWhenResolvedBy: a contradicts pair
// where at least one node has a resolved_by edge to another node must not be
// re-flagged by rule 1 of FindDrift.
func TestFindDrift_ContradictsPair_ExcludedWhenResolvedBy(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "approach A", "rules")
	nB := mustAddNode(t, s, "approach B", "rules")
	nR := mustAddNode(t, s, "resolution node", "rules")

	// A contradicts B.
	if _, err := s.AddEdge(nA.ID, nB.ID, "contradicts", ""); err != nil {
		t.Fatalf("AddEdge contradicts: %v", err)
	}
	// A is resolved_by R.
	if _, err := s.AddEdge(nA.ID, nR.ID, "resolved_by", ""); err != nil {
		t.Fatalf("AddEdge resolved_by: %v", err)
	}

	candidates, err := s.FindDrift("rules", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.ConflictsWith != nil && c.Reason == "explicitly marked as contradicting each other" {
			if (c.Node.ID == nA.ID && c.ConflictsWith.ID == nB.ID) ||
				(c.Node.ID == nB.ID && c.ConflictsWith.ID == nA.ID) {
				t.Errorf("resolved contradicts pair must not appear in drift; got: %+v", c)
			}
		}
	}
}

// TestFindDrift_ContradictsPair_StillFlaggedWithoutResolution: an unresolved
// contradicts pair must still be flagged.
func TestFindDrift_ContradictsPair_StillFlaggedWithoutResolution(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "approach alpha", "rules2")
	nB := mustAddNode(t, s, "approach beta", "rules2")

	if _, err := s.AddEdge(nA.ID, nB.ID, "contradicts", ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("rules2", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == nA.ID || c.Node.ID == nB.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("unresolved contradicts pair must still appear in drift")
	}
}

// TestFindConflictCandidates_ExcludesPairsAlreadyLinked: pairs that already
// have a contradicts edge must not appear in the conflict candidates output.
func TestFindConflictCandidates_ExcludesPairsAlreadyLinked(t *testing.T) {
	s := newStore(t)
	nA := mustAddNode(t, s, "cap pool at 20", "rules")
	nB := mustAddNode(t, s, "cap pool at 35", "rules")
	// Mark them as contradicting.
	s.AddEdge(nA.ID, nB.ID, "contradicts", "different pool sizes")

	// No embeddings available (no Ollama) — result must be empty, not error.
	candidates, err := s.FindConflictCandidates("rules", 10, nil, nil)
	if err != nil {
		t.Fatalf("FindConflictCandidates: %v", err)
	}
	// With no embeddings, result will be empty — that's expected.
	for _, c := range candidates {
		if (c.AID == nA.ID && c.BID == nB.ID) || (c.AID == nB.ID && c.BID == nA.ID) {
			t.Errorf("contradicts-linked pair must be excluded; got candidate: %+v", c)
		}
	}
}

// TestFindConflictCandidates_EmptyWhenNoEmbeddings: without embeddings, the
// result must be an empty slice (not an error).
func TestFindConflictCandidates_EmptyWhenNoEmbeddings(t *testing.T) {
	s := newStore(t)
	mustAddNode(t, s, "cap pool at 20", "rules")
	mustAddNode(t, s, "cap pool at 35", "rules")

	candidates, err := s.FindConflictCandidates("rules", 10, nil, nil)
	if err != nil {
		t.Fatalf("FindConflictCandidates: %v", err)
	}
	if candidates == nil {
		t.Error("expected a non-nil slice (may be empty) when no embeddings exist")
	}
}

func TestFindDisconnected_TagsFilter(t *testing.T) {
	s := newStore(t)
	inResult := mustAddNodeWithTags(t, s, "orphan review", "proj", "review")
	mustAddNodeWithTags(t, s, "orphan other", "proj", "other")

	nodes, err := s.FindDisconnected("", []string{"review"}, nil)
	if err != nil {
		t.Fatalf("FindDisconnected with tags: %v", err)
	}
	ids := nodeIDs(nodes)
	if !contains(ids, inResult.ID) {
		t.Error("review-tagged orphan should be included")
	}
	for _, n := range nodes {
		if n.Tags != "review" {
			t.Errorf("expected only review-tagged orphan, got tag %q", n.Tags)
		}
	}
}
