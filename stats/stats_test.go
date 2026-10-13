package stats_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	r := stats.New(filepath.Join(dir, "stats.log"), filepath.Join(dir, "stats.jsonl"))

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
	r := stats.New(filepath.Join(dir, "stats.log"), "")

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
	r := stats.New(filepath.Join(dir, "stats.log"), "")

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
	r := stats.New(filepath.Join(dir, "stats.log"), "")

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
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(path, jsonPath)

	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":5}`, false)
	call(r, "remember", map[string]any{"label": "Decision X", "domain": "proj"}, `{"node":{"id":"d-1"}}`, false)
	call(r, "connect", map[string]any{"from_node": "d-1", "to_node": "other", "relationship": "led_to"}, `{"id":"e-1"}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Human file must exist without data line
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stats file not created: %v", err)
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "<!-- data:") {
		t.Error("human stats file should NOT contain <!-- data: --> line")
	}
	if !strings.Contains(string(content), "memoryweb session") {
		t.Error("stats file should contain session header")
	}

	// JSON file must exist and contain valid JSON
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("json stats file not created: %v", err)
	}
	jsonContent, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonContent), &sd); err != nil {
		t.Errorf("json stats file should contain valid JSON: %v\ncontent: %s", err, jsonContent)
	}

	// Return value should be the summary
	if !strings.Contains(summary, "memoryweb session") {
		t.Errorf("Flush should return summary text; got: %s", summary)
	}
}

func TestFlush_CumulativeTrend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.log")
	jsonPath := filepath.Join(dir, "stats.jsonl")

	// Write two prior sessions as JSONL.
	prior := `{"start_ts":"2026-04-28T10:00:00Z","wkd":12.0,"type":"filing","nodes":3,"edges":2,"orphans":1,"transient":0,"ratio":0.60,"burst":false}
{"start_ts":"2026-04-29T10:00:00Z","wkd":16.5,"type":"filing","nodes":4,"edges":4,"orphans":0,"transient":0,"ratio":0.75,"burst":false}
`
	os.WriteFile(jsonPath, []byte(prior), 0644)

	r := stats.New(path, jsonPath)
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

func TestRecord_WhatsStale_ParsesCandidates(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"), filepath.Join(dir, "stats.jsonl"))

	// whats_stale result: 1 duplicate (3 edges), 1 transient (0 edges), 1 contradicts (7 edges)
	result := `[
		{"node":{"id":"a"},"reason":"possible duplicate of newer node","edge_count":3},
		{"node":{"id":"b"},"reason":"transient node older than 7 days — consider archiving once the related work is complete","edge_count":0},
		{"node":{"id":"c"},"reason":"explicitly marked as contradicting each other","edge_count":7}
	]`
	call(r, "whats_stale", map[string]any{"domain": "proj"}, result, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "Stale checks") {
		t.Errorf("summary should contain Stale checks line; got:\n%s", summary)
	}
	if !strings.Contains(summary, "3 candidate") {
		t.Errorf("summary should report 3 candidates; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1 duplicate") {
		t.Errorf("summary should break down type 'duplicate'; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1 transient") {
		t.Errorf("summary should break down type 'transient'; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1 contradicts") {
		t.Errorf("summary should break down type 'contradicts'; got:\n%s", summary)
	}
	// dup edge line: 1 duplicate with 3 edges → bucket 3-5 → "1x(3-5)"
	if !strings.Contains(summary, "Dup edges") {
		t.Errorf("summary should contain Dup edges line; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1x(3-5)") {
		t.Errorf("summary should show dup candidate with 3-5 edges; got:\n%s", summary)
	}

	// Check JSONL has stale fields
	jsonContent, _ := os.ReadFile(filepath.Join(dir, "stats.jsonl"))
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonContent), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if sc, ok := sd["stale_checks"].(float64); !ok || int(sc) != 1 {
		t.Errorf("stale_checks should be 1, got %v", sd["stale_checks"])
	}
	if sc, ok := sd["stale_candidates"].(float64); !ok || int(sc) != 3 {
		t.Errorf("stale_candidates should be 3, got %v", sd["stale_candidates"])
	}
	// dup_edge_3_5 = 1 (the duplicate candidate with 3 edges)
	if v, ok := sd["dup_edge_3_5"].(float64); !ok || int(v) != 1 {
		t.Errorf("dup_edge_3_5 should be 1, got %v", sd["dup_edge_3_5"])
	}
}

// TestFlush_Idempotent verifies that calling Flush twice writes exactly one JSONL row
// (reproduces defect 1: double-flush via signal handler + defer).
func TestFlush_Idempotent(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":5}`, false)
	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a"}}`, false)

	r.Flush() //nolint:errcheck
	r.Flush() //nolint:errcheck // second call must be a no-op

	data, _ := os.ReadFile(jsonPath)
	var rows []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) != "" {
			rows = append(rows, line)
		}
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 JSONL row after two Flush calls, got %d:\n%s", len(rows), string(data))
	}
}

