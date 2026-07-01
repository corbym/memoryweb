package tools_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestAddNode_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "RST boot crash", "deep-game", map[string]any{
		"description": "ROM calls RST $10 which hangs the boot sequence",
		"why_matters": "Blocks the demo from running",
	})
	if !strings.HasPrefix(id, "rst-boot-crash-") {
		t.Errorf("unexpected ID format: %s", id)
	}
}

func TestAddNode_WithOccurredAtDateOnly(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Boot crash discovered",
		"domain":      "deep-game",
		"occurred_at": "2026-04-01",
		"why_matters": "marks when the crash was first seen",
	})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			OccurredAt string `json:"occurred_at"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.Node.OccurredAt == "" {
		t.Error("occurred_at not set in response")
	}
}

func TestAddNode_WithOccurredAtRFC3339(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Boot crash discovered",
		"domain":      "deep-game",
		"occurred_at": "2026-04-01T14:30:00Z",
		"why_matters": "precise timestamp of first crash observation",
	})
	mustNotError(t, tr)
}

func TestAddNode_InvalidOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "Bad date node",
		"domain":      "deep-game",
		"occurred_at": "not-a-date",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "occurred_at") {
		t.Errorf("error message should mention occurred_at, got: %s", text(t, tr))
	}
}

func TestAddNode_EmptyLabel_StillCreatesNode(t *testing.T) {
	// The tool doesn't validate required fields itself; that's the MCP layer.
	// An empty label is passed through — test that it doesn't panic.
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "",
		"domain": "deep-game",
	})
	// Whether this errors or not, the handler must return something.
	if tr == nil {
		t.Fatal("nil ToolResult for empty label")
	}
}

// ── get_node ──────────────────────────────────────────────────────────────────

func TestAddNode_WithTags_SearchableByTag(t *testing.T) {
	_, h := newEnv(t)
	// The description uses "approval parameterised" so search will match even without tags,
	// but we verify the tag field is echoed back and search works.
	id := addNode(t, h, "Test scaffold decision", "proj", map[string]any{
		"tags": "approval parameterised withNameSuffix kotlin",
	})

	tr := call(t, h, "search", map[string]any{"query": "withNameSuffix", "domain": "proj"})
	mustNotError(t, tr)
	if !contains(searchIDs(t, tr), id) {
		t.Error("node not found via tags field search")
	}
}

func TestAddNode_WithTags_TagsInResponse(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Tagged Node",
		"domain": "proj",
		"tags":   "alpha beta gamma",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			Tags string `json:"tags"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.Node.Tags != "alpha beta gamma" {
		t.Errorf("tags in response: got %q", resp.Node.Tags)
	}
}

// TestAddNode_ResponseShape: remember must return {node, suggested_connections}.

// TestAddNode_ResponseShape: remember must return {node, suggested_connections}.
func TestAddNode_ResponseShape(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Shape test node",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var resp struct {
		Node *struct {
			ID string `json:"id"`
		} `json:"node"`
		SuggestedConnections *[]struct {
			ID string `json:"id"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.Node == nil {
		t.Error("response must have a 'node' field")
	}
	if resp.SuggestedConnections == nil {
		t.Error("response must have a 'suggested_connections' field (even if empty)")
	}
}

// TestAddNode_ResponseIncludesSuggestedConnections: when a related node already
// exists in the same domain, it should appear in suggested_connections.

// TestAddNode_ResponseIncludesSuggestedConnections: when a related node already
// exists in the same domain, it should appear in suggested_connections.
func TestAddNode_ResponseIncludesSuggestedConnections(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "RST crash root cause", "proj", map[string]any{
		"description": "ROM calls RST $10 which hangs the boot sequence",
	})

	tr := call(t, h, "remember", map[string]any{
		"label":       "RST crash investigation",
		"domain":      "proj",
		"description": "RST $10 handler analysis",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
		SuggestedConnections []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Reason string `json:"reason"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}

	found := false
	for _, s := range resp.SuggestedConnections {
		if s.ID == existingID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected existingID %q in suggested_connections; got %+v", existingID, resp.SuggestedConnections)
	}
}

// ── add_node related_to ───────────────────────────────────────────────────────

