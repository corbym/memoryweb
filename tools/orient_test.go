package tools_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestSummariseDomain_ReturnsNodes: the response must contain the labels of
// all live nodes in the domain.
func TestSummariseDomain_ReturnsNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha summarise node", "sum-domain", map[string]any{
		"description": "first node description",
		"why_matters": "first node why matters",
	})
	addNode(t, h, "Beta summarise node", "sum-domain", map[string]any{
		"description": "second node description",
		"why_matters": "second node why matters",
	})
	addNode(t, h, "Gamma summarise node", "sum-domain", map[string]any{
		"description": "third node description",
		"why_matters": "third node why matters",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "sum-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	for _, label := range []string{"Alpha summarise node", "Beta summarise node", "Gamma summarise node"} {
		if !strings.Contains(body, label) {
			t.Errorf("result should contain label %q; got:\n%s", label, body)
		}
	}
}

// TestSummariseDomain_EmptyDomain: a domain with no nodes returns a clear
// "nothing filed" message rather than empty content.

// TestSummariseDomain_EmptyDomain: a domain with no nodes returns a clear
// "nothing filed" message rather than empty content.
func TestSummariseDomain_EmptyDomain(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "orient", map[string]any{"domain": "completely-empty-domain-xyz"})
	mustNotError(t, tr)
	body := text(t, tr)
	if !strings.Contains(body, "Nothing has been filed") {
		t.Errorf("empty domain should return 'Nothing has been filed' message; got:\n%s", body)
	}
}

// TestSummariseDomain_ExcludesArchived: an archived node's label must not
// appear in the summarise_domain response.

// TestSummariseDomain_ExcludesArchived: an archived node's label must not
// appear in the summarise_domain response.
func TestSummariseDomain_ExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	addNode(t, h, "Visible summarise node", "sum-archive-domain", nil)
	hiddenID := addNode(t, h, "Hidden archived summarise node", "sum-archive-domain", nil)
	store.ArchiveNode(hiddenID, "test archive")

	tr := call(t, h, "orient", map[string]any{"domain": "sum-archive-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	if strings.Contains(body, "Hidden archived summarise node") {
		t.Errorf("archived node label should NOT appear in summarise_domain result; got:\n%s", body)
	}
	if !strings.Contains(body, "Visible summarise node") {
		t.Errorf("live node label should appear in result; got:\n%s", body)
	}
}

// TestSummariseDomain_IncludesRecentChanges: a node with occurred_at set must
// have that date visible in the response.

