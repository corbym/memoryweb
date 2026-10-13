package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Dedupe rewrites a JSONL stats file in place, collapsing duplicate rows for
// the same session into one. A backup is written to path+".bak" first.
//
// Deduplication key: session_id when present; start_ts (RFC3339) for legacy rows.
// When multiple rows share a key, the one with the most nodes is kept; ties go
// to the last row. Output is sorted by start_ts ascending.
//
// Returns (originalCount, dedupedCount, error).
func Dedupe(path string) (int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("read %s: %w", path, err)
	}

	var raw []sessionData
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var sd sessionData
		if err := json.Unmarshal([]byte(line), &sd); err != nil {
			return 0, 0, fmt.Errorf("parse line %q: %w", line, err)
		}
		raw = append(raw, sd)
	}

	deduped := deduplicateSessions(raw)

	// Sort deduplicated rows by start_ts ascending so the reader can rely on order.
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].StartTS.Before(deduped[j].StartTS)
	})

	// Write backup before modifying.
	bak := path + ".bak"
	if err := os.WriteFile(bak, data, 0600); err != nil {
		return 0, 0, fmt.Errorf("write backup %s: %w", bak, err)
	}

	// Encode deduplicated rows.
	var sb strings.Builder
	for _, sd := range deduped {
		b, err := json.Marshal(sd)
		if err != nil {
			return 0, 0, fmt.Errorf("marshal session: %w", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0600); err != nil {
		return 0, 0, fmt.Errorf("write %s: %w", path, err)
	}

	return len(raw), len(deduped), nil
}