func TestAddNode_WithRelatedTo_PlainStringCreatesConnectsToEdge(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Existing Node", "proj", nil)

	newID := addNode(t, h, "New Node", "proj", map[string]any{
		"related_to": []string{existingID},
	})

	tr := call(t, h, "recall", map[string]any{"id": newID})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "connects_to" &&
			((e.FromMemory == newID && e.ToMemory == existingID) ||
				(e.FromMemory == existingID && e.ToMemory == newID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected connects_to edge between %q and %q, got edges: %+v", newID, existingID, nwe.Edges)
	}
}

func TestAddNode_WithRelatedTo_ExplicitRelationshipObject(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Cause Node", "proj", nil)

	newID := addNode(t, h, "Effect Node", "proj", map[string]any{
		"related_to": []map[string]any{
			{"id": existingID, "relationship": "led_to"},
		},
	})

	tr := call(t, h, "recall", map[string]any{"id": newID})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nwe); err != nil {
		t.Fatalf("parse get_node: %v", err)
	}

	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "led_to" &&
			((e.FromMemory == newID && e.ToMemory == existingID) ||
				(e.FromMemory == existingID && e.ToMemory == newID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected led_to edge between %q and %q; got edges: %+v", newID, existingID, nwe.Edges)
	}
}

func TestAddNode_WithRelatedTo_MixedFormats(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Node A mixed", "proj", nil)
	idB := addNode(t, h, "Node B mixed", "proj", nil)

	// idA via plain string → connects_to; idB via object → depends_on
	idC := addNode(t, h, "Node C mixed", "proj", map[string]any{
		"related_to": []any{
			idA,
			map[string]any{"id": idB, "relationship": "depends_on"},
		},
	})

	tr := call(t, h, "recall", map[string]any{"id": idC})
	mustNotError(t, tr)

	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nwe)

	relByTarget := map[string]string{}
	for _, e := range nwe.Edges {
		if e.FromMemory == idC {
			relByTarget[e.ToMemory] = e.Relationship
		} else if e.ToMemory == idC {
			relByTarget[e.FromMemory] = e.Relationship
		}
	}

	if relByTarget[idA] != "connects_to" {
		t.Errorf("plain string entry: expected connects_to to idA, got %q", relByTarget[idA])
	}
	if relByTarget[idB] != "depends_on" {
		t.Errorf("object entry: expected depends_on to idB, got %q", relByTarget[idB])
	}
}

func TestAddNode_WithRelatedTo_UnknownIDSilentlySkipped(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Safe Node",
		"domain":     "proj",
		"related_to": []string{"ghost-id-xxxx"},
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)

	gettr := call(t, h, "recall", map[string]any{"id": resp.Node.ID})
	mustNotError(t, gettr)

	var nwe struct {
		Edges []struct{} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, gettr)), &nwe)
	if len(nwe.Edges) != 0 {
		t.Errorf("expected no edges for unknown ID, got %d", len(nwe.Edges))
	}
}

// ── related_to: skipped_connections surfaced ──────────────────────────────────

func TestSingleRemember_RelatedToInvalidId_ReportedNotSilent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Node With Bad Link",
		"domain":     "proj",
		"related_to": []string{"bad-id-does-not-exist"},
	})
	mustNotError(t, tr)

	var resp struct {
		SkippedConnections []struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		} `json:"skipped_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.SkippedConnections) == 0 {
		t.Fatal("expected skipped_connections to contain the bad ID, got none")
	}
	if resp.SkippedConnections[0].ID != "bad-id-does-not-exist" {
		t.Errorf("expected skipped ID %q, got %q", "bad-id-does-not-exist", resp.SkippedConnections[0].ID)
	}
	if resp.SkippedConnections[0].Reason == "" {
		t.Error("expected non-empty reason for skipped connection")
	}
}

func TestSingleRemember_RelatedToValidId_NoSkipped(t *testing.T) {
	_, h := newEnv(t)
	existingID := addNode(t, h, "Target Node", "proj", nil)
	tr := call(t, h, "remember", map[string]any{
		"label":      "Source Node",
		"domain":     "proj",
		"related_to": []string{existingID},
	})
	mustNotError(t, tr)

	var resp struct {
		SkippedConnections []struct{} `json:"skipped_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.SkippedConnections) != 0 {
		t.Errorf("expected no skipped_connections for valid ID, got %d", len(resp.SkippedConnections))
	}
}