// TestFlush_NoNewCalls_WritesNothing verifies that a second Flush with no new calls
// after the first is a no-op (reproduces defect 2: identical re-reports).
func TestFlush_NoNewCalls_WritesNothing(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a"}}`, false)
	r.Flush() //nolint:errcheck

	before, _ := os.ReadFile(jsonPath)
	r.Flush() //nolint:errcheck
	after, _ := os.ReadFile(jsonPath)

	if string(before) != string(after) {
		t.Error("second Flush with no new calls must not append to the JSONL file")
	}
}

// TestRecorder_LongLivedProcessThreeSessions simulates a single process serving
// three MCP sessions via FlushAndReset. It must produce exactly three JSONL rows,
// each with only that session's node count (reproduces defect 1 cumulative variant).
func TestRecorder_LongLivedProcessThreeSessions(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	// Session 1: 2 nodes
	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a"}}`, false)
	call(r, "remember", map[string]any{"label": "B", "domain": "proj"}, `{"node":{"id":"b"}}`, false)
	r.FlushAndReset() //nolint:errcheck

	// Session 2: 3 nodes
	call(r, "remember", map[string]any{"label": "C", "domain": "proj"}, `{"node":{"id":"c"}}`, false)
	call(r, "remember", map[string]any{"label": "D", "domain": "proj"}, `{"node":{"id":"d"}}`, false)
	call(r, "remember", map[string]any{"label": "E", "domain": "proj"}, `{"node":{"id":"e"}}`, false)
	r.FlushAndReset() //nolint:errcheck

	// Session 3: 1 node
	call(r, "remember", map[string]any{"label": "F", "domain": "proj"}, `{"node":{"id":"f"}}`, false)
	r.Flush() //nolint:errcheck

	data, _ := os.ReadFile(jsonPath)
	var sessions []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var s map[string]any
		if err := json.Unmarshal([]byte(line), &s); err == nil {
			sessions = append(sessions, s)
		}
	}

	if len(sessions) != 3 {
		t.Fatalf("expected 3 JSONL rows for 3 sessions, got %d:\n%s", len(sessions), string(data))
	}

	// Each session must have a distinct session_id.
	ids := map[any]bool{}
	for _, s := range sessions {
		ids[s["session_id"]] = true
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 unique session_ids, got %d: %v", len(ids), ids)
	}

	// Node counts must be per-session, not cumulative.
	want := []int{2, 3, 1}
	for i, s := range sessions {
		got := int(s["nodes"].(float64))
		if got != want[i] {
			t.Errorf("session %d: expected %d nodes, got %d", i+1, want[i], got)
		}
	}
}