// TestSummariseDomain_IncludesRecentChanges: a node with occurred_at set must
// have that date visible in the response.
func TestSummariseDomain_IncludesRecentChanges(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated node one", "sum-dated-domain", nil)
	addNode(t, h, "Undated node two", "sum-dated-domain", nil)
	addNode(t, h, "Dated event node", "sum-dated-domain", map[string]any{
		"occurred_at": "2026-04-01",
		"why_matters": "significant milestone for the domain",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "sum-dated-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	if !strings.Contains(body, "2026-04-01") {
		t.Errorf("result should include occurred_at date '2026-04-01'; got:\n%s", body)
	}
}

// TestOrient_LiveNodesCount: orient response must include live_nodes reflecting
// only non-archived nodes.

// TestOrient_LiveNodesCount: orient response must include live_nodes reflecting
// only non-archived nodes.
func TestOrient_LiveNodesCount(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha orient", "orient-counts", nil)
	addNode(t, h, "Beta orient", "orient-counts", nil)
	addNode(t, h, "Gamma orient", "orient-counts", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-counts"})
	mustNotError(t, tr)

	var resp struct {
		LiveNodes int `json:"live_nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.LiveNodes != 3 {
		t.Errorf("live_nodes: got %d, want 3", resp.LiveNodes)
	}
}

// TestOrient_ArchivedNodesCount: orient response must include archived_nodes
// reflecting soft-deleted nodes; live_nodes must exclude them.

// TestOrient_ArchivedNodesCount: orient response must include archived_nodes
// reflecting soft-deleted nodes; live_nodes must exclude them.
func TestOrient_ArchivedNodesCount(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Keep A", "orient-arch-count", nil)
	addNode(t, h, "Keep B", "orient-arch-count", nil)
	archiveID := addNode(t, h, "Archive me", "orient-arch-count", nil)

	call(t, h, "forget", map[string]any{"id": archiveID, "reason": "test"})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-arch-count"})
	mustNotError(t, tr)

	var resp struct {
		LiveNodes     int `json:"live_nodes"`
		ArchivedNodes int `json:"archived_nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.LiveNodes != 2 {
		t.Errorf("live_nodes: got %d, want 2", resp.LiveNodes)
	}
	if resp.ArchivedNodes != 1 {
		t.Errorf("archived_nodes: got %d, want 1", resp.ArchivedNodes)
	}
}

// TestOrient_NoTotalNodes: orient response must NOT contain a total_nodes field.

// TestOrient_NoTotalNodes: orient response must NOT contain a total_nodes field.
func TestOrient_NoTotalNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Some node", "orient-no-total", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-no-total"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["total_nodes"]; ok {
		t.Error("orient response must not contain total_nodes — superseded by live_nodes")
	}
}

// TestOrient_ResponseIncludesServerVersion: orient response must include a
// server_version field so agents can detect schema drift after a server update.

// TestOrient_ResponseIncludesServerVersion: orient response must include a
// server_version field so agents can detect schema drift after a server update.
func TestOrient_ResponseIncludesServerVersion(t *testing.T) {
	_, h := newEnv(t) // newEnv creates handler with version "dev"
	addNode(t, h, "Version test node", "orient-version", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-version"})
	mustNotError(t, tr)

	var resp struct {
		ServerVersion string `json:"server_version"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.ServerVersion == "" {
		t.Error("server_version must be present and non-empty in orient response")
	}
	if resp.ServerVersion != "dev" {
		t.Errorf("server_version: got %q, want %q", resp.ServerVersion, "dev")
	}
}

// TestOrient_DeclaredSpineEmpty: orient on a domain whose nodes all lack
// occurred_at must return an empty declared_spine list.

// TestOrient_DeclaredSpineEmpty: orient on a domain whose nodes all lack
// occurred_at must return an empty declared_spine list.
func TestOrient_DeclaredSpineEmpty(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated alpha", "orient-spine-empty", nil)
	addNode(t, h, "Undated beta", "orient-spine-empty", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-empty"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 0 {
		t.Errorf("declared_spine: got %d entries, want 0", len(resp.DeclaredSpine))
	}
}

// TestOrient_DeclaredSpineOnlyContainsOccurredAtNodes: only nodes with
// occurred_at set must appear in declared_spine; undated nodes must not.

// TestOrient_DeclaredSpineOnlyContainsOccurredAtNodes: only nodes with
// occurred_at set must appear in declared_spine; undated nodes must not.
func TestOrient_DeclaredSpineOnlyContainsOccurredAtNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Undated node", "orient-spine-filter", nil)
	addNode(t, h, "Dated decision", "orient-spine-filter", map[string]any{
		"occurred_at": "2026-03-10",
		"why_matters": "significant choice that shaped the architecture",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-filter"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 1 {
		t.Fatalf("declared_spine: got %d entries, want 1", len(resp.DeclaredSpine))
	}
	if resp.DeclaredSpine[0].Label != "Dated decision" {
		t.Errorf("declared_spine[0].label: got %q, want %q", resp.DeclaredSpine[0].Label, "Dated decision")
	}
}

// TestOrient_DeclaredSpineIsChronological: multiple dated entries in the spine
// must be ordered by occurred_at ascending.

// TestOrient_DeclaredSpineIsChronological: multiple dated entries in the spine
// must be ordered by occurred_at ascending.
func TestOrient_DeclaredSpineIsChronological(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Third decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-05-01",
		"why_matters": "third in sequence",
	})
	addNode(t, h, "First decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "first in sequence",
	})
	addNode(t, h, "Second decision", "orient-spine-order", map[string]any{
		"occurred_at": "2026-03-01",
		"why_matters": "second in sequence",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-order"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine []struct {
			Label string `json:"label"`
		} `json:"declared_spine"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.DeclaredSpine) != 3 {
		t.Fatalf("declared_spine: got %d entries, want 3", len(resp.DeclaredSpine))
	}
	want := []string{"First decision", "Second decision", "Third decision"}
	for i, w := range want {
		if resp.DeclaredSpine[i].Label != w {
			t.Errorf("declared_spine[%d].label: got %q, want %q", i, resp.DeclaredSpine[i].Label, w)
		}
	}
}

// TestOrient_DeclaredSpineExcludesArchived: an archived node with occurred_at
// must not appear in the declared_spine.

// TestOrient_DeclaredSpineExcludesArchived: an archived node with occurred_at
// must not appear in the declared_spine.
func TestOrient_DeclaredSpineExcludesArchived(t *testing.T) {
	store, h := newEnv(t)
	addNode(t, h, "Live dated decision", "orient-spine-archive", map[string]any{
		"occurred_at": "2026-04-01",
		"why_matters": "live and significant",
	})
	archivedID := addNode(t, h, "Archived dated decision", "orient-spine-archive", map[string]any{
		"occurred_at": "2026-04-02",
		"why_matters": "will be archived",
	})
	store.ArchiveNode(archivedID, "test archive")

	tr := call(t, h, "orient", map[string]any{"domain": "orient-spine-archive"})
	mustNotError(t, tr)

	body := text(t, tr)
	if strings.Contains(body, "Archived dated decision") {
		t.Error("archived node must not appear in declared_spine")
	}
	if !strings.Contains(body, "Live dated decision") {
		t.Error("live dated node must appear in declared_spine")
	}
}

// ── orient: significant section + no all_nodes ───────────────────────────────

// TestOrient_HasSignificantSection: orient response must include a `significant`
// array. It may be empty when no edges exist in the domain.

// TestOrient_HasSignificantSection: orient response must include a `significant`
// array. It may be empty when no edges exist in the domain.
func TestOrient_HasSignificantSection(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Lone node", "orient-sig", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig"})
	mustNotError(t, tr)

	var resp struct {
		Significant *json.RawMessage `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.Significant == nil {
		t.Error("significant field must be present in orient response (even if empty)")
	}
}

// TestOrient_SignificantRankedByImportance: the node with more inbound edges from
// recent nodes must appear first in the significant section.

// TestOrient_SignificantRankedByImportance: the node with more inbound edges from
// recent nodes must appear first in the significant section.
func TestOrient_SignificantRankedByImportance(t *testing.T) {
	_, h := newEnv(t)
	popularID := addNode(t, h, "Popular node", "orient-sig-rank", nil)
	nicheID := addNode(t, h, "Niche node", "orient-sig-rank", nil)

	// Three linkers → popular
	for _, label := range []string{"Linker A", "Linker B", "Linker C"} {
		linkerID := addNode(t, h, label, "orient-sig-rank", nil)
		call(t, h, "connect", map[string]any{
			"from_memory":  linkerID,
			"to_memory":    popularID,
			"relationship": "connects_to",
			"narrative":    "links to popular",
		})
	}
	// One linker → niche
	nicheLinkerID := addNode(t, h, "Niche linker", "orient-sig-rank", nil)
	call(t, h, "connect", map[string]any{
		"from_memory":  nicheLinkerID,
		"to_memory":    nicheID,
		"relationship": "connects_to",
		"narrative":    "links to niche",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig-rank"})
	mustNotError(t, tr)

	var resp struct {
		Significant []struct {
			ID string `json:"id"`
		} `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Significant) < 2 {
		t.Fatalf("significant: want at least 2 entries, got %d", len(resp.Significant))
	}
	if resp.Significant[0].ID != popularID {
		t.Errorf("significant[0]: got %q, want popular node %q", resp.Significant[0].ID, popularID)
	}
}