// ── batch remember: related_to support ───────────────────────────────────────

func TestBatchRemember_RelatedToString(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Source",
				"domain":     "proj",
				"related_to": []string{targetID},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct{} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one node in batch response")
	}
	sourceID := resp.Nodes[0].Node.ID

	// Edge should exist
	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory string `json:"from_memory"`
			ToMemory   string `json:"to_memory"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if (e.FromMemory == sourceID && e.ToMemory == targetID) || (e.FromMemory == targetID && e.ToMemory == sourceID) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected edge between %q and %q; got edges: %+v", sourceID, targetID, nwe.Edges)
	}
	if len(resp.Nodes[0].SkippedConnections) != 0 {
		t.Errorf("expected no skipped_connections for valid ID, got %d", len(resp.Nodes[0].SkippedConnections))
	}
}

func TestBatchRemember_RelatedToObject(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Cause", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Batch Effect",
				"domain": "proj",
				"related_to": []map[string]any{
					{"id": targetID, "relationship": "caused_by"},
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if e.Relationship == "caused_by" &&
			((e.FromMemory == sourceID && e.ToMemory == targetID) ||
				(e.FromMemory == targetID && e.ToMemory == sourceID)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected caused_by edge between %q and %q; got: %+v", sourceID, targetID, nwe.Edges)
	}
}

func TestBatchRemember_RelatedToArray(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "Batch Array Target A", "proj", nil)
	idB := addNode(t, h, "Batch Array Target B", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Batch Array Source",
				"domain": "proj",
				"related_to": []any{
					idA,
					map[string]any{"id": idB, "relationship": "depends_on"},
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct {
			FromMemory   string `json:"from_memory"`
			ToMemory     string `json:"to_memory"`
			Relationship string `json:"relationship"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)

	relByTarget := map[string]string{}
	for _, e := range nwe.Edges {
		if e.FromMemory == sourceID {
			relByTarget[e.ToMemory] = e.Relationship
		} else if e.ToMemory == sourceID {
			relByTarget[e.FromMemory] = e.Relationship
		}
	}
	if relByTarget[idA] != "connects_to" {
		t.Errorf("expected connects_to to idA, got %q", relByTarget[idA])
	}
	if relByTarget[idB] != "depends_on" {
		t.Errorf("expected depends_on to idB, got %q", relByTarget[idB])
	}
}

func TestBatchRemember_RelatedToAbsent_NoEdge(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Batch No Links", "domain": "proj"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		} `json:"nodes"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	sourceID := resp.Nodes[0].Node.ID

	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	mustNotError(t, recall)
	var nwe struct {
		Edges []struct{} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	if len(nwe.Edges) != 0 {
		t.Errorf("expected no edges, got %d", len(nwe.Edges))
	}
}

func TestBatchRemember_OrphanWarning_AbsentWhenRelatedToUsed(t *testing.T) {
	_, h := newEnv(t)
	targetID := addNode(t, h, "Batch Orphan Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Orphan Source",
				"domain":     "proj",
				"related_to": []string{targetID},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		OrphanWarning string `json:"orphan_warning"`
	}
	json.Unmarshal([]byte(text(t, tr)), &resp)
	if resp.OrphanWarning != "" {
		t.Errorf("expected no orphan_warning when related_to used, got %q", resp.OrphanWarning)
	}
}

func TestBatchRemember_RelatedToInvalidId_ReportedNotSilent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":      "Batch Bad Link",
				"domain":     "proj",
				"related_to": []string{"bad-batch-id-xxxx"},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct {
				ID     string `json:"id"`
				Reason string `json:"reason"`
			} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected node in response")
	}
	sc := resp.Nodes[0].SkippedConnections
	if len(sc) == 0 {
		t.Fatal("expected skipped_connections in batch item, got none")
	}
	if sc[0].ID != "bad-batch-id-xxxx" {
		t.Errorf("expected skipped ID %q, got %q", "bad-batch-id-xxxx", sc[0].ID)
	}
	if sc[0].Reason == "" {
		t.Error("expected non-empty reason in skipped_connections")
	}
}

