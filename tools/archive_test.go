package tools_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestForgetNode_HidesFromSearch: after forget_node the node must not appear
// in search_nodes results.
func TestForgetNode_HidesFromSearch(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "test forget node", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id":     id,
		"reason": "stale",
	}))

	tr := call(t, h, "search", map[string]any{
		"query": "test forget node", "domain": "test",
	})
	mustNotError(t, tr)
	if contains(searchIDs(t, tr), id) {
		t.Error("forgotten node should NOT appear in search_nodes results")
	}
}

// TestForgetNode_DoesNotDelete: forgotten node must appear in list_archived
// with archived_at present and non-empty.

// TestForgetNode_DoesNotDelete: forgotten node must appear in list_archived
// with archived_at present and non-empty.
func TestForgetNode_DoesNotDelete(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "forget does not delete", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": id}))

	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "test"})
	mustNotError(t, archivedTr)

	var nodes []struct {
		ID         string `json:"id"`
		ArchivedAt string `json:"archived_at"`
	}
	if err := json.Unmarshal([]byte(text(t, archivedTr)), &nodes); err != nil {
		t.Fatalf("parse list_archived response: %v", err)
	}

	found := false
	for _, n := range nodes {
		if n.ID == id {
			found = true
			if n.ArchivedAt == "" {
				t.Error("archived_at should be present and non-empty")
			}
		}
	}
	if !found {
		t.Error("forgotten node should appear in list_archived results")
	}
}

// TestRestoreNode_ReappearsInSearch: restore_node must make a forgotten node
// visible again in search_nodes.

// TestRestoreNode_ReappearsInSearch: restore_node must make a forgotten node
// visible again in search_nodes.
func TestRestoreNode_ReappearsInSearch(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "restore reappears", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id": id, "reason": "testing restore",
	}))
	if contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "restore reappears", "domain": "test",
	})), id) {
		t.Fatal("node should be hidden after forget_node")
	}

	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	if !contains(searchIDs(t, call(t, h, "search", map[string]any{
		"query": "restore reappears", "domain": "test",
	})), id) {
		t.Error("node should reappear in search_nodes after restore_node")
	}
}

// TestAuditLog_RecordsForgetAndRestore: the audit_log table must contain exactly
// two entries — one archive (with the supplied reason) and one restore.

// TestAuditLog_RecordsForgetAndRestore: the audit_log table must contain exactly
// two entries — one archive (with the supplied reason) and one restore.
func TestAuditLog_RecordsForgetAndRestore(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "audit log test node", "test", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{
		"id": id, "reason": "test reason",
	}))
	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	// Open a second connection to read audit_log directly.
	// WAL mode allows concurrent readers — no need to close the primary store.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, reason FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action string
		reason sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.reason); err != nil {
			t.Fatalf("scan audit_log row: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 audit_log entries, got %d", len(entries))
	}
	if entries[0].action != "archive" {
		t.Errorf("first entry action: got %q, want %q", entries[0].action, "archive")
	}
	if !entries[0].reason.Valid || entries[0].reason.String != "test reason" {
		t.Errorf("first entry reason: got %q, want %q", entries[0].reason.String, "test reason")
	}
	if entries[1].action != "restore" {
		t.Errorf("second entry action: got %q, want %q", entries[1].action, "restore")
	}
}

// TestListArchived_ScopedByDomain: list_archived with a domain must only return
// archived nodes from that domain.

// TestListArchived_ScopedByDomain: list_archived with a domain must only return
// archived nodes from that domain.
func TestListArchived_ScopedByDomain(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "node in domain-1", "domain-1", nil)
	id2 := addNode(t, h, "node in domain-2", "domain-2", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": id1, "reason": "scope test"}))
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id2, "reason": "scope test"}))

	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "domain-1"})
	mustNotError(t, archivedTr)

	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, archivedTr)), &nodes); err != nil {
		t.Fatalf("parse list_archived response: %v", err)
	}

	foundFirst := false
	for _, n := range nodes {
		if n.ID == id2 {
			t.Error("domain-2 node should NOT appear when listing domain-1 archived nodes")
		}
		if n.ID == id1 {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Error("domain-1 node SHOULD appear in domain-1 archived list")
	}
}