// TestSessionType_ReviseNotRetrieval verifies that a session containing a revise call
// is not classified as retrieval-only (reproduces defect 5).
func TestSessionType_ReviseNotRetrieval(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"), "")

	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":10}`, false)
	call(r, "orient", map[string]any{"domain": "proj"}, `{"total_nodes":10}`, false)
	call(r, "revise", map[string]any{"id": "x", "description": "updated"}, `{"node":{"id":"x"}}`, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if strings.Contains(summary, "high-value retrieval") {
		t.Errorf("session containing revise must not be classified as retrieval; got:\n%s", summary)
	}
}

// TestTrend_DeduplicatesSessionID verifies that duplicate JSONL rows for the same
// session_id are collapsed to one when computing the 30-day trend.
func TestTrend_DeduplicatesSessionID(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")

	// session "aabbccdd" appears three times (9→10→20 nodes); "eeff0011" once.
	dupe := strings.Join([]string{
		`{"session_id":"aabbccdd","start_ts":"2026-10-05T10:00:00Z","wkd":3.0,"type":"filing","nodes":9,"edges":2,"orphans":7,"transient":0,"ratio":0.0,"burst":false}`,
		`{"session_id":"aabbccdd","start_ts":"2026-10-05T10:00:00Z","wkd":8.0,"type":"filing","nodes":10,"edges":3,"orphans":7,"transient":0,"ratio":0.1,"burst":false}`,
		`{"session_id":"aabbccdd","start_ts":"2026-10-05T10:00:00Z","wkd":12.0,"type":"filing","nodes":20,"edges":8,"orphans":12,"transient":0,"ratio":0.3,"burst":false}`,
		`{"session_id":"eeff0011","start_ts":"2026-10-06T10:00:00Z","wkd":15.0,"type":"filing","nodes":5,"edges":5,"orphans":0,"transient":0,"ratio":0.5,"burst":false}`,
	}, "\n") + "\n"
	os.WriteFile(jsonPath, []byte(dupe), 0644)

	r := stats.New(filepath.Join(dir, "stats2.log"), jsonPath)
	call(r, "remember", map[string]any{"label": "N", "domain": "proj"}, `{"node":{"id":"n"}}`, false)
	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Deduplicated prior = 2 sessions; plus current = 3 total.
	if !strings.Contains(summary, "3 total") {
		t.Errorf("trend must show 3 total sessions after deduplication; got:\n%s", summary)
	}
}

// ── v1.59 counting tests ──────────────────────────────────────────────────────

// TestBatchRememberCorrectCounting verifies that a batch remember with related_to
// on each item counts nodes, inline edges, and orphans correctly.
func TestBatchRememberCorrectCounting(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	batchArgs := map[string]any{
		"items": []map[string]any{
			{"label": "A", "domain": "proj", "related_to": []string{"existing-1"}},
			{"label": "B", "domain": "proj", "related_to": []string{"existing-2"}},
			{"label": "C", "domain": "proj", "related_to": []string{"existing-3"}},
		},
	}
	batchResult := `{"nodes":[
		{"node":{"id":"a-001"},"skipped_connections":[]},
		{"node":{"id":"b-002"},"skipped_connections":[]},
		{"node":{"id":"c-003"},"skipped_connections":[]}
	]}`
	call(r, "remember", batchArgs, batchResult, false)

	_, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if v := int(sd["nodes"].(float64)); v != 3 {
		t.Errorf("nodes = %d, want 3", v)
	}
	if v := int(sd["edges"].(float64)); v != 3 {
		t.Errorf("edges = %d, want 3", v)
	}
	if v := int(sd["edges_inline"].(float64)); v != 3 {
		t.Errorf("edges_inline = %d, want 3", v)
	}
	if v, ok := sd["edges_connect"]; ok && int(v.(float64)) != 0 {
		t.Errorf("edges_connect = %v, want 0 or omitted", v)
	}
	if v := int(sd["orphans"].(float64)); v != 0 {
		t.Errorf("orphans = %d, want 0", v)
	}
}

// TestSingleRememberThenConnect verifies that a single remember without related_to
// followed by an explicit connect counts correctly (1 node, 1 connect edge, 0 orphans,
// 0 inline edges).
func TestSingleRememberThenConnect_NoInline(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a-abc"}}`, false)
	call(r, "connect",
		map[string]any{"from_memory": "a-abc", "to_memory": "existing-1", "relationship": "led_to"},
		`{"id":"edge-1","from_memory":"a-abc","to_memory":"existing-1","relationship":"led_to","narrative":""}`,
		false)

	_, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if v := int(sd["nodes"].(float64)); v != 1 {
		t.Errorf("nodes = %d, want 1", v)
	}
	if v := int(sd["edges"].(float64)); v != 1 {
		t.Errorf("edges = %d, want 1", v)
	}
	inlineEdges := 0
	if v, ok := sd["edges_inline"]; ok {
		inlineEdges = int(v.(float64))
	}
	if inlineEdges != 0 {
		t.Errorf("edges_inline = %d, want 0", inlineEdges)
	}
	if v := int(sd["edges_connect"].(float64)); v != 1 {
		t.Errorf("edges_connect = %d, want 1", v)
	}
	if v := int(sd["orphans"].(float64)); v != 0 {
		t.Errorf("orphans = %d, want 0", v)
	}
}

// TestNodeNoEdgesIsOrphan verifies that a node with no connections at flush time
// counts as exactly 1 orphan.
func TestNodeNoEdgesIsOrphan(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "remember", map[string]any{"label": "Lone", "domain": "proj"}, `{"node":{"id":"lone-1"}}`, false)

	_, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if v := int(sd["orphans"].(float64)); v != 1 {
		t.Errorf("orphans = %d, want 1", v)
	}
}