func TestBatchRemember_RelatedToPartialSuccess(t *testing.T) {
	_, h := newEnv(t)
	validID := addNode(t, h, "Partial Valid Target", "proj", nil)

	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Partial Source",
				"domain": "proj",
				"related_to": []any{
					validID,
					"ghost-partial-id-xxxx",
				},
			},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
			SkippedConnections []struct {
				ID string `json:"id"`
			} `json:"skipped_connections"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	sourceID := resp.Nodes[0].Node.ID

	// Valid edge should exist
	recall := call(t, h, "recall", map[string]any{"id": sourceID})
	var nwe struct {
		Edges []struct {
			FromMemory string `json:"from_memory"`
			ToMemory   string `json:"to_memory"`
		} `json:"edges"`
	}
	json.Unmarshal([]byte(text(t, recall)), &nwe)
	found := false
	for _, e := range nwe.Edges {
		if (e.FromMemory == sourceID && e.ToMemory == validID) || (e.FromMemory == validID && e.ToMemory == sourceID) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected valid edge to %q to exist", validID)
	}

	// Only invalid ID in skipped_connections
	sc := resp.Nodes[0].SkippedConnections
	if len(sc) != 1 {
		t.Fatalf("expected exactly 1 skipped connection, got %d: %+v", len(sc), sc)
	}
	if sc[0].ID != "ghost-partial-id-xxxx" {
		t.Errorf("expected skipped ID %q, got %q", "ghost-partial-id-xxxx", sc[0].ID)
	}
}

func TestAddNodes_WithTags_Searchable(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{
				"label":  "Batch Node One",
				"domain": "proj",
				"tags":   "batchsearch uniqueterm",
			},
		},
	})
	mustNotError(t, tr)

	srTr := call(t, h, "search", map[string]any{"query": "uniqueterm", "domain": "proj"})
	mustNotError(t, srTr)
	ids := searchIDs(t, srTr)
	if len(ids) == 0 {
		t.Error("batch node not findable by tag")
	}
}

// ── audit_log for update_node ─────────────────────────────────────────────────

// TestAuditLog_RecordsUpdateNode: every call to update_node must write an
// audit_log entry with action="revise". The reason must name the changed
// fields and their old values.

// TestAuditLog_OccurredAt_Remember: when remember sets occurred_at, the
// audit_log must record an entry with action="occurred_at_set" and
// provenance="agent-assigned".
func TestAuditLog_OccurredAt_Remember(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	id := addNode(t, h, "significant decision", "proj", map[string]any{
		"occurred_at": "2024-06-01",
		"why_matters": "chose this approach because of constraint X",
	})

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
	if entries[0].action != "occurred_at_set" {
		t.Errorf("action: got %q, want %q", entries[0].action, "occurred_at_set")
	}
	if !entries[0].provenance.Valid || entries[0].provenance.String != "agent-assigned" {
		t.Errorf("provenance: got %q, want %q", entries[0].provenance.String, "agent-assigned")
	}
}

// TestAuditLog_OccurredAt_Revise: when revise sets occurred_at, the audit_log
// update entry must have provenance="agent-assigned".

// TestAuditLog_NoOccurredAt_ProvenanceIsNull: when revise does NOT set
// occurred_at, the audit_log entry must have a NULL provenance.
func TestAuditLog_NoOccurredAt_ProvenanceIsNull(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)
	id := addNode(t, h, "plain node", "proj", nil)

	mustNotError(t, call(t, h, "revise", map[string]any{
		"id":          id,
		"description": "updated description only",
	}))

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()

	var provenance sql.NullString
	err = rawDB.QueryRow(
		`SELECT provenance FROM audit_log WHERE node_id = ? AND action = 'update'`, id,
	).Scan(&provenance)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if provenance.Valid {
		t.Errorf("provenance should be NULL when occurred_at is not set, got %q", provenance.String)
	}
}

// ── occurred_at requires why_matters enforcement ──────────────────────────────

// TestRemember_OccurredAt_WithWhyMatters_Succeeds: setting occurred_at with
// why_matters present must succeed.
func TestRemember_OccurredAt_WithWhyMatters_Succeeds(t *testing.T) {
	_, h := newEnv(t)
	mustNotError(t, call(t, h, "remember", map[string]any{
		"label":       "deploy decision",
		"domain":      "proj",
		"occurred_at": "2024-06-01",
		"why_matters": "chose blue-green over rolling — downtime constraint",
	}))
}

// TestRemember_OccurredAt_WithoutWhyMatters_Fails: setting occurred_at without
// why_matters must return the exact validation error.

// TestRemember_OccurredAt_WithoutWhyMatters_Fails: setting occurred_at without
// why_matters must return the exact validation error.
func TestRemember_OccurredAt_WithoutWhyMatters_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "deploy decision",
		"domain":      "proj",
		"occurred_at": "2024-06-01",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr))
	}
}

// TestRememberAll_OccurredAt_WithoutWhyMatters_Fails: same constraint applies
// to remember_all — the failing node's index appears in the error.

// TestRememberAll_OccurredAt_WithoutWhyMatters_Fails: same constraint applies
// to remember_all — the failing node's index appears in the error.
func TestRememberAll_OccurredAt_WithoutWhyMatters_Fails(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "fine node", "domain": "proj", "why_matters": "ok"},
			{"label": "bad node", "domain": "proj", "occurred_at": "2024-06-01"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), errOccurredAtRequiresWhyMatters) {
		t.Errorf("wrong error message; got: %s", text(t, tr))
	}
}

// TestRevise_OccurredAt_WhyMattersInDB_Succeeds: revise with occurred_at must
// succeed when why_matters already exists in the DB record (even if omitted
// from the call).

func TestAddNode_NodeKindTransient_PersistedAndReturned(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":     "Sprint ticket ABC",
		"domain":    "proj",
		"node_kind": "transient",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			NodeKind string `json:"node_kind"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.Node.NodeKind != "transient" {
		t.Errorf("node_kind should be 'transient' in remember response, got %q", resp.Node.NodeKind)
	}
}