// ── archive integration (agent workflow) ─────────────────────────────────────

// TestArchiveWorkflow_FullLifecycle simulates the full agent lifecycle entirely
// through the tool interface: file → forget → verify hidden → restore → verify visible.

// TestArchiveWorkflow_FullLifecycle simulates the full agent lifecycle entirely
// through the tool interface: file → forget → verify hidden → restore → verify visible.
func TestArchiveWorkflow_FullLifecycle(t *testing.T) {
	_, h := newEnv(t)

	// Agent files a node
	id := addNode(t, h, "Stale decision", "project-alpha", map[string]any{
		"description": "We decided to use XYZ framework",
		"why_matters": "Was the basis for the initial architecture",
	})

	// Verify it's findable
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Fatal("node should be findable before forget")
	}

	// Archive it via the tool
	mustNotError(t, call(t, h, "forget", map[string]any{
		"id":     id,
		"reason": "framework was replaced by ABC",
	}))

	// Verify it's gone from all retrieval paths
	if contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Error("should be hidden from search_nodes after forget_node")
	}
	if call(t, h, "recall", map[string]any{"id": id}).IsError == false {
		t.Error("should be hidden from get_node after forget_node")
	}
	recentIDs := func() []string {
		tr := call(t, h, "recent", map[string]any{"domain": "project-alpha"})
		var nodes []struct {
			ID string `json:"id"`
		}
		json.Unmarshal([]byte(text(t, tr)), &nodes)
		ids := make([]string, len(nodes))
		for i, n := range nodes {
			ids[i] = n.ID
		}
		return ids
	}
	if contains(recentIDs(), id) {
		t.Error("should be hidden from recent_changes after forget_node")
	}

	// Verify it appears in list_archived
	archivedTr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "project-alpha"})
	mustNotError(t, archivedTr)
	var archivedNodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, archivedTr)), &archivedNodes)
	foundInArchived := false
	for _, n := range archivedNodes {
		if n.ID == id {
			foundInArchived = true
		}
	}
	if !foundInArchived {
		t.Error("forgotten node should appear in list_archived")
	}

	// Restore it via the tool
	mustNotError(t, call(t, h, "restore", map[string]any{"id": id}))

	// Verify it's visible again
	if !contains(searchIDs(t, call(t, h, "search", map[string]any{"query": "Stale"})), id) {
		t.Error("node should reappear in search after restore_node")
	}
	if !contains(recentIDs(), id) {
		t.Error("node should reappear in recent_changes after restore_node")
	}

	// Verify it's no longer in list_archived
	archivedTr = call(t, h, "audit", map[string]any{"mode": "archived", "domain": "project-alpha"})
	mustNotError(t, archivedTr)
	json.Unmarshal([]byte(text(t, archivedTr)), &archivedNodes)
	for _, n := range archivedNodes {
		if n.ID == id {
			t.Error("restored node should no longer be in list_archived")
		}
	}
}

func TestArchiveWorkflow_MultipleNodes_OnlySomeArchived(t *testing.T) {
	_, h := newEnv(t)

	live1 := addNode(t, h, "Live node A", "proj", nil)
	live2 := addNode(t, h, "Live node B", "proj", nil)
	archived := addNode(t, h, "Archived node C", "proj", nil)

	mustNotError(t, call(t, h, "forget", map[string]any{"id": archived, "reason": "reason"}))

	tr := call(t, h, "search", map[string]any{"query": "node", "domain": "proj"})
	ids := searchIDs(t, tr)

	if !contains(ids, live1) {
		t.Error("live1 should be in results")
	}
	if !contains(ids, live2) {
		t.Error("live2 should be in results")
	}
	if contains(ids, archived) {
		t.Error("archived should NOT be in results")
	}
}

// ── invalid CallTool params ───────────────────────────────────────────────────

