package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/corbym/memoryweb/tools"
)

func TestSearch_LeanFormat_NoDescription(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "lean search node", "search-lean", map[string]any{
		"description": "this description must not appear in lean search output",
	})

	tr := call(t, h, "search", map[string]any{"query": "lean search", "domain": "search-lean"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	for _, n := range resp.Nodes {
		if _, ok := n["description"]; ok {
			t.Error("search result must not contain 'description' field — lean format required")
		}
	}
}

func TestSearch_LeanFormat_WhyMattersTruncated(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	longWhy := strings.Repeat("x", 200)
	addNode(t, h, "lean search trunc node", "search-lean-trunc", map[string]any{
		"why_matters": longWhy,
	})

	tr := call(t, h, "search", map[string]any{"query": "lean search trunc", "domain": "search-lean-trunc"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	const maxLen = 153 // 150 + len("...")
	if len(resp.Nodes[0].WhyMatters) > maxLen {
		t.Errorf("why_matters: got %d chars, want at most %d", len(resp.Nodes[0].WhyMatters), maxLen)
	}
	if !resp.Nodes[0].Truncated {
		t.Error("truncated must be true when why_matters was cut")
	}
}

func TestSearch_LeanFormat_EdgesOmitNarrative(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	from := addNode(t, h, "lean edge source", "search-lean-edges", nil)
	to := addNode(t, h, "lean edge target", "search-lean-edges", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": from, "to_memory": to,
		"relationship": "connects_to", "narrative": "this narrative must not appear in lean search output",
	}))

	tr := call(t, h, "search", map[string]any{"query": "lean edge", "domain": "search-lean-edges"})
	mustNotError(t, tr)

	var resp struct {
		Edges []map[string]json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Edges) == 0 {
		t.Fatal("expected at least one edge between the two matched nodes")
	}
	for _, e := range resp.Edges {
		if _, ok := e["narrative"]; ok {
			t.Error("search edge must not contain 'narrative' field — lean format required")
		}
		if _, ok := e["from_memory"]; !ok {
			t.Error("search edge missing 'from_memory' field")
		}
		if _, ok := e["to_memory"]; !ok {
			t.Error("search edge missing 'to_memory' field")
		}
		if _, ok := e["relationship"]; !ok {
			t.Error("search edge missing 'relationship' field")
		}
	}
}

func TestRecent_LeanFormat_NoDescription(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "lean recent node", "recent-lean", map[string]any{
		"description": "this description must not appear in lean recent output",
	})

	tr := call(t, h, "history", map[string]any{"domain": "recent-lean", "order": "modified"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recent response: %v", err)
	}
	nodes := resp.Nodes
	if len(nodes) == 0 {
		t.Fatal("expected at least one recent result")
	}
	for _, n := range nodes {
		if _, ok := n["description"]; ok {
			t.Error("recent result must not contain 'description' field — lean format required")
		}
	}
}

func TestSignificance_LeanFormat_NoDescription(t *testing.T) {
	_, h := newEnv(t)
	// Declared + potentially_stale: occurred_at set, no inbound edges.
	addNode(t, h, "lean sig declared node", "sig-lean", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z",
		"description": "declared description must not appear",
		"why_matters": "declared",
	})
	// Structural + uncurated: no occurred_at, has an inbound edge.
	structural := addNode(t, h, "lean sig structural node", "sig-lean", map[string]any{
		"description": "structural description must not appear",
	})
	linker := addNode(t, h, "lean sig linker", "sig-lean", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": linker, "to_memory": structural,
		"relationship": "connects_to", "narrative": "links",
	}))

	tr := call(t, h, "significance", map[string]any{"domain": "sig-lean"})
	mustNotError(t, tr)

	var resp struct {
		Declared         []map[string]json.RawMessage `json:"declared"`
		Structural       []map[string]json.RawMessage `json:"structural"`
		Uncurated        []map[string]json.RawMessage `json:"uncurated"`
		PotentiallyStale []map[string]json.RawMessage `json:"potentially_stale"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse significance response: %v", err)
	}
	sections := map[string][]map[string]json.RawMessage{
		"declared":          resp.Declared,
		"structural":        resp.Structural,
		"uncurated":         resp.Uncurated,
		"potentially_stale": resp.PotentiallyStale,
	}
	foundAny := false
	for name, entries := range sections {
		if len(entries) > 0 {
			foundAny = true
		}
		for _, n := range entries {
			if _, ok := n["description"]; ok {
				t.Errorf("significance section %q: entry contains 'description' field — lean format required", name)
			}
		}
	}
	if !foundAny {
		t.Fatal("expected at least one entry across the four significance sections")
	}
}

func TestHistory_LeanFormat_NoDescription(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "lean history node", "history-lean", map[string]any{
		"description": "this description must not appear in lean history output",
	})

	tr := call(t, h, "history", map[string]any{"domain": "history-lean"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse history response: %v", err)
	}
	nodes := resp.Nodes
	if len(nodes) == 0 {
		t.Fatal("expected at least one history result")
	}
	for _, n := range nodes {
		if _, ok := n["description"]; ok {
			t.Error("history result must not contain 'description' field — lean format required")
		}
	}
}

func descriptionFor(t *testing.T, h *tools.Handler, toolName string) string {
	t.Helper()
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
		if td.Name == toolName {
			return td.Description
		}
	}
	t.Fatalf("tool %q not found in ListTools", toolName)
	return ""
}

// TestRecall_ReturnsFullLabelUntruncated: recall must return the full stored
// label even when it exceeds 120 chars (the lean output budget).
func TestRecall_ReturnsFullLabelUntruncated(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	longLabel := "recall-full-label " + strings.Repeat("x", 110) // 128 runes
	id := addNode(t, h, longLabel, "recall-full", nil)

	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)

	var resp struct {
		Node struct {
			Label string `json:"label"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse recall response: %v", err)
	}
	if resp.Node.Label != longLabel {
		t.Errorf("recall must return full label: got %q (len %d), want %q (len %d)",
			resp.Node.Label, utf8.RuneCountInString(resp.Node.Label),
			longLabel, utf8.RuneCountInString(longLabel))
	}
}