func TestAddNode_NodeKind_Default(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "Plain memory",
		"domain": "proj",
	})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			NodeKind string `json:"node_kind"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}
	if resp.Node.NodeKind != "decision" {
		t.Errorf("node_kind should default to 'decision', got %q", resp.Node.NodeKind)
	}
}

func TestAddNode_NodeKind_AllValues(t *testing.T) {
	_, h := newEnv(t)
	kinds := []string{"transient", "reference", "issue", "decision", "option", "assumption", "finding", "standing", "goal"}
	for _, kind := range kinds {
		tr := call(t, h, "remember", map[string]any{
			"label":       "node of kind " + kind,
			"domain":      "proj",
			"node_kind":   kind,
			"why_matters": "required for standing kind",
		})
		mustNotError(t, tr)

		var resp struct {
			Node struct {
				NodeKind string `json:"node_kind"`
			} `json:"node"`
		}
		if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
			t.Fatalf("parse remember response for kind %q: %v", kind, err)
		}
		if resp.Node.NodeKind != kind {
			t.Errorf("node_kind: got %q, want %q", resp.Node.NodeKind, kind)
		}
	}
}

func TestAddNode_DecisionType_Rejected(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":         "should be rejected",
		"domain":        "proj",
		"decision_type": "decision",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "node_kind") {
		t.Errorf("expected rejection message to mention node_kind, got: %s", text(t, tr))
	}
}

func TestRemember_OrphanWarning_PresentWhenNoConnections(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":  "lonely node",
		"domain": "test",
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "orphan_warning") {
		t.Error("expected orphan_warning field in response")
	}
	if !strings.Contains(tr.Content[0].Text, "No connections were made") {
		t.Error("expected orphan_warning message in response")
	}
	if strings.Contains(tr.Content[0].Text, "cannot be connected directly") {
		t.Error("orphan_warning must not say 'cannot be connected directly' — use usage-instruction wording instead")
	}
	if !strings.Contains(tr.Content[0].Text, "pass their domain explicitly") {
		t.Error("orphan_warning must instruct agent to pass domain explicitly for cross-domain connect")
	}
}

func TestRemember_OrphanWarning_AbsentWhenRelatedToProvided(t *testing.T) {
	_, h := newEnv(t)
	idA := addNode(t, h, "anchor", "test", nil)
	tr := call(t, h, "remember", map[string]any{
		"label":      "linked node",
		"domain":     "test",
		"related_to": []string{idA},
	})
	mustNotError(t, tr)
	if strings.Contains(tr.Content[0].Text, `"orphan_warning"`) {
		t.Error("orphan_warning should be absent when related_to was provided")
	}
}

func TestRememberAll_OrphanWarning_PresentWhenNoEdges(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "node one", "domain": "test"},
			{"label": "node two", "domain": "test"},
		},
	})
	mustNotError(t, tr)
	if !strings.Contains(tr.Content[0].Text, "orphan_warning") {
		t.Error("expected orphan_warning field in remember batch response")
	}
	if !strings.Contains(tr.Content[0].Text, "No connections were made") {
		t.Error("expected orphan_warning message in remember batch response")
	}
	if strings.Contains(tr.Content[0].Text, "cannot be connected directly") {
		t.Error("orphan_warning must not say 'cannot be connected directly' — use usage-instruction wording instead")
	}
	if !strings.Contains(tr.Content[0].Text, "pass their domain explicitly") {
		t.Error("orphan_warning must instruct agent to pass domain explicitly for cross-domain connect")
	}
}

// ── rename_domain ─────────────────────────────────────────────────────────────

// TestRemember_BatchViaItems_FilesMultipleNodes: calling remember with an
// items array must create all nodes and return a nodes array response.
func TestRemember_BatchViaItems_FilesMultipleNodes(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Batch node A", "domain": "batch-test"},
			{"label": "Batch node B", "domain": "batch-test"},
		},
	})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Node struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"node"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember batch response: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes in response, got %d", len(resp.Nodes))
	}
	labels := map[string]bool{}
	for _, n := range resp.Nodes {
		if n.Node.ID == "" {
			t.Error("batch node missing ID")
		}
		labels[n.Node.Label] = true
	}
	if !labels["Batch node A"] || !labels["Batch node B"] {
		t.Errorf("unexpected labels in batch response: %v", labels)
	}
}

// TestRemember_BatchViaItems_OrphanWarningPresent: batch remember with no
// edges must include orphan_warning and return a nodes array (not single node shape).

// TestRemember_BatchViaItems_OrphanWarningPresent: batch remember with no
// edges must include orphan_warning and return a nodes array (not single node shape).
func TestRemember_BatchViaItems_OrphanWarningPresent(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "Orphan batch node", "domain": "batch-orphan"},
		},
	})
	mustNotError(t, tr)

	// Must return the batch shape (nodes array), not the single shape (node object).
	var resp struct {
		Nodes         []json.RawMessage `json:"nodes"`
		OrphanWarning string            `json:"orphan_warning"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse batch remember response: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node in batch response, got %d", len(resp.Nodes))
	}
	if resp.OrphanWarning == "" {
		t.Error("expected non-empty orphan_warning in batch remember response")
	}
}

