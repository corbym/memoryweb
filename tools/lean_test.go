package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

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

	tr := call(t, h, "search", map[string]any{"query": "", "domain": "search-lean-edges"})
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

	tr := call(t, h, "recent", map[string]any{"domain": "recent-lean"})
	mustNotError(t, tr)

	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("parse recent response: %v", err)
	}
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

	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text(t, tr)), &nodes); err != nil {
		t.Fatalf("parse history response: %v", err)
	}
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

func TestListTools_SearchDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "search"), "recall(id)") {
		t.Error(`search description must contain "recall(id)" — truncation disclosure is missing`)
	}
}

func TestListTools_RecentDescriptionTruncationDisclosure(t *testing.T) {
	_, h := newEnv(t)
	if !strings.Contains(descriptionFor(t, h, "recent"), "recall(id)") {
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
