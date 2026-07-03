package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/corbym/memoryweb/db"
)

// ── raw-SQL verification helpers ───────────────────────────────────────────────
//
// These bypass the Store entirely and query the underlying SQLite file
// directly, exactly like an external auditor (or "doctor") would, so a bug in
// Store's own counting methods can't mask a bug in Purge itself.

func rawCount(t *testing.T, dbPath, query string, args ...interface{}) int {
	t.Helper()
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("raw query %q: %v", query, err)
	}
	return n
}

func rawNodeExists(t *testing.T, dbPath, id string) bool {
	t.Helper()
	return rawCount(t, dbPath, `SELECT COUNT(*) FROM nodes WHERE id = ?`, id) > 0
}

func rawEdgeTouchingNode(t *testing.T, dbPath, id string) bool {
	t.Helper()
	return rawCount(t, dbPath, `SELECT COUNT(*) FROM edges WHERE from_node = ? OR to_node = ?`, id, id) > 0
}

// newStoreAtPath is like newStore but returns the backing file path too, so
// callers can re-open the file with a completely independent connection
// (mirroring how the CLI's `doctor` subcommand and `purge` subcommand are
// always separate processes hitting the same file).
func newStoreAtPath(t *testing.T) (dbPath string, s *db.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "test.db")
	var err error
	s, err = db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return dbPath, s
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestPurge_DomainScoped_DeletesOnlyMatchingDomain proves that Purge(domain=X)
// removes archived nodes in domain X and leaves archived nodes in every other
// domain completely untouched.
func TestPurge_DomainScoped_DeletesOnlyMatchingDomain(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	a := mustAddNode(t, s, "Domain A Archived 1", "domain-a")
	b := mustAddNode(t, s, "Domain A Archived 2", "domain-a")
	c := mustAddNode(t, s, "Domain A Live", "domain-a")
	d := mustAddNode(t, s, "Domain B Archived", "domain-b")

	mustArchive(t, s, a.ID)
	mustArchive(t, s, b.ID)
	mustArchive(t, s, d.ID)

	result, err := s.Purge("domain-a", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected 2 nodes purged, got %d (%v)", len(result.Nodes), nodeIDs(result.Nodes))
	}

	// Independent, fresh connection — same contract as the doctor CLI reading
	// the file after purge has exited.
	if rawNodeExists(t, dbPath, a.ID) {
		t.Error("BUG: domain-a archived node A still present in nodes table after purge")
	}
	if rawNodeExists(t, dbPath, b.ID) {
		t.Error("BUG: domain-a archived node B still present in nodes table after purge")
	}
	if !rawNodeExists(t, dbPath, c.ID) {
		t.Error("live node in domain-a must survive a purge (purge only ever targets archived nodes)")
	}
	if !rawNodeExists(t, dbPath, d.ID) {
		t.Error("archived node in a DIFFERENT domain must survive a domain-scoped purge")
	}
}

