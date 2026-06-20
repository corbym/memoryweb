package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/corbym/memoryweb/tools"
)

func TestTimeline_OrderedByOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "Early event", "proj", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "first in timeline order test",
	})
	id2 := addNode(t, h, "Late event", "proj", map[string]any{
		"occurred_at": "2026-06-01",
		"why_matters": "second in timeline order test",
	})

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	if len(nodes) < 2 {
		t.Fatalf("expected 2 timeline nodes, got %d", len(nodes))
	}
	if nodes[0].ID != id1 || nodes[1].ID != id2 {
		t.Errorf("timeline order wrong: got [%s, %s], want [%s, %s]",
			nodes[0].ID, nodes[1].ID, id1, id2)
	}
}

func TestTimeline_ExcludesNodesWithoutOccurredAt(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "No date node", "proj", nil) // no occurred_at
	idDated := addNode(t, h, "Dated node", "proj", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline for excludes-no-date test",
	})

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID == idDated {
			return // found it — pass
		}
	}
	t.Error("dated node not in timeline")
}

func TestTimeline_ArchivedNodeExcluded(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Archived timeline node", "proj", map[string]any{
		"occurred_at": "2026-03-15",
		"why_matters": "baseline for archive-exclusion test",
	})
	store.ArchiveNode(id, "test")

	tr := call(t, h, "history", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	for _, n := range nodes {
		if n.ID == id {
			t.Error("archived node should not appear in timeline")
		}
	}
}

func TestTimeline_DateRangeFilter(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Jan event", "proj", map[string]any{
		"occurred_at": "2026-01-15",
		"why_matters": "outside range — before",
	})
	idMar := addNode(t, h, "Mar event", "proj", map[string]any{
		"occurred_at": "2026-03-15",
		"why_matters": "inside date range",
	})
	addNode(t, h, "Jun event", "proj", map[string]any{
		"occurred_at": "2026-06-15",
		"why_matters": "outside range — after",
	})

	tr := call(t, h, "history", map[string]any{
		"domain": "proj",
		"from":   "2026-02-01",
		"to":     "2026-04-30",
	})
	mustNotError(t, tr)

	var nodes []struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(text(t, tr)), &nodes)
	if len(nodes) != 1 || nodes[0].ID != idMar {
		t.Errorf("date range should return only Mar event; got %+v", nodes)
	}
}

func TestTimeline_InvalidFromDate(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{"from": "not-a-date"})
	mustError(t, tr)
}

func TestTimeline_EmptyReturnsGracefully(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{})
	mustNotError(t, tr)
}

// ── list_domains ──────────────────────────────────────────────────────────────

func historyIDs(t *testing.T, tr *tools.ToolResult) []string {
	t.Helper()
	var nodes []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("parse history response: %v", err)
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func TestHistory_DefaultMode_IncludesAllNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	// Node with occurred_at.
	idDated := addNode(t, h, "dated", "hist", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "baseline for default mode test",
	})
	// Node without occurred_at.
	idUndated := addNode(t, h, "undated", "hist", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idDated) {
		t.Error("default mode: dated node should be included")
	}
	if !contains(ids, idUndated) {
		t.Error("default mode: undated node should be included")
	}
}

func TestHistory_ImportantOnly_ExcludesUndatedNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idDated := addNode(t, h, "dated event", "hist2", map[string]any{
		"occurred_at": "2026-04-01",
		"why_matters": "baseline for important-only test",
	})
	idUndated := addNode(t, h, "undated node", "hist2", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist2", "important_only": true})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idDated) {
		t.Error("important_only: dated node should appear")
	}
	if contains(ids, idUndated) {
		t.Error("important_only: undated node should not appear")
	}
}