// TestAcceptance_10Nodes6Inline2Connect2Orphan verifies the acceptance scenario:
// 10 nodes (6 linked inline, 2 linked by connect, 2 unlinked).
func TestAcceptance_10Nodes6Inline2Connect2Orphan(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	// 6 nodes with inline edges
	call(r, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "A", "domain": "proj", "related_to": []string{"e1"}},
			{"label": "B", "domain": "proj", "related_to": []string{"e2"}},
			{"label": "C", "domain": "proj", "related_to": []string{"e3"}},
			{"label": "D", "domain": "proj", "related_to": []string{"e4"}},
			{"label": "E", "domain": "proj", "related_to": []string{"e5"}},
			{"label": "F", "domain": "proj", "related_to": []string{"e6"}},
		},
	}, `{"nodes":[
		{"node":{"id":"a-1"},"skipped_connections":[]},
		{"node":{"id":"b-2"},"skipped_connections":[]},
		{"node":{"id":"c-3"},"skipped_connections":[]},
		{"node":{"id":"d-4"},"skipped_connections":[]},
		{"node":{"id":"e-5"},"skipped_connections":[]},
		{"node":{"id":"f-6"},"skipped_connections":[]}
	]}`, false)

	// 4 nodes without inline edges
	call(r, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "G", "domain": "proj"},
			{"label": "H", "domain": "proj"},
			{"label": "I", "domain": "proj"},
			{"label": "J", "domain": "proj"},
		},
	}, `{"nodes":[
		{"node":{"id":"g-7"},"skipped_connections":[]},
		{"node":{"id":"h-8"},"skipped_connections":[]},
		{"node":{"id":"i-9"},"skipped_connections":[]},
		{"node":{"id":"j-10"},"skipped_connections":[]}
	]}`, false)

	// Connect g and h via explicit connect (g connected to existing, h connected to existing)
	call(r, "connect",
		map[string]any{"from_memory": "g-7", "to_memory": "e1", "relationship": "led_to"},
		`{"id":"eg","from_memory":"g-7","to_memory":"e1","relationship":"led_to","narrative":""}`,
		false)
	call(r, "connect",
		map[string]any{"from_memory": "h-8", "to_memory": "e2", "relationship": "led_to"},
		`{"id":"eh","from_memory":"h-8","to_memory":"e2","relationship":"led_to","narrative":""}`,
		false)
	// i-9 and j-10 are orphans

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &sd); err != nil {
		t.Fatalf("json parse: %v\nsummary:\n%s", err, summary)
	}

	check := func(key string, want int) {
		t.Helper()
		v, ok := sd[key].(float64)
		if !ok {
			t.Errorf("%s missing or wrong type: %v\nsummary:\n%s", key, sd[key], summary)
			return
		}
		if int(v) != want {
			t.Errorf("%s = %d, want %d\nsummary:\n%s", key, int(v), want, summary)
		}
	}
	check("nodes", 10)
	check("edges", 8) // 6 inline + 2 connect
	check("edges_inline", 6)
	check("edges_connect", 2)
	check("orphans", 2)
}

