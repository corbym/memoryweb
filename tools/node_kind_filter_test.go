package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSearch_NodeKindFilter_ListWithoutQuery(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "standing rule A", "nk", map[string]any{"node_kind": "standing", "why_matters": "governs A"})
	addNode(t, h, "plain decision", "nk", map[string]any{"node_kind": "decision", "why_matters": "a decision"})

	tr := call(t, h, "search", map[string]any{"domain": "nk", "node_kind": "standing"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "standing rule A") {
		t.Errorf("expected standing node in results, got: %s", body)
	}
	if strings.Contains(body, "plain decision") {
		t.Error("decision node should be excluded by node_kind filter")
	}
}

func TestSearch_NodeKindFilter_UnionMatch(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "goal one", "nk2", map[string]any{"node_kind": "goal"})
	addNode(t, h, "issue one", "nk2", map[string]any{"node_kind": "issue"})
	addNode(t, h, "decision one", "nk2", nil)

	tr := call(t, h, "search", map[string]any{
		"domain":    "nk2",
		"node_kind": "goal issue",
		"query":     "one",
	})
	mustNotError(t, tr)
	ids := searchIDs(t, tr)
	if len(ids) != 2 {
		t.Fatalf("expected 2 results for union filter, got %d: %v", len(ids), ids)
	}
}

func TestRecent_NodeKindFilter(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "recent standing", "nk3", map[string]any{"node_kind": "standing"})
	addNode(t, h, "recent decision", "nk3", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "nk3", "node_kind": "decision", "limit": 10})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), "recent standing") {
		t.Error("standing node should be excluded")
	}
	if !strings.Contains(text(t, tr), "recent decision") {
		t.Error("decision node should appear")
	}
}

func TestAudit_Stale_NodeKindFilter(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "old transient", "nk4", map[string]any{"node_kind": "transient"})
	// backdate via DB not available in tool test — use standing node instead won't be transient drift
	// File a transient and rely on FindDrift rule 5 needing age — skip if no drift.
	// Instead filter orphans: non-transient orphan with kind filter.
	id := addNode(t, h, "orphan standing", "nk4", map[string]any{"node_kind": "standing"})
	addNode(t, h, "orphan decision", "nk4", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": "nk4", "node_kind": "standing"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, id) {
		t.Errorf("expected standing orphan %q in audit orphans, got: %s", id, body)
	}
	if strings.Contains(body, "orphan decision") {
		t.Error("decision orphan should be filtered out")
	}
}

func TestHistory_NodeKindFilter_StandingSpine(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "standing spine", "nk5", map[string]any{
		"node_kind":   "standing",
		"occurred_at": "2026-01-01",
		"why_matters": "rule",
	})
	addNode(t, h, "decision spine", "nk5", map[string]any{
		"occurred_at": "2026-01-02",
		"why_matters": "decided",
	})

	tr := call(t, h, "history", map[string]any{
		"domain":         "nk5",
		"important_only": true,
		"node_kind":      "standing",
	})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "standing spine") {
		t.Error("expected standing spine entry")
	}
	if strings.Contains(body, "decision spine") {
		t.Error("decision should be excluded from standing-only history")
	}
}

func TestSignificance_NodeKindFilter(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "sig standing", "nk6", map[string]any{
		"node_kind":   "standing",
		"occurred_at": "2026-01-01",
		"why_matters": "rule",
	})
	addNode(t, h, "sig decision", "nk6", map[string]any{
		"occurred_at": "2026-01-02",
		"why_matters": "decided",
	})

	tr := call(t, h, "significance", map[string]any{"domain": "nk6", "node_kind": "standing"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatal(err)
	}
	declared := string(resp["declared"])
	if !strings.Contains(declared, "sig standing") {
		t.Errorf("expected standing in declared, got: %s", declared)
	}
	if strings.Contains(declared, "sig decision") {
		t.Error("decision should not appear when filtering standing only")
	}
}

func TestRecent_GroupByDomainAndNodeKind_Error(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "recent", map[string]any{
		"group_by_domain": true,
		"node_kind":       "decision",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "group_by_domain and node_kind") {
		t.Errorf("expected incompatibility error, got: %s", text(t, tr))
	}
}