func TestListTools_SearchDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "search"), "recall(id)") {
		t.Error(`search description must contain "recall(id)" — truncation disclosure is missing`)
	}
}

func TestListTools_RecentDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "history"), "recall(id)") {
		t.Error(`recent description must contain "recall(id)" — truncation disclosure is missing`)
	}
}

func TestListTools_SignificanceDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "significance"), "recall(id)") {
		t.Error(`significance description must contain "recall(id)" — truncation disclosure is missing`)
	}
}

func TestListTools_HistoryDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "history"), "recall(id)") {
		t.Error(`history description must contain "recall(id)" — truncation disclosure is missing`)
	}
}

func TestSearch_LeanFormat_LifecycleStateResolved(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	issue := addNode(t, h, "lifecycle resolved issue", "lc-lean", map[string]any{
		"node_kind":   "issue",
		"why_matters": "open work item",
	})
	fix := addNode(t, h, "lifecycle fix shipped", "lc-lean", nil)
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": issue, "to_memory": fix,
		"relationship": "resolved", "narrative": "shipped the fix",
	}))

	tr := call(t, h, "search", map[string]any{"query": "lifecycle resolved issue", "domain": "lc-lean"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID             string `json:"id"`
			LifecycleState string `json:"lifecycle_state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	for _, n := range resp.Nodes {
		if n.ID != issue {
			continue
		}
		if n.LifecycleState != "resolved" {
			t.Errorf("issue node lifecycle_state: got %q, want resolved", n.LifecycleState)
		}
		return
	}
	t.Fatalf("issue node %q not found in search results", issue)
}

func TestSearch_LeanFormat_LifecycleStateContested(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	a := addNode(t, h, "lifecycle claim alpha", "lc-contested", map[string]any{"why_matters": "first claim"})
	b := addNode(t, h, "lifecycle claim beta", "lc-contested", map[string]any{"why_matters": "conflicting claim"})
	mustNotError(t, call(t, h, "connect", map[string]any{
		"from_memory": a, "to_memory": b,
		"relationship": "contradicts", "narrative": "direct conflict",
	}))

	tr := call(t, h, "search", map[string]any{"query": "lifecycle claim", "domain": "lc-contested"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID             string `json:"id"`
			LifecycleState string `json:"lifecycle_state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) < 2 {
		t.Fatalf("expected both contested nodes in search results, got %d", len(resp.Nodes))
	}
	for _, n := range resp.Nodes {
		if n.LifecycleState != "contested" {
			t.Errorf("node %q lifecycle_state: got %q, want contested", n.ID, n.LifecycleState)
		}
	}
}

