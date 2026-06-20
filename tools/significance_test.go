package tools_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSignificance_ReturnsAllFourSections(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{"domain": "empty-domain"})
	mustNotError(t, tr)

	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	for _, key := range []string{"declared", "structural", "uncurated", "potentially_stale", "call_id"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}
}

func TestSignificance_IsErrorWhenNeitherDomainNorMemoryIDProvided(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{})
	mustError(t, tr)
}

func TestSignificance_StructuralRankingCorrect(t *testing.T) {
	_, h := newEnv(t)

	popular := addNode(t, h, "Popular node", "proj", nil)
	niche := addNode(t, h, "Niche node", "proj", nil)

	// 3 linkers → popular
	for i := 0; i < 3; i++ {
		linker := addNode(t, h, fmt.Sprintf("Linker %d", i), "proj", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": popular,
			"relationship": "connects_to", "narrative": "links",
		}))
	}
	// 1 linker → niche
	nicheLinker := addNode(t, h, "Niche linker", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nicheLinker, "to_memory": niche,
		"relationship": "connects_to", "narrative": "links",
	}))

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Structural []struct {
			ID              string  `json:"id"`
			ImportanceScore float64 `json:"importance_score"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Structural) < 2 {
		t.Fatalf("expected at least 2 structural entries, got %d", len(resp.Structural))
	}
	if resp.Structural[0].ID != popular {
		t.Errorf("Structural[0]: want %q (popular), got %q", popular, resp.Structural[0].ID)
	}
}

func TestSignificance_PotentiallyStaleDetected(t *testing.T) {
	_, h := newEnv(t)

	// Node with occurred_at but no inbound edges — structurally irrelevant.
	isolated := addNode(t, h, "Isolated significant node", "proj", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z",
		"why_matters": "key decision with no dependants",
	})

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		PotentiallyStale []struct {
			ID string `json:"id"`
		} `json:"potentially_stale"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	found := false
	for _, n := range resp.PotentiallyStale {
		if n.ID == isolated {
			found = true
		}
	}
	if !found {
		t.Error("isolated node with occurred_at and no inbound edges should appear in potentially_stale")
	}
}

func TestSignificance_DefaultsApplied(t *testing.T) {
	// Omitting limit and recency_window should default to 10 and 90.
	// Verify: a domain with >10 linker targets returns at most 10 structural entries.
	_, h := newEnv(t)

	for i := 0; i < 12; i++ {
		target := addNode(t, h, fmt.Sprintf("Target %d", i), "proj", nil)
		linker := addNode(t, h, fmt.Sprintf("Linker %d", i), "proj", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": target,
			"relationship": "connects_to", "narrative": "links",
		}))
	}

	tr := call(t, h, "significance", map[string]any{"domain": "proj"})
	mustNotError(t, tr)

	var resp struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(resp.Structural) > 10 {
		t.Errorf("default limit=10: structural should have at most 10 entries, got %d", len(resp.Structural))
	}
}

// ── forget / forget_all discoverability ──────────────────────────────────────

// TestListTools_ForgetAllLeadsWithUseCase: forget_all description must open with
// "Batch archive" so agents reach for it when archiving multiple nodes.

// TestSignificance_MemoryIDMode_ReturnsAllFourSections: calling significance
// with a memory_id must return all four sections without error.
func TestSignificance_MemoryIDMode_ReturnsAllFourSections(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor memory", "proj", nil)
	neighbour := addNode(t, h, "neighbour memory", "proj", nil)
	linker := addNode(t, h, "linker memory", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "linked",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "linker points to neighbour",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	out := text(t, tr)
	for _, section := range []string{"declared", "structural", "uncurated", "potentially_stale"} {
		if !strings.Contains(out, section) {
			t.Errorf("expected section %q in significance response; got: %s", section, out)
		}
	}
}

// TestSignificance_MemoryIDMode_DomainClipped: cross-domain nodes connected to
// the anchor must NOT appear in the neighbourhood result.

// TestSignificance_MemoryIDMode_DomainClipped: cross-domain nodes connected to
// the anchor must NOT appear in the neighbourhood result.
func TestSignificance_MemoryIDMode_DomainClipped(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor proj", "proj", nil)
	crossDomain := addNode(t, h, "foreign memory", "other-domain", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": crossDomain,
		"relationship": "connects_to", "narrative": "cross-domain edge",
	}))
	linker := addNode(t, h, "linker for foreign", "other-domain", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": crossDomain,
		"relationship": "connects_to", "narrative": "linker",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor})
	mustNotError(t, tr)
	out := text(t, tr)
	if strings.Contains(out, crossDomain) {
		t.Errorf("cross-domain memory %q must not appear in neighbourhood significance", crossDomain)
	}
}

// TestSignificance_MemoryIDMode_Depth2Included: a node two hops from the
// anchor (anchor→A→B) must appear in the result when depth=2.