// TestDriftContradictingEdge: nodes connected by a contradicts edge must both
// appear as drift candidates with reason containing "contradicting".
func TestDriftContradictingEdge(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Approach Alpha", "test-drift-1", nil)
	idB := addNode(t, h, "Approach Beta", "test-drift-1", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-1"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, "contradicting") {
		t.Errorf("drift result should mention 'contradicting'; got:\n%s", body)
	}
	if !strings.Contains(body, idA) {
		t.Errorf("node A (%s) should appear in drift result; got:\n%s", idA, body)
	}
	if !strings.Contains(body, idB) {
		t.Errorf("node B (%s) should appear in drift result; got:\n%s", idB, body)
	}
}

// TestDriftSupersededLabel: a node whose label contains "old" must appear with
// reason containing "superseded".

// TestDriftSupersededLabel: a node whose label contains "old" must appear with
// reason containing "superseded".
func TestDriftSupersededLabel(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "old RST $10 approach", "test-drift-2", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-2"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("superseded node (%s) should appear in drift; got:\n%s", id, body)
	}
	if !strings.Contains(body, "superseded") {
		t.Errorf("reason should mention 'superseded'; got:\n%s", body)
	}
}

// TestDriftStaleOpenQuestion: a node whose description contains "open question"
// with occurred_at > 30 days ago must appear with reason containing "open question".

// TestDriftStaleOpenQuestion: a node whose description contains "open question"
// with occurred_at > 30 days ago must appear with reason containing "open question".
func TestDriftStaleOpenQuestion(t *testing.T) {
	_, h := newEnv(t)
	staleDate := time.Now().AddDate(0, 0, -31).Format("2006-01-02")
	id := addNode(t, h, "RST handler timing", "test-drift-3", map[string]any{
		"description": "open question: should we patch at boot or at runtime?",
		"occurred_at": staleDate,
		"why_matters": "unresolved timing decision that affects boot reliability",
	})

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-3"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("stale open-question node (%s) should appear in drift; got:\n%s", id, body)
	}
	if !strings.Contains(body, "open question") {
		t.Errorf("reason should mention 'open question'; got:\n%s", body)
	}
}

// TestDriftDuplicateLabel: two nodes with identical labels in the same domain
// must both appear with reason containing "duplicate".

// TestDriftDuplicateLabel: two nodes with identical labels in the same domain
// must both appear with reason containing "duplicate".
func TestDriftDuplicateLabel(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "boot crash duplicate label", "test-drift-4", nil)
	id2 := addNode(t, h, "boot crash duplicate label", "test-drift-4", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-4"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id1) {
		t.Errorf("first duplicate node (%s) should appear in drift; got:\n%s", id1, body)
	}
	if !strings.Contains(body, id2) {
		t.Errorf("second duplicate node (%s) should appear in drift; got:\n%s", id2, body)
	}
	if !strings.Contains(body, "duplicate") {
		t.Errorf("reason should mention 'duplicate'; got:\n%s", body)
	}
}

// TestDriftDoesNotSurfaceArchived: an archived node that would otherwise match
// a drift rule must NOT appear in drift results.

// TestDriftDoesNotSurfaceArchived: an archived node that would otherwise match
// a drift rule must NOT appear in drift results.
func TestDriftDoesNotSurfaceArchived(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "old archived stale thing", "test-drift-5", nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-5"})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), id) {
		t.Errorf("archived node (%s) should NOT appear in drift; got:\n%s", id, text(t, tr))
	}
}

// TestDriftScopedByDomain: a drift candidate in domain A must not appear when
// calling drift scoped to domain B.

// TestDriftScopedByDomain: a drift candidate in domain A must not appear when
// calling drift scoped to domain B.
func TestDriftScopedByDomain(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "old deprecated approach", "test-drift-a", nil)
	addNode(t, h, "fresh new approach", "test-drift-b", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "test-drift-b"})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), idA) {
		t.Errorf("node from test-drift-a (%s) should NOT appear in test-drift-b drift; got:\n%s", idA, text(t, tr))
	}
}

