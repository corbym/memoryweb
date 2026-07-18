package tools_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// TestAuditLog_RecordsUpdateNode: every call to update_node must write an
// audit_log entry with action="revise". The reason must name the changed
// fields and their old values.
func TestAuditLog_RecordsUpdateNode(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "original label", "proj", map[string]any{
		"description": "original description",
	})

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"description": "improved description",
		"why_matters": "now it matters more",
	}))

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
			t.Fatalf("scan audit_log: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit_log entry after update_node, got %d", len(entries))
	}
	if entries[0].action != "update" {
		t.Errorf("audit action: got %q, want %q", entries[0].action, "update")
	}
	if !entries[0].reason.Valid || entries[0].reason.String == "" {
		t.Error("audit reason should be non-empty")
	}
	// Reason should mention fields that changed.
	reason := entries[0].reason.String
	if !strings.Contains(reason, "description") {
		t.Errorf("reason should mention 'description'; got %q", reason)
	}
	if !strings.Contains(reason, "why_matters") {
		t.Errorf("reason should mention 'why_matters'; got %q", reason)
	}
	// And should include the old value so the trail is useful.
	if !strings.Contains(reason, "original description") {
		t.Errorf("reason should include old description value; got %q", reason)
	}
}

// ── occurred_at propose+confirm descriptions ──────────────────────────────────

// TestOccurredAt_ToolDescriptions_ContainProposeConfirmGuidance verifies that
// every tool which accepts occurred_at carries the propose+confirm wording in
// its schema description, so agents receive the correct guidance at runtime.

// TestAuditLog_OccurredAt_Revise: when revise sets occurred_at, the audit_log
// update entry must have provenance="agent-assigned".
func TestAuditLog_OccurredAt_Revise(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "some decision", "proj", map[string]any{
		"why_matters": "reason already on file",
	})

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-15",
	}))

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	rows, err := rawDB.Query(
		`SELECT action, provenance FROM audit_log WHERE node_id = ? ORDER BY actioned_at ASC`, id,
	)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer rows.Close()

	type entry struct {
		action     string
		provenance sql.NullString
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.provenance); err != nil {
			t.Fatalf("scan audit_log row: %v", err)
		}
		entries = append(entries, e)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 audit_log entry, got %d", len(entries))
	}
	if entries[0].action != "update" {
		t.Errorf("action: got %q, want %q", entries[0].action, "update")
	}
	if !entries[0].provenance.Valid || entries[0].provenance.String != "agent-assigned" {
		t.Errorf("provenance: got %q, want %q", entries[0].provenance.String, "agent-assigned")
	}
}

// TestAuditLog_NoOccurredAt_ProvenanceIsNull: when revise does NOT set
// occurred_at, the audit_log entry must have a NULL provenance.

// TestRevise_OccurredAt_WhyMattersInDB_Succeeds: revise with occurred_at must
// succeed when why_matters already exists in the DB record (even if omitted
// from the call).
func TestRevise_OccurredAt_WhyMattersInDB_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "existing decision", "proj", map[string]any{
		"why_matters": "already filed when originally created",
	})
	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-10",
		// why_matters intentionally absent from call — should satisfy from DB
	}))
}

// TestRevise_OccurredAt_WhyMattersMissingBoth_Fails: revise with occurred_at
// must fail when why_matters is absent from both the call and the DB record.

// TestRevise_OccurredAt_WhyMattersMissingBoth_Fails: revise with occurred_at
// must fail when why_matters is absent from both the call and the DB record.
func TestRevise_OccurredAt_WhyMattersMissingBoth_Fails(t *testing.T) {
	_, h := newEnv(t)
	// addNode without why_matters — the DB record has an empty why_matters
	tr := call(t, h, "remember", map[string]any{
		"label":  "undocumented node",
		"domain": "proj",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse node: %v", err)
	}
	id := resp.Node.ID

	tr2 := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2024-06-10",
	})
	mustError(t, tr2)
	if !strings.Contains(text(t, tr2), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr2))
	}
}