// TestPurge_DomainScoped_CascadesEdges proves every edge touching a purged
// node is gone (required by the edges→nodes foreign key), while edges that
// don't touch any purged node survive untouched.
func TestPurge_DomainScoped_CascadesEdges(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	a := mustAddNode(t, s, "Domain A Archived", "domain-a")
	b := mustAddNode(t, s, "Domain A Live", "domain-a")
	live1 := mustAddNode(t, s, "Unrelated Live 1", "domain-c")
	live2 := mustAddNode(t, s, "Unrelated Live 2", "domain-c")

	// Edges must be created while both endpoints are live.
	crossEdge, err := s.AddEdge(a.ID, b.ID, "connects_to", "a to live b")
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	unrelatedEdge, err := s.AddEdge(live1.ID, live2.ID, "connects_to", "unrelated")
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	mustArchive(t, s, a.ID)

	result, err := s.Purge("domain-a", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if result.TotalEdges != 1 {
		t.Errorf("expected 1 edge removed (the one touching the purged node), got %d", result.TotalEdges)
	}

	if rawEdgeTouchingNode(t, dbPath, a.ID) {
		t.Error("BUG: edge referencing the purged node still present in edges table")
	}
	if rawCount(t, dbPath, `SELECT COUNT(*) FROM edges WHERE id = ?`, crossEdge.ID) != 0 {
		t.Error("BUG: edge from the purged node to a live node was not deleted")
	}
	if rawCount(t, dbPath, `SELECT COUNT(*) FROM edges WHERE id = ?`, unrelatedEdge.ID) != 1 {
		t.Error("edge between two untouched live nodes must survive the purge")
	}
}

// TestPurge_DomainScoped_ResolvesAlias proves purging by an alias name
// resolves to the canonical domain the nodes are actually stored under.
func TestPurge_DomainScoped_ResolvesAlias(t *testing.T) {
	_, s := newStoreAtPath(t)

	n := mustAddNode(t, s, "Aliased Domain Node", "deep-game")
	mustArchive(t, s, n.ID)

	if err := s.AddAlias("dg", "deep-game"); err != nil {
		t.Fatalf("AddAlias: %v", err)
	}

	result, err := s.Purge("dg", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected the aliased domain's archived node to be purged, got %d nodes", len(result.Nodes))
	}
}

// TestPurge_DomainScoped_UnknownDomainPurgesNothing proves a domain with no
// archived nodes (typo, wrong case, etc.) safely purges zero nodes rather
// than falling back to a global purge.
func TestPurge_DomainScoped_UnknownDomainPurgesNothing(t *testing.T) {
	_, s := newStoreAtPath(t)

	n := mustAddNode(t, s, "Real Domain Node", "domain-a")
	mustArchive(t, s, n.ID)

	result, err := s.Purge("domain-a-typo", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 0 {
		t.Fatalf("expected 0 nodes for a non-matching domain, got %d", len(result.Nodes))
	}
}

// TestPurge_DomainScoped_DryRunLeavesEverythingInPlace proves dry-run never
// mutates the database, even when nodes match the domain filter.
func TestPurge_DomainScoped_DryRunLeavesEverythingInPlace(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	a := mustAddNode(t, s, "Domain A Archived", "domain-a")
	mustArchive(t, s, a.ID)

	result, err := s.Purge("domain-a", nil, true, false)
	if err != nil {
		t.Fatalf("Purge (dry-run): %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("dry-run should still report the 1 candidate, got %d", len(result.Nodes))
	}
	if result.TotalEdges != 0 {
		t.Errorf("dry-run must never report deleted edges, got %d", result.TotalEdges)
	}
	if !rawNodeExists(t, dbPath, a.ID) {
		t.Error("dry-run must not delete anything")
	}
}

// TestPurge_DomainScoped_ThenNodeCountsAndEdgeCountAreAccurate is the
// "doctor" regression test: after a domain-scoped purge, a brand-new Store
// (fresh connection, exactly what `memoryweb doctor` opens) must report
// live/archived node counts and edge counts that match the raw table state —
// not zero, and not the pre-purge totals.
func TestPurge_DomainScoped_ThenNodeCountsAndEdgeCountAreAccurate(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	a := mustAddNode(t, s, "Domain A Archived", "domain-a")
	_ = mustAddNode(t, s, "Domain A Live", "domain-a")
	d := mustAddNode(t, s, "Domain B Archived", "domain-b")

	edge, err := s.AddEdge(a.ID, d.ID, "connects_to", "cross domain, both archived later")
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	mustArchive(t, s, a.ID)
	mustArchive(t, s, d.ID)

	if _, err := s.Purge("domain-a", nil, false, false); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	s.Close()

	// Re-open completely independently, as `memoryweb doctor` would.
	doctorStore, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("re-open db as doctor would: %v", err)
	}
	defer doctorStore.Close()

	live, archived, err := doctorStore.NodeCounts()
	if err != nil {
		t.Fatalf("NodeCounts: %v", err)
	}
	edges, err := doctorStore.EdgeCount()
	if err != nil {
		t.Fatalf("EdgeCount: %v", err)
	}

	wantLive := rawCount(t, dbPath, `SELECT COUNT(*) FROM nodes WHERE archived_at IS NULL`)
	wantArchived := rawCount(t, dbPath, `SELECT COUNT(*) FROM nodes WHERE archived_at IS NOT NULL`)
	wantEdges := rawCount(t, dbPath, `SELECT COUNT(*) FROM edges`)

	if live != wantLive {
		t.Errorf("BUG: doctor-equivalent NodeCounts() reports %d live, raw table has %d", live, wantLive)
	}
	if archived != wantArchived {
		t.Errorf("BUG: doctor-equivalent NodeCounts() reports %d archived, raw table has %d", archived, wantArchived)
	}
	if edges != wantEdges {
		t.Errorf("BUG: doctor-equivalent EdgeCount() reports %d, raw table has %d", edges, wantEdges)
	}

	// domain-b's archived node must have survived (different domain) — this
	// is the concrete "still nodes/edges left after the purge" check, but
	// verified as *expected* survivors, not leaked purge targets.
	if archived != 1 {
		t.Errorf("expected exactly 1 archived node to remain (domain-b's), got %d", archived)
	}
	if !rawNodeExists(t, dbPath, d.ID) {
		t.Error("domain-b's archived node should NOT have been touched by a domain-a purge")
	}
	// The edge from the purged node to d must be gone even though d survives —
	// you cannot have an edge pointing at a node that no longer exists.
	if rawCount(t, dbPath, `SELECT COUNT(*) FROM edges WHERE id = ?`, edge.ID) != 0 {
		t.Error("BUG: edge from the purged node was not removed even though the node it pointed to was deleted")
	}
}

// TestPurge_ReportedNodeCountMatchesActualDeletions proves Purge's own return
// value (what `memoryweb purge` prints as "N node(s) purged, M edge(s)
// removed") is never a false positive — every ID it claims to have purged
// really is gone afterwards.
func TestPurge_ReportedNodeCountMatchesActualDeletions(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	var archivedIDs []string
	for i := 0; i < 5; i++ {
		n := mustAddNode(t, s, "Bulk Archived", "domain-bulk")
		mustArchive(t, s, n.ID)
		archivedIDs = append(archivedIDs, n.ID)
	}

	result, err := s.Purge("domain-bulk", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != len(archivedIDs) {
		t.Fatalf("expected %d nodes reported purged, got %d", len(archivedIDs), len(result.Nodes))
	}
	for _, id := range archivedIDs {
		if rawNodeExists(t, dbPath, id) {
			t.Errorf("BUG: node %s was reported as purged but is still in the nodes table", id)
		}
	}
}

// TestPurge_DomainScoped_MatchesCaseAndWhitespaceVariants reproduces the
// real-world bug report: on a long-lived database, archived nodes filed
// under slightly different raw domain strings ("Sedex" vs "sedex" vs
// "sedex " with trailing whitespace vs "SEDEX" — e.g. typed by different
// agent sessions over months, or filed before any naming convention was
// settled) must all be treated as the same domain by a domain-scoped purge.
// Previously the query did a byte-exact `domain = ?` comparison, so
// `purge --domain sedex --confirm` only deleted the exact-case,
// exact-whitespace matches, leaving the other spelling variants archived
// forever with zero signal — a subsequent `--dry-run` for the same literal
// string then falsely read as "nothing left to purge" while nodes/edges for
// the same conceptual domain were still sitting in the table.
//
// Purge now normalizes domain matching (trim + case-fold) so all of these
// variants are caught by a single scoped purge.
func TestPurge_DomainScoped_MatchesCaseAndWhitespaceVariants(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	canonical := mustAddNode(t, s, "Filed under canonical casing", "sedex")
	capitalized := mustAddNode(t, s, "Filed early on, before casing convention settled", "Sedex")
	trailingSpace := mustAddNode(t, s, "Filed with a trailing space typo", "sedex ")
	upper := mustAddNode(t, s, "Filed in all caps", "SEDEX")

	for _, id := range []string{canonical.ID, capitalized.ID, trailingSpace.ID, upper.ID} {
		mustArchive(t, s, id)
	}

	result, err := s.Purge("sedex", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 4 {
		t.Fatalf("expected all 4 case/whitespace variants of 'sedex' to be purged together, got %d (%v)",
			len(result.Nodes), nodeIDs(result.Nodes))
	}

	for _, id := range []string{canonical.ID, capitalized.ID, trailingSpace.ID, upper.ID} {
		if rawNodeExists(t, dbPath, id) {
			t.Errorf("domain-name variant node %s should have been purged along with 'sedex'", id)
		}
	}

	// A follow-up dry-run must genuinely report 0 — no more silent leftovers.
	dryRun, err := s.Purge("sedex", nil, true, false)
	if err != nil {
		t.Fatalf("Purge (dry-run): %v", err)
	}
	if len(dryRun.Nodes) != 0 {
		t.Fatalf("expected dry-run to report 0 remaining, got %d", len(dryRun.Nodes))
	}
	remainingArchived := rawCount(t, dbPath, `SELECT COUNT(*) FROM nodes WHERE archived_at IS NOT NULL`)
	if remainingArchived != 0 {
		t.Errorf("expected 0 archived nodes left for any spelling of 'sedex', got %d", remainingArchived)
	}
}

// TestPurge_DomainScoped_NormalizationDoesNotBleedIntoOtherDomains proves the
// case/whitespace normalization is still a precise match on the domain
// name itself — it must not turn into a substring or fuzzy match that
// accidentally sweeps up an unrelated domain.
func TestPurge_DomainScoped_NormalizationDoesNotBleedIntoOtherDomains(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	target := mustAddNode(t, s, "Sedex node", "Sedex")
	unrelated := mustAddNode(t, s, "Sedex Two node", "sedex-two")
	mustArchive(t, s, target.ID)
	mustArchive(t, s, unrelated.ID)

	result, err := s.Purge("sedex", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected exactly 1 node purged (the 'Sedex' variant only), got %d", len(result.Nodes))
	}
	if !rawNodeExists(t, dbPath, unrelated.ID) {
		t.Error("purging 'sedex' must not sweep up the distinct domain 'sedex-two'")
	}
}

// TestPurge_DomainScoped_LiveNodesReportedButNotDeletedByDefault reproduces
// the real-world confusion this feature closes: a user archives some nodes
// in a domain, purges them, and a follow-up dry-run correctly reports 0 —
// but the domain still has live (never-archived) nodes sitting in the table.
// Without visibility into that, "0 to purge" is easily misread as "domain is
// empty". Purge must surface the live count whenever a domain filter is used,
// while never touching those live nodes unless includeLive is set.
func TestPurge_DomainScoped_LiveNodesReportedButNotDeletedByDefault(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	archived := mustAddNode(t, s, "Archived commercial node", "recordari-commercial")
	live1 := mustAddNode(t, s, "Live commercial node 1", "recordari-commercial")
	live2 := mustAddNode(t, s, "Live commercial node 2", "recordari-commercial")
	mustArchive(t, s, archived.ID)

	result, err := s.Purge("recordari-commercial", nil, false, false)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected only the 1 archived node purged, got %d", len(result.Nodes))
	}
	if result.LiveRemaining != 2 {
		t.Errorf("expected LiveRemaining=2 (the two never-archived nodes), got %d", result.LiveRemaining)
	}
	if !rawNodeExists(t, dbPath, live1.ID) || !rawNodeExists(t, dbPath, live2.ID) {
		t.Error("live nodes must survive a default (includeLive=false) purge")
	}

	// The follow-up dry-run must ALSO report the live count, so an operator
	// re-checking doesn't lose this signal.
	dryRun, err := s.Purge("recordari-commercial", nil, true, false)
	if err != nil {
		t.Fatalf("Purge (dry-run): %v", err)
	}
	if len(dryRun.Nodes) != 0 {
		t.Fatalf("expected 0 archived candidates left, got %d", len(dryRun.Nodes))
	}
	if dryRun.LiveRemaining != 2 {
		t.Errorf("expected dry-run to still report LiveRemaining=2, got %d", dryRun.LiveRemaining)
	}
}

// TestPurge_IncludeLive_HardDeletesLiveNodesInDomain proves the escape hatch:
// with includeLive=true and a domain filter, purge removes ALL nodes in that
// domain regardless of archive status, and LiveRemaining reports 0 since
// nothing was left behind.
func TestPurge_IncludeLive_HardDeletesLiveNodesInDomain(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	archived := mustAddNode(t, s, "Archived commercial node", "recordari-commercial")
	live := mustAddNode(t, s, "Live commercial node", "recordari-commercial")
	untouched := mustAddNode(t, s, "Different domain node", "other-domain")
	mustArchive(t, s, archived.ID)

	result, err := s.Purge("recordari-commercial", nil, false, true)
	if err != nil {
		t.Fatalf("Purge (includeLive): %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Fatalf("expected both the archived and live node purged, got %d (%v)", len(result.Nodes), nodeIDs(result.Nodes))
	}
	if result.LiveRemaining != 0 {
		t.Errorf("expected LiveRemaining=0 after includeLive purge, got %d", result.LiveRemaining)
	}
	if rawNodeExists(t, dbPath, archived.ID) {
		t.Error("archived node should have been purged")
	}
	if rawNodeExists(t, dbPath, live.ID) {
		t.Error("live node should have been purged with includeLive=true")
	}
	if !rawNodeExists(t, dbPath, untouched.ID) {
		t.Error("nodes in a different domain must never be touched by includeLive")
	}
}

// TestPurge_IncludeLive_DryRunDoesNotDelete proves includeLive respects
// dry-run just like the default archived-only path.
func TestPurge_IncludeLive_DryRunDoesNotDelete(t *testing.T) {
	dbPath, s := newStoreAtPath(t)

	live := mustAddNode(t, s, "Live node", "domain-x")

	result, err := s.Purge("domain-x", nil, true, true)
	if err != nil {
		t.Fatalf("Purge (includeLive dry-run): %v", err)
	}
	if len(result.Nodes) != 1 {
		t.Fatalf("expected dry-run to report the 1 live candidate, got %d", len(result.Nodes))
	}
	if !rawNodeExists(t, dbPath, live.ID) {
		t.Error("dry-run must never delete anything, even with includeLive")
	}
}

// ── local helpers ─────────────────────────────────────────────────────────────

func mustArchive(t *testing.T, s *db.Store, id string) {
	t.Helper()
	if err := s.ArchiveNode(id, "stale"); err != nil {
		t.Fatalf("ArchiveNode(%s): %v", id, err)
	}
}