// TestSignificance_MemoryIDMode_Depth2Included: a node two hops from the
// anchor (anchor→A→B) must appear in the result when depth=2.
func TestSignificance_MemoryIDMode_Depth2Included(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor node d2", "proj", nil)
	nodeA := addNode(t, h, "depth1 node d2", "proj", nil)
	nodeB := addNode(t, h, "depth2 node d2", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": nodeA,
		"relationship": "connects_to", "narrative": "a1",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nodeA, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "a2",
	}))
	linker := addNode(t, h, "linker for b d2", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "linker b",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor, "depth": 2})
	mustNotError(t, tr)
	if !strings.Contains(text(t, tr), nodeB) {
		t.Errorf("depth-2 memory %q must appear in result with depth=2", nodeB)
	}
}

// TestSignificance_MemoryIDMode_Depth1Excluded: a node two hops from the
// anchor must NOT appear when depth=1.

// TestSignificance_MemoryIDMode_Depth1Excluded: a node two hops from the
// anchor must NOT appear when depth=1.
func TestSignificance_MemoryIDMode_Depth1Excluded(t *testing.T) {
	_, h := newEnv(t)
	anchor := addNode(t, h, "anchor node d1", "proj", nil)
	nodeA := addNode(t, h, "depth1 node d1", "proj", nil)
	nodeB := addNode(t, h, "depth2 node d1", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": nodeA,
		"relationship": "connects_to", "narrative": "a1",
	}))
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": nodeA, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "a2",
	}))
	linker := addNode(t, h, "linker for b d1", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": nodeB,
		"relationship": "connects_to", "narrative": "linker b",
	}))

	tr := call(t, h, "significance", map[string]any{"memory_id": anchor, "depth": 1})
	mustNotError(t, tr)
	if strings.Contains(text(t, tr), nodeB) {
		t.Errorf("depth-2 memory %q must NOT appear in result when depth=1", nodeB)
	}
}

// TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain: when both domain
// and memory_id are supplied, memory_id mode runs (neighbourhood is smaller
// than the full domain).

// TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain: when both domain
// and memory_id are supplied, memory_id mode runs (neighbourhood is smaller
// than the full domain).
func TestSignificance_MemoryIDMode_TakesPrecedenceOverDomain(t *testing.T) {
	_, h := newEnv(t)
	for i := 0; i < 4; i++ {
		popular := addNode(t, h, fmt.Sprintf("popular domain node %d", i), "proj", nil)
		for j := 0; j < 3; j++ {
			lnk := addNode(t, h, fmt.Sprintf("domain linker %d-%d", i, j), "proj", nil)
			mustNotError(t, call(t, h, "connect", map[string]any{
				"from_memory": lnk, "to_memory": popular,
				"relationship": "connects_to", "narrative": "link",
			}))
		}
	}
	anchor := addNode(t, h, "isolated anchor prec", "proj", nil)
	neighbour := addNode(t, h, "only neighbour prec", "proj", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": anchor, "to_memory": neighbour,
		"relationship": "connects_to", "narrative": "sole link",
	}))

	memoryTR := call(t, h, "significance", map[string]any{"domain": "proj", "memory_id": anchor})
	mustNotError(t, memoryTR)

	for i := 0; i < 4; i++ {
		if strings.Contains(text(t, memoryTR), fmt.Sprintf("popular domain node %d", i)) {
			t.Errorf("popular domain node %d must not appear in memory_id mode result", i)
		}
	}
}

// TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID: passing a
// non-existent memory_id must return an error.

// TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID: passing a
// non-existent memory_id must return an error.
func TestSignificance_MemoryIDMode_IsErrorOnUnknownMemoryID(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "significance", map[string]any{"memory_id": "nonexistent-memory-id-xyz"})
	mustError(t, tr)
}

// TestSignificance_MemoryIDMode_InSchemaWithDepth: the significance schema
// must expose memory_id and depth properties, and domain must not be in the
// Required array.