// TestOrient_NoAllNodes: orient response must NOT include a top-level `nodes`
// (all_nodes dump) field. The response is the three-section design only.

// TestOrient_NoAllNodes: orient response must NOT include a top-level `nodes`
// (all_nodes dump) field. The response is the three-section design only.
func TestOrient_NoAllNodes(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Test node", "orient-no-all", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-no-all"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["nodes"]; ok {
		t.Error("orient response must not contain a top-level `nodes` field (all_nodes dump removed)")
	}
}

// TestOrient_DescriptionImperativeFirst: orient description must not start with
// "The " or "This " — it must open with an imperative verb.

// TestOrient_DescriptionImperativeFirst: orient description must not start with
// "The " or "This " — it must open with an imperative verb.
func TestOrient_DescriptionImperativeFirst(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if strings.HasPrefix(td.Description, "The ") || strings.HasPrefix(td.Description, "This ") {
				t.Errorf("orient description starts with %q — must open with an imperative verb",
					td.Description[:min(50, len(td.Description))])
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
}

// TestOrient_RecentCappedAtFive: the recent section must contain at most 5 entries
// even when more than 5 live nodes exist in the domain.

// TestOrient_RecentCappedAtFive: the recent section must contain at most 5 entries
// even when more than 5 live nodes exist in the domain.
func TestOrient_RecentCappedAtFive(t *testing.T) {
	_, h := newEnv(t)
	for i := 0; i < 10; i++ {
		addNode(t, h, fmt.Sprintf("Node %02d", i), "orient-recent-cap", nil)
	}

	tr := call(t, h, "orient", map[string]any{"domain": "orient-recent-cap"})
	mustNotError(t, tr)

	var resp struct {
		Recent []json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) > 5 {
		t.Errorf("recent: got %d entries, want at most 5", len(resp.Recent))
	}
}

// TestOrient_SignificantCappedAtTen: significant must contain at most 10 entries
// even when more than 10 nodes have inbound edges in the domain.

// TestOrient_SignificantCappedAtTen: significant must contain at most 10 entries
// even when more than 10 nodes have inbound edges in the domain.
func TestOrient_SignificantCappedAtTen(t *testing.T) {
	_, h := newEnv(t)
	// 12 hub nodes each with one inbound linker → all qualify for significant.
	for i := 0; i < 12; i++ {
		hubID := addNode(t, h, fmt.Sprintf("Hub %02d", i), "orient-sig-cap", nil)
		linkerID := addNode(t, h, fmt.Sprintf("Linker %02d", i), "orient-sig-cap", nil)
		call(t, h, "connect", map[string]any{
			"from_memory":  linkerID,
			"to_memory":    hubID,
			"relationship": "connects_to",
			"narrative":    "links to hub",
		})
	}

	tr := call(t, h, "orient", map[string]any{"domain": "orient-sig-cap"})
	mustNotError(t, tr)

	var resp struct {
		Significant []json.RawMessage `json:"significant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Significant) > 10 {
		t.Errorf("significant: got %d entries, want at most 10", len(resp.Significant))
	}
}

// TestOrient_LeanFormat_NoDescription: orient must not include a description field
// in any section entry — lean format returns id, label, why_matters only.

// TestOrient_LeanFormat_NoDescription: orient must not include a description field
// in any section entry — lean format returns id, label, why_matters only.
func TestOrient_LeanFormat_NoDescription(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node with description", "orient-lean-nodesc", map[string]any{
		"description": "This description must not appear in orient output.",
		"why_matters": "It matters because of X.",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-nodesc"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	for _, section := range []string{"declared_spine", "significant", "recent"} {
		sectionRaw, ok := raw[section]
		if !ok {
			continue
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(sectionRaw, &entries); err != nil {
			continue
		}
		for _, entry := range entries {
			if _, hasDesc := entry["description"]; hasDesc {
				t.Errorf("section %q: entry contains 'description' field — orient must use lean format", section)
			}
		}
	}
}

// TestOrient_LeanFormat_WhyMattersTruncated: a why_matters longer than 150 chars
// with no sentence boundary must be hard-cut at 150 chars + "..." and truncated:true.

// TestOrient_LeanFormat_WhyMattersTruncated: a why_matters longer than 150 chars
// with no sentence boundary must be hard-cut at 150 chars + "..." and truncated:true.
func TestOrient_LeanFormat_WhyMattersTruncated(t *testing.T) {
	_, h := newEnv(t)
	longWhy := strings.Repeat("x", 200)
	addNode(t, h, "Node long why", "orient-lean-trunc", map[string]any{
		"why_matters": longWhy,
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-trunc"})
	mustNotError(t, tr)

	var resp struct {
		Recent []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) == 0 {
		t.Fatal("recent section is empty")
	}
	entry := resp.Recent[0]
	const maxLen = 153 // 150 + len("...")
	if len(entry.WhyMatters) > maxLen {
		t.Errorf("why_matters: got %d chars, want at most %d", len(entry.WhyMatters), maxLen)
	}
	if !strings.HasSuffix(entry.WhyMatters, "...") {
		t.Errorf("why_matters must end with '...', got %q", entry.WhyMatters)
	}
	if !entry.Truncated {
		t.Error("truncated must be true when why_matters was hard-cut")
	}
}

// TestOrient_LeanFormat_SentenceBoundary: a why_matters with a sentence ending
// within the 150-char budget must be cut at the sentence boundary (no "..."),
// with truncated:true.

// TestOrient_LeanFormat_SentenceBoundary: a why_matters with a sentence ending
// within the 150-char budget must be cut at the sentence boundary (no "..."),
// with truncated:true.
func TestOrient_LeanFormat_SentenceBoundary(t *testing.T) {
	_, h := newEnv(t)
	// First sentence ends at ~30 chars; total is well over 150.
	why := "This is the short first sentence. " + strings.Repeat("more content ", 20)
	addNode(t, h, "Node sentence boundary", "orient-lean-sentence", map[string]any{
		"why_matters": why,
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-sentence"})
	mustNotError(t, tr)

	var resp struct {
		Recent []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Recent) == 0 {
		t.Fatal("recent section is empty")
	}
	entry := resp.Recent[0]
	if strings.HasSuffix(entry.WhyMatters, "...") {
		t.Errorf("sentence-boundary cut must not append '...', got %q", entry.WhyMatters)
	}
	if !strings.HasSuffix(entry.WhyMatters, ".") {
		t.Errorf("sentence-boundary cut must end with '.', got %q", entry.WhyMatters)
	}
	if !entry.Truncated {
		t.Error("truncated must be true when why_matters was cut at a sentence boundary")
	}
}

// TestOrient_LeanFormat_TruncatedFalseWhenFits: a short why_matters that fits
// within 150 chars must not include truncated:true in the orient entry.

// TestOrient_LeanFormat_TruncatedFalseWhenFits: a short why_matters that fits
// within 150 chars must not include truncated:true in the orient entry.
func TestOrient_LeanFormat_TruncatedFalseWhenFits(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node short why", "orient-lean-fits", map[string]any{
		"why_matters": "Short enough.",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-fits"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	recentRaw, ok := raw["recent"]
	if !ok {
		t.Fatal("recent section missing")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(recentRaw, &entries); err != nil {
		t.Fatalf("parse recent entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("recent section is empty")
	}
	if _, hasTruncated := entries[0]["truncated"]; hasTruncated {
		t.Error("truncated must be omitted when why_matters fits within budget")
	}
}

// TestOrient_LeanFormat_WhyMattersOmitted: a node with no why_matters must not
// include a why_matters key in the orient entry (omitempty, not empty string).

// TestOrient_LeanFormat_WhyMattersOmitted: a node with no why_matters must not
// include a why_matters key in the orient entry (omitempty, not empty string).
func TestOrient_LeanFormat_WhyMattersOmitted(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Node no why", "orient-lean-omit", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-lean-omit"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	recentRaw, ok := raw["recent"]
	if !ok {
		t.Fatal("recent section missing from orient response")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(recentRaw, &entries); err != nil {
		t.Fatalf("parse recent entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("recent section is empty")
	}
	if _, hasWhy := entries[0]["why_matters"]; hasWhy {
		t.Error("why_matters must be omitted when empty — lean format uses omitempty")
	}
}

// TestListTools_OrientDescriptionTruncationDisclosure: orient description must
// tell agents that full content requires recall(id), not orient alone.

// TestOrient_StaleCountZeroWhenNoDrift: orient must include stale_count = 0
// when no nodes match any drift rule.
func TestOrient_StaleCountZeroWhenNoDrift(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Fresh memory", "orient-stalecnt-zero", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-zero"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount != 0 {
		t.Errorf("stale_count: got %d, want 0", resp.StaleCount)
	}
}

// TestOrient_StaleCountNonZeroWhenTransientIsStale: orient stale_count must be
// > 0 when a transient node is older than 7 days.

// TestOrient_StaleCountNonZeroWhenTransientIsStale: orient stale_count must be
// > 0 when a transient node is older than 7 days.
func TestOrient_StaleCountNonZeroWhenTransientIsStale(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	addNode(t, h, "Old sprint ticket", "orient-stalecnt-transient", map[string]any{
		"node_kind": "transient",
	})

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE domain = ?`, old, "orient-stalecnt-transient"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-transient"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 for a stale transient node")
	}
}

