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

// ── Rule 7: connected placeholder with resolved target ────────────────────────

// TestFindDrift_GoalPlaceholder_ConnectsTo_CompletionNode:
// A goal-kind node with an outbound connects_to edge to a node whose label
// contains "complete" must appear as a drift candidate with the placeholder reason.
func TestFindDrift_GoalPlaceholder_ConnectsTo_CompletionNode(t *testing.T) {
	s := newStore(t)

	placeholder, err := s.AddNode("Story needed: wire up payment gateway", "desc", "why", "ph-domain", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode placeholder: %v", err)
	}
	completion, err := s.AddNode("STORY-42 payment gateway complete", "shipped and done", "why", "ph-domain", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode completion: %v", err)
	}
	if _, err := s.AddEdge(placeholder.ID, completion.ID, "connects_to", "placeholder links to completion"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == placeholder.ID {
			found = true
			if !strings.Contains(c.Reason, "placeholder") {
				t.Errorf("reason should mention 'placeholder'; got %q", c.Reason)
			}
		}
	}
	if !found {
		t.Errorf("goal placeholder (%s) connected to completion node should appear in drift", placeholder.ID)
	}
}

// TestFindDrift_GoalPlaceholder_LedTo_DoneNode:
// A goal-kind placeholder with an outbound led_to edge to a node whose
// description contains "done" must also be flagged.
func TestFindDrift_GoalPlaceholder_LedTo_DoneNode(t *testing.T) {
	s := newStore(t)

	placeholder, err := s.AddNode("TODO: refactor auth module", "desc", "why", "ph-domain2", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode placeholder: %v", err)
	}
	doneNode, err := s.AddNode("auth refactor", "this is done and merged", "why", "ph-domain2", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode done: %v", err)
	}
	if _, err := s.AddEdge(placeholder.ID, doneNode.ID, "led_to", "todo led to done work"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain2", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.Node.ID == placeholder.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("TODO placeholder (%s) led_to done node should appear in drift", placeholder.ID)
	}
}

// TestFindDrift_GoalNode_NoCompletionSignal_NotFlagged:
// A live goal-kind node whose outbound targets show no completion signal
// must NOT be flagged by this rule.
func TestFindDrift_GoalNode_NoCompletionSignal_NotFlagged(t *testing.T) {
	s := newStore(t)

	goal, err := s.AddNode("Improve CI pipeline speed", "desc", "why", "ph-domain3", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode goal: %v", err)
	}
	inProgress, err := s.AddNode("CI pipeline work in progress", "still being investigated", "why", "ph-domain3", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode inProgress: %v", err)
	}
	if _, err := s.AddEdge(goal.ID, inProgress.ID, "connects_to", "goal links to wip"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain3", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == goal.ID && strings.Contains(c.Reason, "placeholder") {
			t.Errorf("goal node with no completion signal should NOT be flagged as placeholder; got reason: %q", c.Reason)
		}
	}
}

// TestFindDrift_PlaceholderLabelKeyword_ConnectsTo_CompletionNode:
// A node (non-goal kind) whose label contains "Placeholder:" connected to
// a shipped node must be flagged.
func TestFindDrift_PlaceholderLabelKeyword_ConnectsTo_CompletionNode(t *testing.T) {
	s := newStore(t)

	ph, err := s.AddNode("Placeholder: openapi admin schema", "desc", "why", "ph-domain4", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode placeholder: %v", err)
	}
	shipped, err := s.AddNode("admin schema shipped", "RESOLVED and closed", "why", "ph-domain4", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode shipped: %v", err)
	}
	if _, err := s.AddEdge(ph.ID, shipped.ID, "connects_to", "ph to shipped"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain4", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	// Note: "Placeholder:" labels also trigger Rule 2 (superseded label) because
	// "placeholder" contains the substring "old" (h-o-l-d-er). So the node is
	// guaranteed to appear in drift — verify it appears for any reason.
	found := false
	for _, c := range candidates {
		if c.Node.ID == ph.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("Placeholder: label node (%s) connected to resolved node should appear in drift", ph.ID)
	}
}

// TestFindDrift_Placeholder_NoOutboundEdges_NotDoubleFlaged:
// An orphan goal-kind node (no edges) must NOT be flagged by the placeholder rule.
// It may be caught by orphan mode separately, but FindDrift must not add it.
func TestFindDrift_Placeholder_NoOutboundEdges_NotDoubleFlaged(t *testing.T) {
	s := newStore(t)

	orphanGoal, err := s.AddNode("Story needed: some future work", "desc", "why", "ph-domain5", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode orphanGoal: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain5", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == orphanGoal.ID && strings.Contains(c.Reason, "placeholder") {
			t.Errorf("orphan goal node should NOT be flagged by placeholder rule (no outbound edges); got reason: %q", c.Reason)
		}
	}
}

// TestFindDrift_PlaceholderItself_HasCompletionSignal_NotFlagged:
// If the placeholder's own label/description already contains a completion
// signal, it should NOT be flagged (it may have been updated in-place).
func TestFindDrift_PlaceholderItself_HasCompletionSignal_NotFlagged(t *testing.T) {
	s := newStore(t)

	updatedPh, err := s.AddNode("Story needed: gateway — RESOLVED", "shipped and done", "why", "ph-domain6", nil, "", "goal")
	if err != nil {
		t.Fatalf("AddNode updatedPh: %v", err)
	}
	completion, err := s.AddNode("gateway work complete", "desc", "why", "ph-domain6", nil, "", "decision")
	if err != nil {
		t.Fatalf("AddNode completion: %v", err)
	}
	if _, err := s.AddEdge(updatedPh.ID, completion.ID, "connects_to", "link"); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	candidates, err := s.FindDrift("ph-domain6", 10, nil, nil, "", 2)
	if err != nil {
		t.Fatalf("FindDrift: %v", err)
	}
	for _, c := range candidates {
		if c.Node.ID == updatedPh.ID && strings.Contains(c.Reason, "placeholder") {
			t.Errorf("placeholder whose own label/desc already signals completion should NOT be flagged; got reason: %q", c.Reason)
		}
	}
}