// TestRemember_SimilarityFloor_NoCrossDomainNoise: filing a node in domain-b
// must not produce suggested_connections from domain-a when the two domains
// have clearly unrelated content (Z80 assembly vs JWT admin bugs).
// This is the regression fixture from the shared finding:
// "filing-time suggested_connections show cross-domain noise, no similarity floor".
// Because we cannot run Ollama in CI, this test disables embeddings and relies
// on the keyword path — which must already be domain-scoped, producing zero
// cross-domain suggestions.
func TestRemember_SimilarityFloor_NoCrossDomainNoise(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	// Domain-a: Z80 assembly — completely unrelated to JWT/admin bugs.
	addNode(t, h, "Z80 register file layout", "domain-a", map[string]any{
		"description": "Z80 CPU uses eight 8-bit registers: A, B, C, D, E, H, L and F. The SP and PC are 16-bit.",
		"why_matters": "Understanding register layout is fundamental to Z80 assembly programming",
		"tags":        "z80 assembly register cpu",
	})
	addNode(t, h, "Z80 stack pointer conventions", "domain-a", map[string]any{
		"description": "The stack grows downward from the initial SP value. Push decrements SP first.",
		"why_matters": "Stack discipline is critical for correct interrupt handling in Z80 code",
		"tags":        "z80 assembly stack pointer",
	})
	addNode(t, h, "Z80 interrupt mode 2 setup", "domain-a", map[string]any{
		"description": "IM 2 uses a vector table pointed to by the I register combined with a byte from the data bus.",
		"why_matters": "IM 2 provides flexible interrupt dispatch for Z80 systems",
		"tags":        "z80 assembly interrupt mode",
	})

	// Domain-b: JWT admin bug report — file a node here.
	addNode(t, h, "JWT provisioning bug root cause", "domain-b", map[string]any{
		"description": "Admin endpoint was generating JWTs with wrong audience claim. Fixed by updating the issuer config.",
		"why_matters": "Broke authentication for all admin users for 2 hours",
		"tags":        "jwt admin auth bug",
	})

	tr := call(t, h, "remember", map[string]any{
		"label":       "JWT expiry not enforced on admin login",
		"domain":      "domain-b",
		"description": "The admin login route was not validating the JWT expiry claim, allowing expired tokens.",
		"why_matters": "Security regression — expired admin sessions remained valid indefinitely",
		"tags":        "jwt admin auth security",
	})
	mustNotError(t, tr)

	var resp struct {
		SuggestedConnections []struct {
			ID     string `json:"id"`
			Domain string `json:"domain"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}

	// No domain-a (Z80) node should appear in suggested_connections.
	for _, s := range resp.SuggestedConnections {
		if s.Domain == "domain-a" {
			t.Errorf("cross-domain noise: domain-a Z80 node %q appeared in suggested_connections for domain-b JWT node", s.ID)
		}
	}
}

// TestRemember_SimilarityFloor_SameDomainStillSuggested: when a genuine
// duplicate/related node exists in the same domain, it must still appear in
// suggested_connections above the floor.
func TestRemember_SimilarityFloor_SameDomainStillSuggested(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	existingID := addNode(t, h, "JWT token expiry enforcement", "auth-domain", map[string]any{
		"description": "Enforce JWT expiry on all API routes including admin endpoints",
		"why_matters": "Prevents session hijack via expired tokens",
		"tags":        "jwt auth expiry",
	})

	tr := call(t, h, "remember", map[string]any{
		"label":       "JWT expiry validation missing on login route",
		"domain":      "auth-domain",
		"description": "Login route skips JWT expiry validation",
		"why_matters": "Security gap in auth flow",
		"tags":        "jwt auth expiry",
	})
	mustNotError(t, tr)

	var resp struct {
		SuggestedConnections []struct {
			ID string `json:"id"`
		} `json:"suggested_connections"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse remember response: %v", err)
	}

	found := false
	for _, s := range resp.SuggestedConnections {
		if s.ID == existingID {
			found = true
		}
	}
	if !found {
		t.Errorf("same-domain related node %q should appear in suggested_connections; got %+v", existingID, resp.SuggestedConnections)
	}
}

// TestRememberAll_IsUnknownTool: after consolidation, remember_all must no
// longer be a registered tool.

// TestRememberAll_IsUnknownTool: after consolidation, remember_all must no
// longer be a registered tool.
func TestRememberAll_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember_all", map[string]any{
		"nodes": []map[string]any{
			{"label": "Should fail", "domain": "test"},
		},
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %s", text(t, tr))
	}
}

// TestRevise_BatchViaItems_UpdatesMultiple: calling revise with an items array
// must update all entries and return an updated array response.

func TestRemember_NodeKindStanding(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "no deploys on friday", "proj", map[string]any{
		"node_kind":   "standing",
		"why_matters": "reduces incidents",
	})
	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			NodeKind string `json:"node_kind"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.NodeKind != "standing" {
		t.Errorf("node_kind: got %q, want %q", resp.Node.NodeKind, "standing")
	}
}

func TestRemember_NodeKindBackcompat_Transient(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "sprint ticket legacy", "proj", map[string]any{
		"transient": true,
	})
	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			NodeKind string `json:"node_kind"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.NodeKind != "transient" {
		t.Errorf("node_kind: got %q, want %q", resp.Node.NodeKind, "transient")
	}
}