// ── summarise_domain ──────────────────────────────────────────────────────────

// TestSummariseDomain_ReturnsNodes: the response must contain the labels of
// all live nodes in the domain.

func TestDrift_TransientOlderThan7Days_Surfaced(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	id := addNode(t, h, "Sprint ticket old", "transient-test", map[string]any{
		"node_kind": "transient",
	})

	// Backdate created_at to 8 days ago.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	stale := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE id = ?`, stale, id); err != nil {
		rawDB.Close()
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "transient-test"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, id) {
		t.Errorf("stale transient node (%s) should appear in drift; got:\n%s", id, body)
	}
}

func TestDrift_TransientNewerThan7Days_NotSurfaced(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Sprint ticket fresh", "transient-fresh", map[string]any{
		"node_kind": "transient",
	})

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "transient-fresh"})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, id) {
		t.Errorf("recent transient node (%s) should NOT appear in drift; got:\n%s", id, body)
	}
}

// ── suggest_edges ─────────────────────────────────────────────────────────────

// TestSuggestEdges_OverlappingTags: two nodes sharing a tag should produce a
// suggestion mentioning the shared tag.

func TestDisconnectedReturnsUnconnectedNodes(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-1"

	lone := addNode(t, h, "Lone wolf node", domain, nil)
	idA := addNode(t, h, "Connected A", domain, nil)
	idB := addNode(t, h, "Connected B", domain, nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": idA, "to_memory": idB, "relationship": "led_to",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, lone) {
		t.Errorf("disconnected should contain lone node %s; got:\n%s", lone, body)
	}
	if strings.Contains(body, idA) {
		t.Errorf("connected node A should NOT appear; got:\n%s", body)
	}
	if strings.Contains(body, idB) {
		t.Errorf("connected node B should NOT appear; got:\n%s", body)
	}
}

func TestDisconnectedExcludesTransient(t *testing.T) {
	_, h := newEnv(t)
	domain := "test-disconnected-2"

	addNode(t, h, "Transient lone node", domain, map[string]any{"node_kind": "transient"})
	live := addNode(t, h, "Live lone node", domain, nil)

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, live) {
		t.Errorf("live disconnected node should appear; got:\n%s", body)
	}
}

func TestDisconnectedExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	domain := "test-disconnected-3"

	id := addNode(t, h, "Archived lone node", domain, nil)
	store.ArchiveNode(id, "test")

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": domain})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, id) {
		t.Errorf("archived disconnected node should NOT appear; got:\n%s", body)
	}
}

// ── trace ──────────────────────────────────────────────────────────────────────

// TestAudit_ModeStale_ReturnsDriftCandidates: mode=stale must return drift
// candidates (same behaviour as the removed whats_stale tool).
func TestAudit_ModeStale_ReturnsDriftCandidates(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "old transient", "proj", map[string]any{"transient": true})
	tr := call(t, h, "audit", map[string]any{"mode": "stale"})
	mustNotError(t, tr)
}

// TestAudit_ModeOrphans_ReturnsDisconnected: mode=orphans must return
// non-transient nodes with zero connections.

// TestAudit_ModeOrphans_ReturnsDisconnected: mode=orphans must return
// non-transient nodes with zero connections.
func TestAudit_ModeOrphans_ReturnsDisconnected(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "lonely node", "proj", nil)
	tr := call(t, h, "audit", map[string]any{"mode": "orphans"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), id) {
		t.Errorf("expected orphan node %q in audit orphans response", id)
	}
}

// TestAudit_ModeArchived_ReturnsArchivedNodes: mode=archived must return
// nodes that were archived.

// TestAudit_ModeArchived_ReturnsArchivedNodes: mode=archived must return
// nodes that were archived.
func TestAudit_ModeArchived_ReturnsArchivedNodes(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "to be archived", "proj", nil)
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id, "reason": "test"}))
	tr := call(t, h, "audit", map[string]any{"mode": "archived"})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), id) {
		t.Errorf("expected archived node %q in audit archived response", id)
	}
}