// TestPerProcessCurrentFiles_WritesWithPIDSuffix verifies that writeCurrent writes
// a file named <path>.current.<pid> (not the shared .current).
func TestPerProcessCurrentFiles_WritesWithPIDSuffix(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a"}}`, false)

	pid := os.Getpid()
	currentFile := fmt.Sprintf("%s.current.%d", jsonPath, pid)
	if _, err := os.Stat(currentFile); err != nil {
		t.Errorf("expected %s to exist after Record(); got: %v", filepath.Base(currentFile), err)
	}

	// Old-style shared .current must NOT exist.
	sharedFile := jsonPath + ".current"
	if _, err := os.Stat(sharedFile); err == nil {
		t.Errorf("shared .current file must not be written; found %s", filepath.Base(sharedFile))
	}

	r.Flush() //nolint:errcheck

	// After flush the .current.<pid> file should be cleaned up.
	if _, err := os.Stat(currentFile); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after Flush(); err: %v", filepath.Base(currentFile), err)
	}
}

// TestPerProcessCurrentFiles_RecoversCrashedProcess verifies that a new recorder
// recovers a .current.<pid> file whose process has died.
func TestPerProcessCurrentFiles_RecoversCrashedProcess(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	humanPath := filepath.Join(dir, "stats.log")

	fakePID := 99991
	crashedData := `{"session_id":"crashed","start_ts":"2026-10-01T10:00:00Z","wkd":5.0,"type":"filing","nodes":3,"edges":2,"orphans":1,"transient":0,"ratio":0.5,"burst":false}` + "\n"
	curFile := fmt.Sprintf("%s.current.%d", jsonPath, fakePID)
	if err := os.WriteFile(curFile, []byte(crashedData), 0600); err != nil {
		t.Fatal(err)
	}

	// Override processAlive: fakePID is dead.
	old := stats.ProcessAliveFn
	stats.ProcessAliveFn = func(pid int) bool { return pid != fakePID }
	defer func() { stats.ProcessAliveFn = old }()

	stats.New(humanPath, jsonPath)

	data, _ := os.ReadFile(jsonPath)
	if !strings.Contains(string(data), "crashed") {
		t.Errorf("crashed session should have been recovered into main jsonl;\ngot: %s", string(data))
	}
	if _, err := os.Stat(curFile); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after recovery", filepath.Base(curFile))
	}
}