func TestHistory_TagFilter_WholeWordMatch(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idA := addNode(t, h, "tagged A", "hist3", map[string]any{"tags": "decision architecture"})
	idB := addNode(t, h, "tagged B", "hist3", map[string]any{"tags": "architecture release"})
	idC := addNode(t, h, "no match C", "hist3", map[string]any{"tags": "release"})
	idD := addNode(t, h, "no tags D", "hist3", nil)

	tr := call(t, h, "history", map[string]any{"domain": "hist3", "tags": "architecture"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, idA) {
		t.Error("node with 'decision architecture' should match tag 'architecture'")
	}
	if !contains(ids, idB) {
		t.Error("node with 'architecture release' should match tag 'architecture'")
	}
	if contains(ids, idC) {
		t.Error("node with only 'release' should not match tag 'architecture'")
	}
	if contains(ids, idD) {
		t.Error("node with no tags should not match tag 'architecture'")
	}
}

// ── history: memory_id mode ───────────────────────────────────────────────────

// TestHistory_MemoryIDMode_ReturnsChronological: nodes in the anchor's
// neighbourhood are returned in COALESCE(occurred_at, created_at) ASC order.

// TestHistory_MemoryIDMode_ReturnsChronological: nodes in the anchor's
// neighbourhood are returned in COALESCE(occurred_at, created_at) ASC order.
func TestHistory_MemoryIDMode_ReturnsChronological(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid", map[string]any{"occurred_at": "2026-01-01", "why_matters": "anchor node"})
	n1 := addNode(t, h, "March node", "hmid", map[string]any{"occurred_at": "2026-03-01", "why_matters": "march event"})
	n2 := addNode(t, h, "June node", "hmid", map[string]any{"occurred_at": "2026-06-01", "why_matters": "june event"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": n1, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": n2, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if !contains(ids, anchor) || !contains(ids, n1) || !contains(ids, n2) {
		t.Fatalf("expected all three nodes, got %v", ids)
	}
	anchorIdx, n1Idx, n2Idx := indexOf(ids, anchor), indexOf(ids, n1), indexOf(ids, n2)
	if !(anchorIdx < n1Idx && n1Idx < n2Idx) {
		t.Errorf("wrong order: anchor=%d n1=%d n2=%d in %v", anchorIdx, n1Idx, n2Idx, ids)
	}
}

// TestHistory_MemoryIDMode_DomainClipped: a node in a different domain
// connected to the anchor must not appear.

// TestHistory_MemoryIDMode_DomainClipped: a node in a different domain
// connected to the anchor must not appear.
func TestHistory_MemoryIDMode_DomainClipped(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid2", nil)
	foreign := addNode(t, h, "Foreign", "other-domain", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": foreign, "relationship": "connects_to", "because": "cross"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, foreign) {
		t.Error("foreign-domain node should not appear in memory_id history")
	}
}

// TestHistory_MemoryIDMode_ImportantOnly: important_only=true filters to nodes
// with occurred_at even in memory_id mode.

// TestHistory_MemoryIDMode_ImportantOnly: important_only=true filters to nodes
// with occurred_at even in memory_id mode.
func TestHistory_MemoryIDMode_ImportantOnly(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid3", nil)
	dated := addNode(t, h, "Dated", "hmid3", map[string]any{"occurred_at": "2026-04-01", "why_matters": "dated decision"})
	undated := addNode(t, h, "Undated", "hmid3", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": dated, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": undated, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "important_only": true})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, undated) {
		t.Error("important_only: undated node should not appear")
	}
	if contains(ids, anchor) {
		t.Error("important_only: anchor (no occurred_at) should not appear")
	}
	if !contains(ids, dated) {
		t.Error("important_only: dated node should appear")
	}
}

// TestHistory_MemoryIDMode_TagsFilter: tags filter applies in memory_id mode.

// TestHistory_MemoryIDMode_TagsFilter: tags filter applies in memory_id mode.
func TestHistory_MemoryIDMode_TagsFilter(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid4", nil)
	tagged := addNode(t, h, "Tagged", "hmid4", map[string]any{"tags": "release"})
	untagged := addNode(t, h, "Untagged", "hmid4", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": tagged, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": untagged, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "tags": "release"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, untagged) {
		t.Error("tag filter: untagged node should not appear")
	}
	if !contains(ids, tagged) {
		t.Error("tag filter: tagged node should appear")
	}
}

// TestHistory_MemoryIDMode_TakesPrecedenceOverDomain: when both memory_id and
// domain are supplied, the result is scoped to the neighbourhood, not the full domain.

// TestHistory_MemoryIDMode_TakesPrecedenceOverDomain: when both memory_id and
// domain are supplied, the result is scoped to the neighbourhood, not the full domain.
func TestHistory_MemoryIDMode_TakesPrecedenceOverDomain(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)

	anchor := addNode(t, h, "Anchor", "hmid5", nil)
	connected := addNode(t, h, "Connected", "hmid5", nil)
	notConnected := addNode(t, h, "NotConnected", "hmid5", nil)
	call(t, h, "connect", map[string]any{"from_memory": anchor, "to_memory": connected, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "history", map[string]any{"memory_id": anchor, "domain": "hmid5"})
	mustNotError(t, tr)
	ids := historyIDs(t, tr)
	if contains(ids, notConnected) {
		t.Error("memory_id takes precedence: unconnected domain node should not appear")
	}
	if !contains(ids, connected) {
		t.Error("connected node should appear")
	}
}

// TestHistory_MemoryIDMode_UnknownMemoryID: passing an unknown memory_id
// must return an error, not an empty list.

// TestHistory_MemoryIDMode_UnknownMemoryID: passing an unknown memory_id
// must return an error, not an empty list.
func TestHistory_MemoryIDMode_UnknownMemoryID(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	tr := call(t, h, "history", map[string]any{"memory_id": "no-such-id-00000000"})
	mustError(t, tr)
}

// TestHistory_MemoryIDMode_InSchema: the history schema must expose memory_id
// and depth properties.