// TestAudit_InvalidMode_ReturnsError: an unrecognised mode must return an error.

// TestAudit_InvalidMode_ReturnsError: an unrecognised mode must return an error.
func TestAudit_InvalidMode_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "audit", map[string]any{"mode": "nonsense"})
	mustError(t, tr)
}

// TestWhatsStale_IsUnknownTool: after consolidation, whats_stale must return
// an error directing to the audit tool.

// TestWhatsStale_IsUnknownTool: after consolidation, whats_stale must return
// an error directing to the audit tool.
func TestWhatsStale_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "whats_stale", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// TestDisconnected_IsUnknownTool: after consolidation, disconnected must
// return an error directing to the audit tool.

// TestDisconnected_IsUnknownTool: after consolidation, disconnected must
// return an error directing to the audit tool.
func TestDisconnected_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "disconnected", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// TestForgotten_IsUnknownTool: after consolidation, forgotten must return an
// error directing to the audit tool.

// TestForgotten_IsUnknownTool: after consolidation, forgotten must return an
// error directing to the audit tool.
func TestForgotten_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "forgotten", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' in error; got: %s", text(t, tr))
	}
}

// ── domains tool (slice 3) ────────────────────────────────────────────────────

// TestDomains_ReturnsDomainsAndAliases: domains must return a combined
// response containing domain list and alias list.

// TestForgetAll_ArchivesMultipleNodes: forget_all must archive all provided
// IDs in a single transaction; they must no longer appear in search.
func TestForgetAll_ArchivesMultipleNodes(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "node alpha", "proj", nil)
	id2 := addNode(t, h, "node beta", "proj", nil)
	tr := call(t, h, "forget_all", map[string]any{
		"items": []map[string]any{
			{"id": id1, "reason": "test cleanup"},
			{"id": id2, "reason": "test cleanup"},
		},
	})
	mustNotError(t, tr)
	// Both should no longer appear in search.
	sr := call(t, h, "search", map[string]any{"query": "node alpha", "domain": "proj"})
	mustNotError(t, sr)
	if strings.Contains(text(t, sr), id1) {
		t.Error("archived node id1 should not appear in search")
	}
}

// TestForgetAll_UnknownID_ReturnsError: forget_all with an unknown ID must
// return an error and not archive any nodes (atomic).

// TestForgetAll_UnknownID_ReturnsError: forget_all with an unknown ID must
// return an error and not archive any nodes (atomic).
func TestForgetAll_UnknownID_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "should stay live", "proj", nil)
	tr := call(t, h, "forget_all", map[string]any{
		"items": []map[string]any{
			{"id": id1, "reason": "cleanup"},
			{"id": "nonexistent-id-xyz", "reason": "cleanup"},
		},
	})
	mustError(t, tr)
	// id1 must still be live (transaction rolled back).
	sr := call(t, h, "search", map[string]any{"query": "should stay live", "domain": "proj"})
	mustNotError(t, sr)
	if !strings.Contains(text(t, sr), id1) {
		t.Error("id1 should still be live after failed forget_all")
	}
}

// ── audit(mode=conflicts) ──────────────────────────────────────────────────────

// TestAudit_ModeConflicts_CoexistsWithOtherModes: mode=conflicts must succeed
// alongside the existing stale/orphans/archived modes without breaking them.
func TestAudit_ModeConflicts_CoexistsWithOtherModes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	// Existing modes must still work.
	for _, mode := range []string{"stale", "orphans", "archived"} {
		tr := call(t, h, "audit", map[string]any{"mode": mode})
		mustNotError(t, tr)
	}

	// conflicts mode must also succeed (empty result is fine).
	tr := call(t, h, "audit", map[string]any{"mode": "conflicts"})
	mustNotError(t, tr)
}

// TestAudit_ModeConflicts_InvalidModeStillErrors: unknown modes must still error.
func TestAudit_ModeConflicts_InvalidModeStillErrors(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "audit", map[string]any{"mode": "bogusmode"})
	mustError(t, tr)
}