// TestOrient_StaleCountNonZeroWhenStandingNodeContradicts: orient stale_count
// must be > 0 when a standing node is connected by a contradicts edge.

// TestOrient_StaleCountNonZeroWhenStandingNodeContradicts: orient stale_count
// must be > 0 when a standing node is connected by a contradicts edge.
func TestOrient_StaleCountNonZeroWhenStandingNodeContradicts(t *testing.T) {
	_, h := newEnv(t)
	aID := addNode(t, h, "Rule A contradicted", "orient-stalecnt-contradicts", map[string]any{
		"node_kind": "standing",
	})
	bID := addNode(t, h, "Rule B contradicts it", "orient-stalecnt-contradicts", map[string]any{
		"node_kind": "standing",
	})
	call(t, h, "connect", map[string]any{
		"from_memory":  aID,
		"to_memory":    bID,
		"relationship": "contradicts",
		"narrative":    "these rules conflict",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-contradicts"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 when a contradicts edge exists")
	}
}

// TestOrient_StaleCountNonZeroWhenLowConnectionStanding: orient stale_count
// must be > 0 for a standing node with < 2 inbound edges older than 30 days.

// TestOrient_StaleCountNonZeroWhenLowConnectionStanding: orient stale_count
// must be > 0 for a standing node with < 2 inbound edges older than 30 days.
func TestOrient_StaleCountNonZeroWhenLowConnectionStanding(t *testing.T) {
	dbPath, _, h := newEnvWithPath(t)

	addNode(t, h, "Lonely standing rule", "orient-stalecnt-standing", map[string]any{
		"node_kind": "standing",
	})

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer rawDB.Close()
	old := time.Now().UTC().AddDate(0, 0, -31).Format("2006-01-02T15:04:05Z")
	if _, err := rawDB.Exec(`UPDATE nodes SET created_at = ? WHERE domain = ?`, old, "orient-stalecnt-standing"); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	rawDB.Close()

	tr := call(t, h, "orient", map[string]any{"domain": "orient-stalecnt-standing"})
	mustNotError(t, tr)

	var resp struct {
		StaleCount int `json:"stale_count"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.StaleCount == 0 {
		t.Error("stale_count: got 0, want > 0 for a low-connection standing node older than 30 days")
	}
}

// TestOrient_DescriptionContainsStaleCountAdvisory: orient description must
// contain the stale_count advisory instructing agents to call audit(mode=stale).

// TestOrient_DescriptionContainsStaleCountAdvisory: orient description must
// contain the stale_count advisory instructing agents to call audit(mode=stale).
func TestOrient_DescriptionContainsStaleCountAdvisory(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "stale_count > 0") {
				t.Error(`orient description must contain "stale_count > 0"`)
			}
			if !strings.Contains(td.Description, "audit(mode=stale)") {
				t.Error(`orient description must contain "audit(mode=stale)"`)
			}
			return
		}
	}
	t.Error("orient tool not found in ListTools response")
}

// ── orient topic ──────────────────────────────────────────────────────────────

// TestOrient_Topic_ReturnsRelevantSection: orient with topic must return a
// relevant section and must not return significant.

// TestOrient_Topic_ReturnsRelevantSection: orient with topic must return a
// relevant section and must not return significant.
func TestOrient_Topic_ReturnsRelevantSection(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Authentication design decision", "orient-topic-dom", map[string]any{
		"description": "We chose JWT over sessions.",
		"why_matters": "Stateless tokens simplify horizontal scaling.",
	})

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-dom",
		"topic":  "authentication",
	})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["relevant"]; !ok {
		t.Error("orient with topic must return a relevant section")
	}
	if _, ok := raw["significant"]; ok {
		t.Error("orient with topic must not return significant — relevant replaces it")
	}
}

// TestOrient_Topic_RelevantIsLean: relevant entries must use lean format —
// no description field, why_matters present.

// TestOrient_Topic_RelevantIsLean: relevant entries must use lean format —
// no description field, why_matters present.
func TestOrient_Topic_RelevantIsLean(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Caching strategy", "orient-topic-lean", map[string]any{
		"description": "This must not appear in orient output.",
		"why_matters": "Cache hits reduce DB load.",
	})

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-lean",
		"topic":  "caching",
	})
	mustNotError(t, tr)

	var resp struct {
		Relevant []map[string]json.RawMessage `json:"relevant"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if len(resp.Relevant) == 0 {
		t.Fatal("relevant section is empty")
	}
	entry := resp.Relevant[0]
	if _, hasDesc := entry["description"]; hasDesc {
		t.Error("relevant entry must not contain description field — lean format")
	}
	if _, hasWhy := entry["why_matters"]; !hasWhy {
		t.Error("relevant entry must contain why_matters when node has one")
	}
}

// TestOrient_Topic_SpineAndRecentUnchanged: topic mode must still return
// declared_spine and recent unchanged.

// TestOrient_Topic_SpineAndRecentUnchanged: topic mode must still return
// declared_spine and recent unchanged.
func TestOrient_Topic_SpineAndRecentUnchanged(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "Database migration", "orient-topic-spine", map[string]any{
		"occurred_at": "2026-01-01",
		"why_matters": "Sets DB schema baseline.",
	})
	addNode(t, h, "Recent work item", "orient-topic-spine", nil)

	tr := call(t, h, "orient", map[string]any{
		"domain": "orient-topic-spine",
		"topic":  "migration",
	})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["declared_spine"]; !ok {
		t.Error("orient with topic must still return declared_spine")
	}
	if _, ok := raw["recent"]; !ok {
		t.Error("orient with topic must still return recent")
	}
}