// TestSignificance_TagsFilter_IncludesMatchingNodes: when tags="mytag", only
// nodes whose tags field contains "mytag" appear in structural.
func TestSignificance_TagsFilter_IncludesMatchingNodes(t *testing.T) {
	_, h := newEnv(t)

	tagged := addNode(t, h, "Tagged node", "proj", map[string]interface{}{"tags": "mytag"})
	untagged := addNode(t, h, "Untagged node", "proj", nil)
	linker1 := addNode(t, h, "Linker for tagged", "proj", nil)
	linker2 := addNode(t, h, "Linker for untagged", "proj", nil)

	call(t, h, "connect", map[string]interface{}{"from_memory": linker1, "to_memory": tagged, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker2, "to_memory": untagged, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "mytag"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	for _, sn := range res.Structural {
		if sn.ID == untagged {
			t.Error("structural contains untagged node; expected only tagged nodes")
		}
	}
	found := false
	for _, sn := range res.Structural {
		if sn.ID == tagged {
			found = true
		}
	}
	if !found {
		t.Error("structural does not contain tagged node")
	}
}

// TestSignificance_TagsFilter_MultiTag_OR: tags="foo,bar" includes nodes
// matching either tag.

// TestSignificance_TagsFilter_MultiTag_OR: tags="foo,bar" includes nodes
// matching either tag.
func TestSignificance_TagsFilter_MultiTag_OR(t *testing.T) {
	_, h := newEnv(t)

	fooNode := addNode(t, h, "Foo node", "proj", map[string]interface{}{"tags": "foo"})
	barNode := addNode(t, h, "Bar node", "proj", map[string]interface{}{"tags": "bar"})
	neither := addNode(t, h, "Neither node", "proj", nil)
	linker1 := addNode(t, h, "Linker foo", "proj", nil)
	linker2 := addNode(t, h, "Linker bar", "proj", nil)
	linker3 := addNode(t, h, "Linker neither", "proj", nil)

	call(t, h, "connect", map[string]interface{}{"from_memory": linker1, "to_memory": fooNode, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker2, "to_memory": barNode, "relationship": "connects_to", "because": "link"})
	call(t, h, "connect", map[string]interface{}{"from_memory": linker3, "to_memory": neither, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "foo,bar"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	ids := map[string]bool{}
	for _, sn := range res.Structural {
		ids[sn.ID] = true
	}
	if !ids[fooNode] {
		t.Error("structural missing foo node")
	}
	if !ids[barNode] {
		t.Error("structural missing bar node")
	}
	if ids[neither] {
		t.Error("structural contains neither node; expected only foo and bar")
	}
}

// TestSignificance_TagsFilter_NoMatch_EmptyStructural: when the tag matches
// no nodes, structural is empty and the call does not error.

// TestSignificance_TagsFilter_NoMatch_EmptyStructural: when the tag matches
// no nodes, structural is empty and the call does not error.
func TestSignificance_TagsFilter_NoMatch_EmptyStructural(t *testing.T) {
	_, h := newEnv(t)

	node := addNode(t, h, "Some node", "proj", nil)
	linker := addNode(t, h, "Linker", "proj", nil)
	call(t, h, "connect", map[string]interface{}{"from_memory": linker, "to_memory": node, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "nonexistent-tag"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if len(res.Structural) != 0 {
		t.Errorf("structural: want 0 entries, got %d", len(res.Structural))
	}
}

// TestSignificance_TagsFilter_WholeWordMatch: a node tagged "foobar" must not
// appear when filtering by tags="foo" (partial match must not fire).

// TestSignificance_TagsFilter_WholeWordMatch: a node tagged "foobar" must not
// appear when filtering by tags="foo" (partial match must not fire).
func TestSignificance_TagsFilter_WholeWordMatch(t *testing.T) {
	_, h := newEnv(t)

	foobar := addNode(t, h, "Foobar node", "proj", map[string]interface{}{"tags": "foobar"})
	linker := addNode(t, h, "Linker", "proj", nil)
	call(t, h, "connect", map[string]interface{}{"from_memory": linker, "to_memory": foobar, "relationship": "connects_to", "because": "link"})

	tr := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "foo"})
	mustNotError(t, tr)

	var res struct {
		Structural []struct {
			ID string `json:"id"`
		} `json:"structural"`
	}
	if err := json.Unmarshal([]byte(tr.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	for _, sn := range res.Structural {
		if sn.ID == foobar {
			t.Error("structural contains foobar node on partial tag match 'foo'; whole-word match required")
		}
	}
}

// TestSignificance_TagsFilter_DeclaredRespected: a node with occurred_at and a
// matching tag must appear in declared when filtering by that tag.

// TestSignificance_TagsFilter_DeclaredRespected: a node with occurred_at and a
// matching tag must appear in declared when filtering by that tag.
func TestSignificance_TagsFilter_DeclaredRespected(t *testing.T) {
	_, h := newEnv(t)

	declaredID := addNode(t, h, "Declared tagged node", "proj", map[string]interface{}{
		"description": "has occurred_at and tag",
		"why_matters": "test",
		"occurred_at": "2024-01-15",
		"tags":        "release",
	})

	other := addNode(t, h, "Untagged declared", "proj", map[string]interface{}{"why_matters": "other"})
	call(t, h, "revise", map[string]interface{}{"id": other, "occurred_at": "2024-02-01", "why_matters": "other"})

	result := call(t, h, "significance", map[string]interface{}{"domain": "proj", "tags": "release"})
	mustNotError(t, result)

	var res struct {
		Declared []struct {
			ID string `json:"id"`
		} `json:"declared"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &res); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	found := false
	for _, n := range res.Declared {
		if n.ID == declaredID {
			found = true
		}
		if n.ID == other {
			t.Errorf("declared contains untagged node %q; expected only release-tagged nodes", other)
		}
	}
	if !found {
		t.Errorf("declared does not contain node %q with tag 'release'", declaredID)
	}
}

// ── NodeKind ──────────────────────────────────────────────────────────────────
