package stats_test

import (
	"bytes"
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