// TestOrient_NoTopic_SignificantPresent: orient without topic must return
// significant and must not return relevant (no regression).

// TestOrient_NoTopic_SignificantPresent: orient without topic must return
// significant and must not return relevant (no regression).
func TestOrient_NoTopic_SignificantPresent(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Some node", "orient-notopic", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-notopic"})
	mustNotError(t, tr)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &raw); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := raw["significant"]; !ok {
		t.Error("orient without topic must return significant section")
	}
	if _, ok := raw["relevant"]; ok {
		t.Error("orient without topic must not return relevant section")
	}
}

// TestListTools_OrientDescriptionMentionsTopic: orient description must
// mention the topic parameter so agents know to use it.

// TestSummariseDomain_IncludesNodeIDs: each entry in recent must carry an "id"
// field so the agent can pass it directly to revise or connect without a second
// lookup. (The all_nodes dump was removed in the orient redesign; IDs are
// available via recent, significant, and declared_spine.)
func TestSummariseDomain_IncludesNodeIDs(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "ID check node alpha", "id-test-domain", map[string]any{
		"description": "first node",
		"why_matters": "verify id round-trips",
	})
	id2 := addNode(t, h, "ID check node beta", "id-test-domain", map[string]any{
		"description": "second node",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "id-test-domain"})
	mustNotError(t, tr)
	body := text(t, tr)

	// Parse the structured response — IDs must appear in recent.
	var resp struct {
		Recent []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse summarise_domain response: %v\nbody: %s", err, body)
	}

	// Every recent entry must have a non-empty ID.
	for _, n := range resp.Recent {
		if n.ID == "" {
			t.Errorf("recent entry %q has empty id in orient response", n.Label)
		}
	}

	// Both filed IDs must appear in recent (freshly filed, no edges).
	var gotIDs []string
	for _, n := range resp.Recent {
		gotIDs = append(gotIDs, n.ID)
	}
	if !contains(gotIDs, id1) {
		t.Errorf("id1 (%s) not found in orient recent; got %v", id1, gotIDs)
	}
	if !contains(gotIDs, id2) {
		t.Errorf("id2 (%s) not found in orient recent; got %v", id2, gotIDs)
	}
}