// TestReviseAll_OccurredAt_WhyMattersMissingBoth_Fails: same constraint applies
// to revise_all when the DB record has no why_matters.

// TestReviseAll_OccurredAt_WhyMattersMissingBoth_Fails: same constraint applies
// to revise_all when the DB record has no why_matters.
func TestReviseAll_OccurredAt_WhyMattersMissingBoth_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "bare node",
		"domain": "proj",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse node: %v", err)
	}
	id := resp.Node.ID

	tr2 := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id, "occurred_at": "2024-07-01"},
		},
	})
	mustError(t, tr2)
	if !strings.Contains(text(t, tr2), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr2))
	}
}

// ── search_nodes multi-word fallback ─────────────────────────────────────────

// TestSearchNodes_MultiWordFallback: multi-word query where no field contains
// the full phrase but each word appears in a different field — should still
// return the node via individual-word OR fallback.

func TestRevise_SetsOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event A", "proj", map[string]any{
		"why_matters": "sets the occurred_at test baseline",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "2026-05-12",
	})
	mustNotError(t, tr)

	// Verify occurred_at is set via history tool.
	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	if !strings.Contains(text(t, hr), "2026-05-12") {
		t.Errorf("expected 2026-05-12 in history output after revise; got: %s", text(t, hr))
	}
}

func TestRevise_InvalidOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event B", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":          id,
		"occurred_at": "not-a-date",
	})
	mustError(t, tr)
}

func TestRevise_OmittingOccurredAt_LeavesItUnchanged(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "event C", "proj", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline event for leave-unchanged test",
	})

	// Revise label only; occurred_at should remain.
	tr := call(t, h, "revise", map[string]any{
		"id":    id,
		"label": "event C revised",
	})
	mustNotError(t, tr)

	// Verify still in important_only history.
	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	if !strings.Contains(text(t, hr), "2026-03-01") {
		t.Errorf("occurred_at should be unchanged after label-only revise; got: %s", text(t, hr))
	}
}

func TestReviseAll_SetsOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "batch A", "proj", map[string]any{
		"why_matters": "batch revise test node A",
	})
	id2 := addNode(t, h, "batch B", "proj", map[string]any{
		"why_matters": "batch revise test node B",
	})

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id1, "occurred_at": "2026-04-01"},
			{"id": id2, "occurred_at": "2026-05-01"},
		},
	})
	mustNotError(t, tr)

	hr := call(t, h, "history", map[string]any{"domain": "proj", "important_only": true})
	mustNotError(t, hr)
	out := text(t, hr)
	if !strings.Contains(out, "2026-04-01") {
		t.Errorf("expected 2026-04-01 in history; got: %s", out)
	}
	if !strings.Contains(out, "2026-05-01") {
		t.Errorf("expected 2026-05-01 in history; got: %s", out)
	}
}

// ── revise: tags ─────────────────────────────────────────────────────────────

// TestRevise_Tags_ReplacesExistingTags: supplying tags to revise must overwrite
// the node's tags, not return the old ones.

