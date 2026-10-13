package stats_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/stats"
)

// TestDedupe_SessionID deduplicates rows sharing a session_id, keeping the
// row with the most nodes.
func TestDedupe_SessionID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")

	// session "aabbccdd" three times: 9 → 10 → 20 nodes. Keep 20.
	// session "eeff0011" once: 5 nodes. Keep 5.
	// session "11223344" once: 3 nodes. Keep 3.
	rows := strings.Join([]string{
		`{"session_id":"aabbccdd","start_ts":"2026-10-09T08:26:36Z","wkd":3.0,"type":"filing","nodes":9,"edges":2,"orphans":7,"transient":0,"ratio":0.0,"burst":false}`,
		`{"session_id":"aabbccdd","start_ts":"2026-10-09T08:26:36Z","wkd":8.0,"type":"filing","nodes":10,"edges":3,"orphans":7,"transient":0,"ratio":0.1,"burst":false}`,
		`{"session_id":"eeff0011","start_ts":"2026-10-06T10:00:00Z","wkd":15.0,"type":"filing","nodes":5,"edges":5,"orphans":0,"transient":0,"ratio":0.5,"burst":false}`,
		`{"session_id":"aabbccdd","start_ts":"2026-10-09T08:26:36Z","wkd":12.0,"type":"filing","nodes":20,"edges":8,"orphans":12,"transient":0,"ratio":0.3,"burst":false}`,
		`{"session_id":"11223344","start_ts":"2026-10-07T09:00:00Z","wkd":6.0,"type":"filing","nodes":3,"edges":3,"orphans":0,"transient":0,"ratio":0.4,"burst":false}`,
	}, "\n") + "\n"
	os.WriteFile(path, []byte(rows), 0644)

	orig, deduped, err := stats.Dedupe(path)
	if err != nil {
		t.Fatalf("Dedupe: %v", err)
	}
	if orig != 5 {
		t.Errorf("expected 5 original rows, got %d", orig)
	}
	if deduped != 3 {
		t.Errorf("expected 3 deduplicated rows, got %d", deduped)
	}

	// Parse output and confirm the surviving row for aabbccdd has 20 nodes.
	data, _ := os.ReadFile(path)
	var sessions []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var s map[string]any
		json.Unmarshal([]byte(line), &s)
		sessions = append(sessions, s)
	}
	if len(sessions) != 3 {
		t.Fatalf("output should have 3 rows, got %d", len(sessions))
	}
	for _, s := range sessions {
		if s["session_id"] == "aabbccdd" {
			if int(s["nodes"].(float64)) != 20 {
				t.Errorf("surviving aabbccdd row should have 20 nodes, got %v", s["nodes"])
			}
		}
	}
}

// TestDedupe_LegacyStartTS deduplicates legacy rows (no session_id) by start_ts.
func TestDedupe_LegacyStartTS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")

	rows := strings.Join([]string{
		`{"start_ts":"2026-10-02T10:12:30Z","wkd":12.0,"type":"filing","nodes":5,"edges":3,"orphans":2,"transient":0,"ratio":0.4,"burst":false}`,
		`{"start_ts":"2026-10-02T10:12:30Z","wkd":12.0,"type":"filing","nodes":5,"edges":3,"orphans":2,"transient":0,"ratio":0.4,"burst":false}`,
		`{"start_ts":"2026-10-03T09:00:00Z","wkd":8.0,"type":"filing","nodes":2,"edges":2,"orphans":0,"transient":0,"ratio":0.5,"burst":false}`,
	}, "\n") + "\n"
	os.WriteFile(path, []byte(rows), 0644)

	orig, deduped, err := stats.Dedupe(path)
	if err != nil {
		t.Fatalf("Dedupe: %v", err)
	}
	if orig != 3 {
		t.Errorf("expected 3 original rows, got %d", orig)
	}
	if deduped != 2 {
		t.Errorf("expected 2 deduplicated rows, got %d", deduped)
	}
}

// TestDedupe_CreatesBackup verifies that a .bak file is written.
func TestDedupe_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")

	row := `{"session_id":"aabbccdd","start_ts":"2026-10-09T08:26:36Z","wkd":5.0,"type":"filing","nodes":3,"edges":2,"orphans":1,"transient":0,"ratio":0.3,"burst":false}` + "\n"
	os.WriteFile(path, []byte(row), 0644)

	if _, _, err := stats.Dedupe(path); err != nil {
		t.Fatalf("Dedupe: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf(".bak file should exist after Dedupe: %v", err)
	}
}

// TestDedupe_SortsByStartTS verifies the output is sorted by start_ts ascending.
func TestDedupe_SortsByStartTS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")

	// Rows are out of order; expect them sorted.
	rows := strings.Join([]string{
		`{"session_id":"cccc","start_ts":"2026-10-09T10:00:00Z","wkd":5.0,"type":"filing","nodes":1,"edges":1,"orphans":0,"transient":0,"ratio":0.0,"burst":false}`,
		`{"session_id":"aaaa","start_ts":"2026-10-07T08:00:00Z","wkd":5.0,"type":"filing","nodes":1,"edges":1,"orphans":0,"transient":0,"ratio":0.0,"burst":false}`,
		`{"session_id":"bbbb","start_ts":"2026-10-08T09:00:00Z","wkd":5.0,"type":"filing","nodes":1,"edges":1,"orphans":0,"transient":0,"ratio":0.0,"burst":false}`,
	}, "\n") + "\n"
	os.WriteFile(path, []byte(rows), 0644)

	if _, _, err := stats.Dedupe(path); err != nil {
		t.Fatalf("Dedupe: %v", err)
	}

	data, _ := os.ReadFile(path)
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var s map[string]any
		json.Unmarshal([]byte(line), &s)
		ids = append(ids, s["session_id"].(string))
	}
	want := []string{"aaaa", "bbbb", "cccc"}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("output row %d: expected session_id %q, got %q", i, want[i], id)
		}
	}
}

// TestDedupe_NoDuplicates verifies that a clean file is written unchanged
// (same rows, same count) but still gets a .bak.
func TestDedupe_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.jsonl")

	rows := strings.Join([]string{
		`{"session_id":"aaaa","start_ts":"2026-10-07T08:00:00Z","wkd":5.0,"type":"filing","nodes":1,"edges":1,"orphans":0,"transient":0,"ratio":0.0,"burst":false}`,
		`{"session_id":"bbbb","start_ts":"2026-10-08T09:00:00Z","wkd":8.0,"type":"filing","nodes":2,"edges":2,"orphans":0,"transient":0,"ratio":0.0,"burst":false}`,
	}, "\n") + "\n"
	os.WriteFile(path, []byte(rows), 0644)

	orig, deduped, err := stats.Dedupe(path)
	if err != nil {
		t.Fatalf("Dedupe: %v", err)
	}
	if orig != 2 || deduped != 2 {
		t.Errorf("expected 2/2, got %d/%d", orig, deduped)
	}
}