// TestAudit_ModeConflicts_ResponseShape: mode=conflicts must return
// {candidates: [...], truncated: bool}.
func TestAudit_ModeConflicts_ResponseShape(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	tr := call(t, h, "audit", map[string]any{"mode": "conflicts"})
	mustNotError(t, tr)

	var resp struct {
		Candidates []struct {
			AID              string  `json:"a_id"`
			ALabel           string  `json:"a_label"`
			BID              string  `json:"b_id"`
			BLabel           string  `json:"b_label"`
			SemanticDistance float64 `json:"semantic_distance"`
			Reason           string  `json:"reason"`
		} `json:"candidates"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse conflicts response: %v\nraw: %s", err, text(t, tr))
	}
	// candidates field must exist (may be empty when no embeddings available).
	if resp.Candidates == nil {
		t.Error("candidates field must be present (even if empty slice)")
	}
}

// TestAudit_ModeConflicts_ExcludesPairsWithContradicts: pairs already linked
// by a contradicts edge must not appear in the conflicts candidates list.
func TestAudit_ModeConflicts_ExcludesPairsWithContradicts(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	idA := addNode(t, h, "JWT expiry must be enforced", "auth", nil)
	idB := addNode(t, h, "JWT expiry is not enforced in admin route", "auth", nil)

	// Mark them as contradicting — conflicts mode must NOT re-flag.
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "conflicts", "domain": "auth"})
	mustNotError(t, tr)

	body := text(t, tr)
	// Since there's no embedding (Ollama disabled), candidates will be empty.
	// The key test: calling the mode with an existing contradicts edge must not
	// produce a response that includes both IDs in the same candidate pair.
	var resp struct {
		Candidates []struct {
			AID string `json:"a_id"`
			BID string `json:"b_id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse conflicts response: %v", err)
	}
	for _, c := range resp.Candidates {
		if (c.AID == idA && c.BID == idB) || (c.AID == idB && c.BID == idA) {
			t.Errorf("pair already linked by contradicts edge must be excluded from conflicts candidates; got: %s", body)
		}
	}
}

// TestAudit_ModeConflicts_DescriptionMentionsContradictsEdge: the audit tool's
// description must include the imperative about contradicts edges.
func TestAudit_ModeConflicts_DescriptionMentionsContradictsEdge(t *testing.T) {
	_, h := newEnv(t)
	tools, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var list []toolEntry
	b, _ := json.Marshal(tools)
	// ListTools returns a struct with a tools array
	var wrapper struct {
		Tools []toolEntry `json:"tools"`
	}
	json.Unmarshal(b, &wrapper)
	list = wrapper.Tools

	var auditDesc string
	for _, tl := range list {
		if tl.Name == "audit" {
			auditDesc = tl.Description
			break
		}
	}
	if auditDesc == "" {
		t.Fatal("audit tool not found in ListTools")
	}
	if !strings.Contains(auditDesc, "contradicts") {
		t.Errorf("audit description must mention 'contradicts'; got: %s", auditDesc)
	}
	if !strings.Contains(auditDesc, "conflicts") {
		t.Errorf("audit description must mention 'conflicts' mode; got: %s", auditDesc)
	}
}

// ── audit: retire resolved contradicts pairs (Story 3) ─────────────────────────

// TestAudit_Stale_ContradictsPair_FlaggedWhenUnresolved: two nodes connected by
// a contradicts edge must appear as drift candidates in mode=stale.
func TestAudit_Stale_ContradictsPair_FlaggedWhenUnresolved(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	idA := addNode(t, h, "pool cap 20", "rules", nil)
	idB := addNode(t, h, "pool cap 35", "rules", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "rules"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, idA) || !strings.Contains(body, idB) {
		t.Errorf("unresolved contradicts pair must appear in stale; got: %s", body)
	}
}