// TestRevise_Tags_ReplacesExistingTags: supplying tags to revise must overwrite
// the node's tags, not return the old ones.
func TestRevise_Tags_ReplacesExistingTags(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "tagged node", "proj", map[string]any{
		"tags": "old-tag alpha",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":   id,
		"tags": "new-tag beta",
	})
	mustNotError(t, tr)

	// The returned node must carry the new tags.
	var resp struct {
		Tags string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise response: %v", err)
	}
	if strings.Contains(resp.Tags, "old-tag") {
		t.Errorf("old tags still present after revise; tags = %q", resp.Tags)
	}
	if !strings.Contains(resp.Tags, "new-tag") {
		t.Errorf("new tags not present after revise; tags = %q", resp.Tags)
	}

	// Confirm via recall that the stored node reflects the new tags.
	// recall returns NodeWithEdges: {"node": {...}, "edges": [...]}
	rr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, rr)
	var recallResp struct {
		Node struct {
			Tags string `json:"tags"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, rr)), &recallResp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if strings.Contains(recallResp.Node.Tags, "old-tag") {
		t.Errorf("old tags still stored after revise; recall tags = %q", recallResp.Node.Tags)
	}
	if !strings.Contains(recallResp.Node.Tags, "new-tag") {
		t.Errorf("new tags not stored after revise; recall tags = %q", recallResp.Node.Tags)
	}
}

// TestRevise_Updates_WrapperRejected: passing tags (or any field) inside an
// "updates" wrapper object must return an error, not silently drop the change.
// This guards against the retired revise_all parameter format.

// TestRevise_Updates_WrapperRejected: passing tags (or any field) inside an
// "updates" wrapper object must return an error, not silently drop the change.
// This guards against the retired revise_all parameter format.
func TestRevise_Updates_WrapperRejected(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "wrapper test node", "proj", map[string]any{
		"tags": "original-tag",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":      id,
		"updates": map[string]any{"tags": "should-not-apply"},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "updates") {
		t.Errorf("error should mention 'updates'; got: %s", text(t, tr))
	}
}

// TestReviseBatch_Tags_ReplacesExistingTags: same guarantee in batch (items) mode.

// TestReviseBatch_Tags_ReplacesExistingTags: same guarantee in batch (items) mode.
func TestReviseBatch_Tags_ReplacesExistingTags(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "batch tagged node", "proj", map[string]any{
		"tags": "old-tag alpha",
	})

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id, "tags": "new-tag beta"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Updated []struct {
			Tags string `json:"tags"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch revise response: %v", err)
	}
	if len(resp.Updated) != 1 {
		t.Fatalf("expected 1 updated node, got %d", len(resp.Updated))
	}
	tags := resp.Updated[0].Tags
	if strings.Contains(tags, "old-tag") {
		t.Errorf("old tags still present after batch revise; tags = %q", tags)
	}
	if !strings.Contains(tags, "new-tag") {
		t.Errorf("new tags not present after batch revise; tags = %q", tags)
	}
}

// ── history tool ──────────────────────────────────────────────────────────────

// TestRevise_BatchViaItems_UpdatesMultiple: calling revise with an items array
// must update all entries and return an updated array response.
func TestRevise_BatchViaItems_UpdatesMultiple(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Revise batch A", "batch-revise", nil)
	idB := addNode(t, h, "Revise batch B", "batch-revise", nil)

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": idA, "label": "Revise batch A updated"},
			{"id": idB, "description": "now has a description"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Updated []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise batch response: %v", err)
	}
	if len(resp.Updated) != 2 {
		t.Fatalf("expected 2 updated in response, got %d", len(resp.Updated))
	}
	ids := map[string]bool{}
	for _, n := range resp.Updated {
		ids[n.ID] = true
	}
	if !ids[idA] || !ids[idB] {
		t.Errorf("unexpected IDs in revise batch response: %v", ids)
	}
}

// TestReviseAll_IsUnknownTool: after consolidation, revise_all must no
// longer be a registered tool.