// ── add_node transient + drift of transient ───────────────────────────────────

// TestOrient_NoDomain_ReturnsCrossDomainSnapshot: calling orient with no domain
// must return mode="cross_domain_snapshot" with a domains array containing at
// least one entry that has domain and recent fields.
func TestOrient_NoDomain_ReturnsCrossDomainSnapshot(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Alpha", "domain-a", nil)
	addNode(t, h, "Beta", "domain-b", nil)

	tr := call(t, h, "orient", map[string]any{})
	mustNotError(t, tr)

	var resp struct {
		Mode    string `json:"mode"`
		Domains []struct {
			Domain string        `json:"domain"`
			Recent []interface{} `json:"recent"`
		} `json:"domains"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse cross-domain snapshot: %v", err)
	}
	if resp.Mode != "cross_domain_snapshot" {
		t.Errorf("expected mode=cross_domain_snapshot; got %q", resp.Mode)
	}
	if len(resp.Domains) == 0 {
		t.Fatal("expected at least one domain in snapshot; got none")
	}
	for _, d := range resp.Domains {
		if d.Domain == "" {
			t.Error("domain entry has empty domain field")
		}
		if d.Recent == nil {
			t.Errorf("domain %q has nil recent array", d.Domain)
		}
	}
}

// TestOrient_WithDomain_Unchanged: orient with a domain must still return the
// three-section response (declared_spine, significant, recent) unchanged.

// TestOrient_WithDomain_Unchanged: orient with a domain must still return the
// three-section response (declared_spine, significant, recent) unchanged.
func TestOrient_WithDomain_Unchanged(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "Existing node", "orient-regression-domain", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "orient-regression-domain"})
	mustNotError(t, tr)

	var resp struct {
		DeclaredSpine interface{} `json:"declared_spine"`
		Significant   interface{} `json:"significant"`
		Recent        interface{} `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if resp.DeclaredSpine == nil {
		t.Error("orient with domain missing declared_spine")
	}
	if resp.Significant == nil {
		t.Error("orient with domain missing significant")
	}
	if resp.Recent == nil {
		t.Error("orient with domain missing recent")
	}
}

// TestListTools_RememberDescriptionContainsDomainInference: remember description
// must instruct agents to infer domain from search results and prefer existing
// domains over creating new ones.

// TestOrient_DescriptionContainsCausalSequenceConstraint: orient description must
// prohibit answering causal/chronological-sequence questions from orient alone.
func TestOrient_DescriptionContainsCausalSequenceConstraint(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "causal or chronological sequence") {
				t.Error("orient description missing prohibition on answering causal/chronological-sequence questions from orient alone")
			}
			return
		}
	}
	t.Fatal("orient tool not found in ListTools")
}