func TestSearch_LeanFormat_OrdinaryDecisionNoLifecycleState(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "lifecycle ordinary decision", "lc-plain", map[string]any{
		"node_kind":   "decision",
		"why_matters": "routine decision",
	})

	tr := call(t, h, "search", map[string]any{"query": "lifecycle ordinary", "domain": "lc-plain"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			ID             string `json:"id"`
			LifecycleState string `json:"lifecycle_state"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	for _, n := range resp.Nodes {
		if n.ID == id && n.LifecycleState != "" {
			t.Errorf("ordinary decision must omit lifecycle_state; got %q", n.LifecycleState)
		}
	}
}

// TestSearch_LeanFormat_WhyMattersMultiByteTruncated: truncation of a long
// multi-byte (emoji) why_matters must cut at a rune boundary and never leave
// invalid or replacement-character-bearing UTF-8.
func TestSearch_LeanFormat_WhyMattersMultiByteTruncated(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	longWhy := strings.Repeat("😀", 200)
	addNode(t, h, "cjk trunc node", "search-lean-mb", map[string]any{
		"why_matters": longWhy,
	})

	tr := call(t, h, "search", map[string]any{"query": "cjk trunc", "domain": "search-lean-mb"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	why := resp.Nodes[0].WhyMatters
	if !utf8.ValidString(why) {
		t.Errorf("why_matters must remain valid UTF-8 after truncation; got invalid bytes: %q", why)
	}
	if strings.ContainsRune(why, '\ufffd') {
		t.Errorf("why_matters must not contain U+FFFD replacement characters (byte-level split); got: %q", why)
	}
	if !resp.Nodes[0].Truncated {
		t.Error("truncated must be true when why_matters was cut")
	}
	const minRunes = 140 // enough runes that the 150-rune cut (not a byte cut) is provable
	if n := utf8.RuneCountInString(why); n < minRunes {
		t.Errorf("why_matters: got %d runes — indicates a byte-level cut, want >= %d", n, minRunes)
	}
	if !strings.HasSuffix(why, "...") {
		t.Errorf("hard-cut why_matters should end with '...'; got: %q", why)
	}
}

// TestSearch_LeanFormat_LabelTruncated: a label > 120 runes must be cut to 120
// runes + "..." and set truncated=true.
func TestSearch_LeanFormat_LabelTruncated(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	longLabel := "label-trunc-search " + strings.Repeat("x", 110) // 129 runes total
	addNode(t, h, longLabel, "label-trunc-search", nil)

	tr := call(t, h, "search", map[string]any{"query": "label-trunc-search", "domain": "label-trunc-search"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Label     string `json:"label"`
			Truncated bool   `json:"truncated"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	const maxRunes = 123 // 120 + len("...")
	if n := utf8.RuneCountInString(resp.Nodes[0].Label); n > maxRunes {
		t.Errorf("label: got %d runes, want at most %d", n, maxRunes)
	}
	if !strings.HasSuffix(resp.Nodes[0].Label, "...") {
		t.Errorf("truncated label must end with '...'; got %q", resp.Nodes[0].Label)
	}
	if !resp.Nodes[0].Truncated {
		t.Error("truncated must be true when label was cut")
	}
}

// TestSearch_LeanFormat_LabelAtLimit: a label of exactly 120 runes must pass
// through intact and not set truncated.
func TestSearch_LeanFormat_LabelAtLimit(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	exactLabel := strings.Repeat("a", 120)
	addNode(t, h, exactLabel, "label-at-limit", nil)

	tr := call(t, h, "search", map[string]any{"query": exactLabel[:20], "domain": "label-at-limit"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			Label     string `json:"label"`
			Truncated bool   `json:"truncated"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	if resp.Nodes[0].Label != exactLabel {
		t.Errorf("label at limit must pass through intact; got %q (len %d runes)", resp.Nodes[0].Label, utf8.RuneCountInString(resp.Nodes[0].Label))
	}
	if resp.Nodes[0].Truncated {
		t.Error("truncated must be false when label is exactly 120 runes")
	}
}

// TestSearch_LeanFormat_WhyMattersMultiByteUnderLimit: a multi-byte why_matters
// that is long in bytes but within the 150-rune budget must pass through intact
// and untruncated.
func TestSearch_LeanFormat_WhyMattersMultiByteUnderLimit(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	shortWhy := strings.Repeat("😀", 60) // 240 bytes but 60 runes → inside the rune budget
	addNode(t, h, "cjk short node", "search-lean-mb-short", map[string]any{
		"why_matters": shortWhy,
	})

	tr := call(t, h, "search", map[string]any{"query": "cjk short", "domain": "search-lean-mb-short"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []struct {
			WhyMatters string `json:"why_matters"`
			Truncated  bool   `json:"truncated"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("expected at least one search result")
	}
	if !utf8.ValidString(resp.Nodes[0].WhyMatters) {
		t.Errorf("why_matters must remain valid UTF-8; got invalid bytes: %q", resp.Nodes[0].WhyMatters)
	}
	if resp.Nodes[0].Truncated {
		t.Errorf("truncated must be false when why_matters fits within 150 runes; got: %q", resp.Nodes[0].WhyMatters)
	}
	if resp.Nodes[0].WhyMatters != shortWhy {
		t.Error("multi-byte why_matters within the rune budget must pass through intact")
	}
}