// TestReviseAll_IsUnknownTool: after consolidation, revise_all must no
// longer be a registered tool.
func TestReviseAll_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Revise all target", "test", nil)
	tr := call(t, h, "revise_all", map[string]any{
		"updates": []map[string]any{
			{"id": id, "label": "Updated"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %s", text(t, tr))
	}
}

// TestConnect_BatchViaItems_CreatesMultipleEdges: calling connect with an
// items array must create all edges and return edges_created count.

// TestRevise_TransientUpdatable covers all transient field scenarios via revise:
// clearing the flag, setting it, leaving it unchanged, batch mode, and edge preservation.
func TestRevise_TransientUpdatable(t *testing.T) {
	t.Run("clear transient (transient to decision)", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Transient node", "transient-test", map[string]any{"node_kind": "transient"})

		tr := call(t, h, "revise", map[string]any{"id": id, "node_kind": "decision"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if strings.Contains(text(t, got), `"node_kind": "transient"`) {
			t.Error("expected node_kind to be cleared to 'decision'")
		}
	})

	t.Run("set transient (decision to transient)", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Permanent node", "transient-test", nil)

		tr := call(t, h, "revise", map[string]any{"id": id, "node_kind": "transient"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), `"node_kind": "transient"`) {
			t.Error("expected node_kind to be set to 'transient'")
		}
	})

	t.Run("omit node_kind - unchanged", func(t *testing.T) {
		_, h := newEnv(t)
		id := addNode(t, h, "Transient node", "transient-test", map[string]any{"node_kind": "transient"})

		tr := call(t, h, "revise", map[string]any{"id": id, "label": "Updated label"})
		mustNotError(t, tr)

		got := call(t, h, "recall", map[string]any{"id": id})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), `"node_kind": "transient"`) {
			t.Error("expected node_kind to remain 'transient' when omitted from revise")
		}
	})

	t.Run("batch mode sets node_kind", func(t *testing.T) {
		_, h := newEnv(t)
		id1 := addNode(t, h, "Batch node A", "transient-test", nil)
		id2 := addNode(t, h, "Batch node B", "transient-test", map[string]any{"node_kind": "transient"})

		items := []map[string]any{
			{"id": id1, "node_kind": "transient"},
			{"id": id2, "node_kind": "decision"},
		}
		tr := call(t, h, "revise", map[string]any{"items": items})
		mustNotError(t, tr)

		got1 := call(t, h, "recall", map[string]any{"id": id1})
		mustNotError(t, got1)
		if !strings.Contains(text(t, got1), `"node_kind": "transient"`) {
			t.Error("batch: expected id1 node_kind='transient'")
		}

		got2 := call(t, h, "recall", map[string]any{"id": id2})
		mustNotError(t, got2)
		if strings.Contains(text(t, got2), `"node_kind": "transient"`) {
			t.Error("batch: expected id2 node_kind to be cleared to 'decision'")
		}
	})

	t.Run("preserves edges when node_kind changes", func(t *testing.T) {
		_, h := newEnv(t)
		id1 := addNode(t, h, "Node with edge", "transient-test", nil)
		id2 := addNode(t, h, "Connected node", "transient-test", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": id1, "to_memory": id2, "relationship": "connects_to", "narrative": "test edge",
		}))

		mustNotError(t, call(t, h, "revise", map[string]any{"id": id1, "node_kind": "transient"}))

		got := call(t, h, "recall", map[string]any{"id": id1})
		mustNotError(t, got)
		if !strings.Contains(text(t, got), id2) {
			t.Error("expected edge to id2 to be preserved after transient change")
		}
	})
}

// TestRevise_TransientInSchema verifies the revise tool schema exposes transient as
// a boolean property in both single mode and the items array.

func TestRevise_NodeKind(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "promote to standing", "proj", map[string]any{
		"node_kind":   "decision",
		"why_matters": "will be promoted",
	})
	tr := call(t, h, "revise", map[string]any{
		"id":        id,
		"node_kind": "assumption",
	})
	mustNotError(t, tr)

	tr2 := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr2)
	var resp struct {
		Node struct {
			NodeKind string `json:"node_kind"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr2)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.NodeKind != "assumption" {
		t.Errorf("node_kind after revise: got %q, want %q", resp.Node.NodeKind, "assumption")
	}
}

func TestRevise_DecisionType_Rejected(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "old key revise target", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":            id,
		"decision_type": "standing",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "node_kind") {
		t.Errorf("expected rejection message to mention node_kind, got: %s", text(t, tr))
	}
}

func TestUpdateNode_NodeKind_InvalidValue(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "any node", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":        id,
		"node_kind": "nonsense",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "transient") || !strings.Contains(text(t, tr), "goal") {
		t.Errorf("expected error to list valid kinds, got: %s", text(t, tr))
	}
}

func TestRevise_Domain_MovesNode(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "mis-filed node", "wrong-domain", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "right-domain",
		"reason": "was filed in wrong domain",
	})
	mustNotError(t, tr)

	var resp struct {
		Domain string `json:"domain"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise response: %v", err)
	}
	if resp.Domain != "right-domain" {
		t.Errorf("domain: got %q, want %q", resp.Domain, "right-domain")
	}
	if resp.ID != id {
		t.Error("ID must not change on domain move")
	}
}