// TestOrient_DescriptionContainsHistoryFallback: orient description must direct
// agents to history(important_only=true) for sequence-dependent questions.

// TestOrient_DescriptionContainsHistoryFallback: orient description must direct
// agents to history(important_only=true) for sequence-dependent questions.
func TestOrient_DescriptionContainsHistoryFallback(t *testing.T) {
	_, h := newEnv(t)
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	b, _ := json.Marshal(raw)
	var resp struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	for _, td := range resp.Tools {
		if td.Name == "orient" {
			if !strings.Contains(td.Description, "history(important_only=true)") {
				t.Error("orient description must reference history(important_only=true) as the required tool for sequence-dependent questions")
			}
			return
		}
	}
	t.Fatal("orient tool not found in ListTools")
}

// ── significance: memory_id mode ─────────────────────────────────────────────

// TestSignificance_MemoryIDMode_ReturnsAllFourSections: calling significance
// with a memory_id must return all four sections without error.

func TestGetStandingNodes_UsesNodeKind(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "pre-existing standing rule", "proj", map[string]any{
		"node_kind":   "standing",
		"why_matters": "governs deployments",
	})

	tr := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	var rules []any
	if err := json.Unmarshal(resp["rules"], &rules); err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("standing node filed via node_kind should appear in orient rules section")
	}
}

