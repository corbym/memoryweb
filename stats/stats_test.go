package stats_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/stats"
)

func call(r *stats.Recorder, tool string, args map[string]any, result string, isError bool) {
	raw, _ := json.Marshal(args)
	r.Record(tool, raw, result, isError)
}

func TestWKD_FilingWithConnections(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"))

	// Orient at start (retrieval)
	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":10}`, false)
	// File two nodes
	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a-1234"}}`, false)
	call(r, "remember", map[string]any{"label": "B", "domain": "proj"}, `{"node":{"id":"b-5678"}}`, false)
	// Connect them — retrieval followed by write
	call(r, "connect", map[string]any{"from_node": "a-1234", "to_node": "b-5678", "relationship": "led_to"}, `{"id":"edge-abc"}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// WKD = (1 connected × 2) + (1 edge × 1.5) - (1 orphan × 1) + ratio×10
	// oriented → ratio should be > 0
	if !strings.Contains(summary, "WKD") {
		t.Error("summary should contain WKD score")
	}
	if strings.Contains(summary, "D-") {
		t.Error("connected filing session should not score D-")
	}
}

func TestWKD_PureRetrieval(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"))

	for i := 0; i < 6; i++ {
		call(r, "search", map[string]any{"query": "something"}, `{"nodes":[]}`, false)
	}
	call(r, "recall", map[string]any{"id": "abc"}, `{"node":{"id":"abc"}}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "retrieval") {
		t.Errorf("pure retrieval session should be labelled as retrieval; got:\n%s", summary)
	}
}

func TestWKD_OrphansArePenalised(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"))

	// File 4 nodes, connect none
	for i := 0; i < 4; i++ {
		call(r, "remember", map[string]any{"label": "node", "domain": "proj"}, `{"node":{"id":"x"}}`, false)
	}

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "Orphan") {
		t.Errorf("orphaned-node session should mention Orphan; got:\n%s", summary)
	}
}

func TestWKD_TransientPenalty(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"))

	call(r, "remember", map[string]any{"label": "sprint note", "domain": "proj", "transient": true}, `{"node":{"id":"t"}}`, false)
	call(r, "remember", map[string]any{"label": "sprint note 2", "domain": "proj", "transient": true}, `{"node":{"id":"t2"}}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "transient") {
		t.Errorf("transient nodes should be flagged; got:\n%s", summary)
	}
}

func TestGrade_Boundaries(t *testing.T) {
	cases := []struct {
		wkd   float64
		grade string
	}{
		{30, "A"},
		{20, "B+"},
		{10, "B"},
		{3, "C"},
		{1, "D"},
		{-2, "D-"},
	}
	for _, tc := range cases {
		got := stats.WKDGrade(tc.wkd)
		if got != tc.grade {
			t.Errorf("WKDGrade(%.0f) = %q, want %q", tc.wkd, got, tc.grade)
		}
	}
}

func TestFlush_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.log")
	r := stats.New(path)

	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":5}`, false)
	call(r, "remember", map[string]any{"label": "Decision X", "domain": "proj"}, `{"node":{"id":"d-1"}}`, false)
	call(r, "connect", map[string]any{"from_node": "d-1", "to_node": "other", "relationship": "led_to"}, `{"id":"e-1"}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// File must exist
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stats file not created: %v", err)
	}

	// Must contain machine-readable data line and human summary
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "<!-- data:") {
		t.Error("stats file should contain data line")
	}
	if !strings.Contains(string(content), "memoryweb session") {
		t.Error("stats file should contain session header")
	}

	// Return value should be the summary
	if !strings.Contains(summary, "memoryweb session") {
		t.Errorf("Flush should return summary text; got: %s", summary)
	}
}

func TestFlush_CumulativeTrend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.log")

	// Write two prior sessions manually.
	prior := `<!-- data: {"start_ts":"2026-04-28T10:00:00Z","wkd":12.0,"type":"filing","nodes":3,"edges":2,"orphans":1,"transient":0,"ratio":0.60,"burst":false} -->
=== memoryweb session — 2026-04-28 10:00 UTC ===
prior session content
=== end ===

<!-- data: {"start_ts":"2026-04-29T10:00:00Z","wkd":16.5,"type":"filing","nodes":4,"edges":4,"orphans":0,"transient":0,"ratio":0.75,"burst":false} -->
=== memoryweb session — 2026-04-29 10:00 UTC ===
prior session content
=== end ===
`
	os.WriteFile(path, []byte(prior), 0644)

	r := stats.New(path)
	call(r, "remember", map[string]any{"label": "N", "domain": "proj"}, `{"node":{"id":"n"}}`, false)
	call(r, "connect", map[string]any{"from_node": "n", "to_node": "x", "relationship": "led_to"}, `{"id":"e"}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "30-day trend") {
		t.Errorf("summary with prior sessions should include 30-day trend; got:\n%s", summary)
	}
	if !strings.Contains(summary, "Sessions") {
		t.Errorf("trend should show session count; got:\n%s", summary)
	}
}