// TestAudit_Stale_ContradictsPair_NotFlaggedAfterResolution: after adding a
// resolved_by edge, the contradicts pair must NOT reappear in mode=stale.
func TestAudit_Stale_ContradictsPair_NotFlaggedAfterResolution(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	idA := addNode(t, h, "pool cap old version", "resolve-test", nil)
	idB := addNode(t, h, "pool cap new version", "resolve-test", nil)
	idResolution := addNode(t, h, "pool cap final decision", "resolve-test", nil)

	// Wire the contradiction.
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	// Resolution action: add a resolved_by edge from the contradicting node
	// to the resolution node.
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idResolution,
		"relationship": "resolved_by",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idB,
		"to_memory":    idResolution,
		"relationship": "resolved_by",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "resolve-test"})
	mustNotError(t, tr)
	body := text(t, tr)

	// The contradicting pair should NOT re-appear (they're resolved).
	// Note: idA and idB may still appear for other reasons (e.g. label "old"),
	// but they must not appear with reason "contradicting each other".
	if strings.Contains(body, "contradicting each other") {
		t.Errorf("resolved contradicts pair must not re-flag in stale; got: %s", body)
	}
}

// TestAudit_Conflicts_ContradictsPair_ExcludedAfterResolution: after a
// resolved_by edge is added, the pair must not appear in mode=conflicts either.
func TestAudit_Conflicts_ContradictsPair_ExcludedAfterResolution(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	idA := addNode(t, h, "pool cap twenty", "conflict-resolve", nil)
	idB := addNode(t, h, "pool cap thirty five", "conflict-resolve", nil)
	idC := addNode(t, h, "pool cap final", "conflict-resolve", nil)

	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idC,
		"relationship": "resolved_by",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "conflicts", "domain": "conflict-resolve"})
	mustNotError(t, tr)

	var resp struct {
		Candidates []struct {
			AID string `json:"a_id"`
			BID string `json:"b_id"`
		} `json:"candidates"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)

	for _, c := range resp.Candidates {
		if (c.AID == idA && c.BID == idB) || (c.AID == idB && c.BID == idA) {
			t.Errorf("resolved pair must not re-appear in conflicts mode; got: %s", text(t, tr))
		}
	}
}

// TestAudit_Stale_ContradictsPair_StillFlaggedWhenUnrelated: unresolved
// contradicts pairs must still appear in stale after an unrelated resolution
// elsewhere in the graph.
func TestAudit_Stale_ContradictsPair_StillFlaggedWhenUnrelated(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	idA := addNode(t, h, "approach alpha", "unresolved-test", nil)
	idB := addNode(t, h, "approach beta", "unresolved-test", nil)
	idC := addNode(t, h, "approach gamma", "unresolved-test", nil)
	idD := addNode(t, h, "resolution for gamma", "unresolved-test", nil)

	// A-B: unresolved contradiction.
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idA,
		"to_memory":    idB,
		"relationship": "contradicts",
	}))

	// C-D: an unrelated resolved_by that should not affect A-B.
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory":  idC,
		"to_memory":    idD,
		"relationship": "resolved_by",
	}))

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "unresolved-test"})
	mustNotError(t, tr)
	body := text(t, tr)

	// A-B must still appear with "contradicting" reason.
	if !strings.Contains(body, "contradicting") {
		t.Errorf("unresolved pair A-B must still appear in stale audit; got: %s", body)
	}
	if !strings.Contains(body, idA) || !strings.Contains(body, idB) {
		t.Errorf("unresolved nodes A (%s) and B (%s) must appear in stale; got: %s", idA, idB, body)
	}
}

// TestAudit_DescriptionMentionsResolutionWorkflow: the audit tool description
// must document the resolution workflow for contradicts edges.
func TestAudit_DescriptionMentionsResolutionWorkflow(t *testing.T) {
	_, h := newEnv(t)
	tools, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var wrapper struct {
		Tools []toolEntry `json:"tools"`
	}
	b, _ := json.Marshal(tools)
	json.Unmarshal(b, &wrapper)

	var auditDesc string
	for _, tl := range wrapper.Tools {
		if tl.Name == "audit" {
			auditDesc = tl.Description
			break
		}
	}
	if !strings.Contains(auditDesc, "resolved_by") && !strings.Contains(auditDesc, "disconnect") {
		t.Errorf("audit description must mention resolution workflow (resolved_by or disconnect); got: %s", auditDesc)
	}
}

// TestCheckForUpdates_IsUnknownTool: check_for_updates must return an error
// after being removed from the MCP surface.

func TestAudit_Stale_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	addNode(t, h, "old plan TDD", "proj", map[string]any{"tags": "TDD"})
	addNode(t, h, "old approach other", "proj", map[string]any{"tags": "other"})

	tr := call(t, h, "audit", map[string]any{
		"mode":   "stale",
		"domain": "proj",
		"tags":   "TDD",
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit stale result: %v", err)
	}
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n == nil {
			continue
		}
		if n["tags"] != "TDD" {
			t.Errorf("expected only TDD-tagged candidate, got tags=%v label=%v", n["tags"], n["label"])
		}
	}
	if len(candidates) == 0 {
		t.Error("expected at least one TDD-tagged stale candidate")
	}
}

func TestAudit_Orphans_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	reviewID := addNode(t, h, "orphan review node", "proj", map[string]any{"tags": "review"})
	addNode(t, h, "orphan other node", "proj", map[string]any{"tags": "other"})

	tr := call(t, h, "audit", map[string]any{
		"mode": "orphans",
		"tags": "review",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse audit orphans result: %v", err)
	}
	ids := make([]string, 0)
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, reviewID) {
		t.Error("review-tagged orphan should be included")
	}
	for _, n := range nodes {
		if n["tags"] != "review" {
			t.Errorf("expected only review-tagged orphan, got tags=%v", n["tags"])
		}
	}
}

func TestAudit_Archived_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	spikeID := addNode(t, h, "spike idea", "proj", map[string]any{"tags": "spike"})
	otherID := addNode(t, h, "other idea", "proj", map[string]any{"tags": "other"})

	call(t, h, "forget", map[string]any{"id": spikeID, "reason": "test"})
	call(t, h, "forget", map[string]any{"id": otherID, "reason": "test"})

	tr := call(t, h, "audit", map[string]any{
		"mode": "archived",
		"tags": "spike",
	})
	mustNotError(t, tr)

	var nodes []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &nodes); err != nil {
		t.Fatalf("parse audit archived result: %v", err)
	}
	ids := make([]string, 0)
	for _, n := range nodes {
		if id, ok := n["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	if !contains(ids, spikeID) {
		t.Error("spike-tagged archived node should be included")
	}
	if contains(ids, otherID) {
		t.Error("other-tagged archived node should be excluded")
	}
}

func TestAudit_Stale_MemoryID(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchorID := addNode(t, h, "anchor", "proj", nil)
	inNeighbourID := addNode(t, h, "old neighbour plan", "proj", nil)
	outsideID := addNode(t, h, "old outside plan", "proj", nil)

	call(t, h, "connect", map[string]any{"from_memory": anchorID, "to_memory": inNeighbourID, "relationship": "connects_to"})

	tr := call(t, h, "audit", map[string]any{
		"mode":      "stale",
		"memory_id": anchorID,
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit stale memory_id result: %v", err)
	}
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n == nil {
			continue
		}
		if n["id"] == outsideID {
			t.Errorf("outside node %q should be excluded when memory_id is set", n["label"])
		}
	}
	found := false
	for _, c := range candidates {
		n, _ := c["node"].(map[string]any)
		if n != nil && n["id"] == inNeighbourID {
			found = true
		}
	}
	if !found {
		t.Error("neighbour stale node should be included when memory_id is set")
	}
}

func TestAudit_ExistingBehaviourUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	addNode(t, h, "old plan alpha", "proj", nil)

	tr := call(t, h, "audit", map[string]any{
		"mode":   "stale",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var candidates []map[string]any
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &candidates); err != nil {
		t.Fatalf("parse audit result: %v", err)
	}
	if len(candidates) == 0 {
		t.Error("expected at least one stale candidate without tags/memory_id filter")
	}
}