func TestRevise_Domain_RequiresReason(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "some node", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "other-proj",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "reason") {
		t.Errorf("error should mention reason, got: %s", text(t, tr))
	}
}

func TestRevise_Domain_EmptyReasonRejected(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "some node", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "other-proj",
		"reason": "   ",
	})
	mustError(t, tr)
}

func TestRevise_Domain_PreservesEdges(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	fromID := addNode(t, h, "source node", "proj", nil)
	toID := addNode(t, h, "target node", "proj", nil)
	call(t, h, "connect", map[string]any{
		"from_memory":  fromID,
		"to_memory":    toID,
		"relationship": "connects_to",
	})

	tr := call(t, h, "revise", map[string]any{
		"id":     fromID,
		"domain": "new-proj",
		"reason": "consolidating",
	})
	mustNotError(t, tr)

	// Edge should still exist — recall the moved node and check edges.
	recallTr := call(t, h, "recall", map[string]any{"id": fromID})
	mustNotError(t, recallTr)
	if !strings.Contains(text(t, recallTr), toID) {
		t.Error("edge to toID should survive domain move")
	}
}

func TestRevise_Domain_ToNewDomain(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "first node in new domain", "existing-domain", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "brand-new-domain",
		"reason": "creating new domain",
	})
	mustNotError(t, tr)
	var resp struct {
		Domain string `json:"domain"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.Domain != "brand-new-domain" {
		t.Errorf("got domain %q, want brand-new-domain", resp.Domain)
	}
}

func TestRevise_Batch_Domain_RequiresReason(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "batch no reason", "proj", nil)

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id, "domain": "other"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "reason") {
		t.Errorf("batch error should mention reason, got: %s", text(t, tr))
	}
}

func TestRevise_Batch_Domain_MovesAll(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id1 := addNode(t, h, "batch move a", "src", nil)
	id2 := addNode(t, h, "batch move b", "src", nil)

	tr := call(t, h, "revise", map[string]any{
		"items": []map[string]any{
			{"id": id1, "domain": "dst", "reason": "consolidating"},
			{"id": id2, "domain": "dst", "reason": "consolidating"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Updated []struct {
			Domain string `json:"domain"`
		} `json:"updated"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch response: %v", err)
	}
	for i, u := range resp.Updated {
		if u.Domain != "dst" {
			t.Errorf("item %d: domain = %q, want dst", i, u.Domain)
		}
	}
}

func TestRevise_Domain_ViaAliasResolvesOnWrite(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "target", "domain": "canonical-target"})
	id := addNode(t, h, "Move via alias", "source-domain", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "target",
		"reason": "consolidating under canonical name",
	})
	mustNotError(t, tr)

	var resp struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise response: %v", err)
	}
	if resp.Domain != "canonical-target" {
		t.Errorf("domain: got %q, want canonical-target", resp.Domain)
	}
}

func TestRevise_Domain_AliasMatchingCurrent_NoReasonRequired(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	call(t, h, "alias", map[string]any{"action": "add", "alias": "engine", "domain": "deep-engine"})
	id := addNode(t, h, "Already canonical", "deep-engine", nil)

	tr := call(t, h, "revise", map[string]any{
		"id":     id,
		"domain": "engine",
	})
	mustNotError(t, tr)

	var resp struct {
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse revise response: %v", err)
	}
	if resp.Domain != "deep-engine" {
		t.Errorf("domain: got %q, want deep-engine", resp.Domain)
	}
}