// TestPerProcessCurrentFiles_DoesNotAdoptLiveProcess verifies that a new recorder
// does NOT recover a .current.<pid> file whose process is still running.
func TestPerProcessCurrentFiles_DoesNotAdoptLiveProcess(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	humanPath := filepath.Join(dir, "stats.log")

	fakePID := 77771
	liveData := `{"session_id":"live","start_ts":"2026-10-01T10:00:00Z","wkd":5.0,"type":"filing","nodes":3,"edges":2,"orphans":1,"transient":0,"ratio":0.5,"burst":false}` + "\n"
	curFile := fmt.Sprintf("%s.current.%d", jsonPath, fakePID)
	if err := os.WriteFile(curFile, []byte(liveData), 0600); err != nil {
		t.Fatal(err)
	}

	// Override processAlive: fakePID is alive.
	old := stats.ProcessAliveFn
	stats.ProcessAliveFn = func(pid int) bool { return pid == fakePID }
	defer func() { stats.ProcessAliveFn = old }()

	stats.New(humanPath, jsonPath)

	// The live .current file must still exist.
	if _, err := os.Stat(curFile); err != nil {
		t.Errorf("live process .current file was incorrectly removed: %v", err)
	}
	// Main jsonl must be empty (no recovery happened).
	if fi, err := os.Stat(jsonPath); err == nil && fi.Size() > 0 {
		data, _ := os.ReadFile(jsonPath)
		if strings.Contains(string(data), "live") {
			t.Errorf("should not have recovered live process's data;\ngot: %s", string(data))
		}
	}
}

// TestDefaultClientIsUnknown verifies that when MEMORYWEB_CLIENT is unset the
// recorded client field is "unknown" rather than empty.
func TestDefaultClientIsUnknown(t *testing.T) {
	old := os.Getenv("MEMORYWEB_CLIENT")
	os.Unsetenv("MEMORYWEB_CLIENT")
	defer func() {
		if old != "" {
			os.Setenv("MEMORYWEB_CLIENT", old)
		}
	}()

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "stats.jsonl")
	r := stats.New(filepath.Join(dir, "stats.log"), jsonPath)

	call(r, "remember", map[string]any{"label": "A", "domain": "proj"}, `{"node":{"id":"a"}}`, false)
	r.Flush() //nolint:errcheck

	data, _ := os.ReadFile(jsonPath)
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	client, ok := sd["client"].(string)
	if !ok || client != "unknown" {
		t.Errorf("expected client='unknown' when MEMORYWEB_CLIENT unset, got %v", sd["client"])
	}
}

func TestRecord_AuditStaleMode_ParsesCandidates(t *testing.T) {
	dir := t.TempDir()
	r := stats.New(filepath.Join(dir, "stats.log"), filepath.Join(dir, "stats.jsonl"))

	// audit(mode=stale) returns the same DriftCandidate JSON as whats_stale did.
	result := `[
		{"node":{"id":"a"},"reason":"possible duplicate of newer node","edge_count":3},
		{"node":{"id":"b"},"reason":"transient node older than 7 days","edge_count":0},
		{"node":{"id":"c"},"reason":"explicitly marked as contradicting each other","edge_count":7}
	]`
	call(r, "audit", map[string]any{"mode": "stale"}, result, false)

	summary, err := r.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !strings.Contains(summary, "Stale checks") {
		t.Errorf("summary should contain Stale checks line; got:\n%s", summary)
	}
	if !strings.Contains(summary, "3 candidate") {
		t.Errorf("summary should report 3 candidates; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1 duplicate") {
		t.Errorf("summary should break down type 'duplicate'; got:\n%s", summary)
	}
	if !strings.Contains(summary, "Dup edges") {
		t.Errorf("summary should contain Dup edges line; got:\n%s", summary)
	}
	if !strings.Contains(summary, "1x(3-5)") {
		t.Errorf("summary should show dup candidate with 3-5 edges; got:\n%s", summary)
	}

	jsonContent, _ := os.ReadFile(filepath.Join(dir, "stats.jsonl"))
	var sd map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(jsonContent), &sd); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if sc, ok := sd["stale_checks"].(float64); !ok || int(sc) != 1 {
		t.Errorf("stale_checks should be 1, got %v", sd["stale_checks"])
	}
	if sc, ok := sd["stale_candidates"].(float64); !ok || int(sc) != 3 {
		t.Errorf("stale_candidates should be 3, got %v", sd["stale_candidates"])
	}
	if v, ok := sd["dup_edge_3_5"].(float64); !ok || int(v) != 1 {
		t.Errorf("dup_edge_3_5 should be 1, got %v", sd["dup_edge_3_5"])
	}
}