func TestOrient_RulesSection_StandingNodes(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "rule alpha", "proj", map[string]any{"node_kind": "standing", "why_matters": "governs deployments"})
	addNode(t, h, "rule beta", "proj", map[string]any{"node_kind": "standing", "why_matters": "governs testing"})
	addNode(t, h, "plain decision", "proj", map[string]any{})

	tr := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	rawRules, ok := resp["rules"]
	if !ok {
		t.Fatal("orient response missing 'rules' key")
	}
	var rules []any
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("rules section should be non-empty")
	}
}

func TestOrient_RulesSection_Absent_WhenNoStanding(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "just a decision", "proj", map[string]any{})
	addNode(t, h, "another decision", "proj", map[string]any{})

	tr := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	if _, ok := resp["rules"]; ok {
		t.Error("orient response should NOT contain 'rules' key when no standing nodes exist")
	}
}

func TestOrient_RulesSection_OrderedByInboundEdgeCount(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	idA := addNode(t, h, "rule with no refs", "proj", map[string]any{"node_kind": "standing", "why_matters": "a"})
	idB := addNode(t, h, "rule with one ref", "proj", map[string]any{"node_kind": "standing", "why_matters": "b"})
	idLinker := addNode(t, h, "work item", "proj", map[string]any{})

	tr := call(t, h, "connect", map[string]any{
		"from_memory":  idLinker,
		"to_memory":    idB,
		"relationship": "governed_by",
		"narrative":    "governed by rule B",
	})
	mustNotError(t, tr)

	tr2 := call(t, h, "orient", map[string]any{"domain": "proj"})
	mustNotError(t, tr2)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr2)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	rawRules, ok := resp["rules"]
	if !ok {
		t.Fatal("orient response missing 'rules' key")
	}
	var rules []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rawRules, &rules); err != nil {
		t.Fatalf("parse rules: %v", err)
	}
	if len(rules) < 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != idB {
		t.Errorf("rules[0]: want idB (%q, 1 inbound), got %q", idB, rules[0].ID)
	}
	if rules[1].ID != idA {
		t.Errorf("rules[1]: want idA (%q, 0 inbound), got %q", idA, rules[1].ID)
	}
}
