package tools_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func assertDigestLines(t *testing.T, raw string, ids ...string) {
	t.Helper()
	var resp struct {
		Lines []string `json:"lines"`
		Nodes []any    `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("parse digest response: %v\nbody: %s", err, raw)
	}
	if len(resp.Nodes) > 0 {
		t.Error("digest mode must not return a nodes array — use lines")
	}
	if len(resp.Lines) == 0 {
		t.Fatal("digest mode must return non-empty lines array")
	}
	for _, id := range ids {
		found := false
		for _, line := range resp.Lines {
			if strings.HasPrefix(line, "["+id+"]") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected digest line starting with [%s], got lines: %v", id, resp.Lines)
		}
	}
}

// assertDigestLineIDPrefix checks the first bracketed token is the memory id (recall key).
func assertDigestLineIDPrefix(t *testing.T, line, id string) bool {
	t.Helper()
	if !strings.HasPrefix(line, "["+id+"]") {
		t.Errorf("digest line must start with [%s] for recall(id); got: %q", id, line)
		return false
	}
	return true
}

func assertDigestStringSection(t *testing.T, section json.RawMessage, minLines int) {
	t.Helper()
	if section == nil {
		t.Fatal("expected digest section to be present")
	}
	var lines []string
	if err := json.Unmarshal(section, &lines); err != nil {
		t.Fatalf("digest section must be a string array, got: %s", string(section))
	}
	if len(lines) < minLines {
		t.Fatalf("expected at least %d digest lines, got %d: %v", minLines, len(lines), lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "[") {
			t.Errorf("digest line must start with [id]; got: %q", line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("digest line must be single-line text; got embedded newline in: %q", line)
		}
	}
}

func TestSearch_DigestMode_SingleLinePerNode(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id1 := addNode(t, h, "digest search alpha", "digest-search", map[string]any{"why_matters": "first digest target"})
	id2 := addNode(t, h, "digest search beta", "digest-search", map[string]any{"why_matters": "second digest target"})

	tr := call(t, h, "search", map[string]any{
		"query": "digest search", "domain": "digest-search", "digest": true,
	})
	mustNotError(t, tr)
	assertDigestLines(t, text(t, tr), id1, id2)
}

func TestSearch_DigestMode_DefaultOff_ReturnsNodesArray(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	addNode(t, h, "digest default off node", "digest-search-off", nil)

	tr := call(t, h, "search", map[string]any{"query": "digest default off", "domain": "digest-search-off"})
	mustNotError(t, tr)

	var resp struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
		Lines []string                     `json:"lines"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search response: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Error("default search must return nodes array")
	}
	if len(resp.Lines) > 0 {
		t.Error("default search must not return lines array")
	}
}

func TestRecent_DigestMode_SingleLinePerNode(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "digest recent one", "digest-recent", nil)
	id2 := addNode(t, h, "digest recent two", "digest-recent", nil)

	tr := call(t, h, "recent", map[string]any{"domain": "digest-recent", "digest": true, "limit": 10})
	mustNotError(t, tr)
	assertDigestLines(t, text(t, tr), id1, id2)
}

func TestHistory_DigestMode_SingleLinePerNode(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "digest history one", "digest-history", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z", "why_matters": "first milestone",
	})
	id2 := addNode(t, h, "digest history two", "digest-history", map[string]any{
		"occurred_at": "2026-02-01T00:00:00Z", "why_matters": "second milestone",
	})

	tr := call(t, h, "history", map[string]any{"domain": "digest-history", "digest": true})
	mustNotError(t, tr)
	assertDigestLines(t, text(t, tr), id1, id2)
}

func TestSignificance_DigestMode_SingleLinePerSection(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "digest sig declared one", "digest-sig", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z", "why_matters": "declared one",
	})
	addNode(t, h, "digest sig declared two", "digest-sig", map[string]any{
		"occurred_at": "2026-02-01T00:00:00Z", "why_matters": "declared two",
	})
	structural := addNode(t, h, "digest sig structural hub", "digest-sig", map[string]any{"why_matters": "hub"})
	for _, label := range []string{"digest sig linker a", "digest sig linker b"} {
		linker := addNode(t, h, label, "digest-sig", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": structural,
			"relationship": "connects_to", "narrative": "links",
		}))
	}

	tr := call(t, h, "significance", map[string]any{"domain": "digest-sig", "digest": true})
	mustNotError(t, tr)

	var resp struct {
		Declared   json.RawMessage `json:"declared"`
		Structural json.RawMessage `json:"structural"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse significance response: %v", err)
	}
	assertDigestStringSection(t, resp.Declared, 2)
	assertDigestStringSection(t, resp.Structural, 1)
}

func TestOrient_DigestMode_SingleLinePerSection(t *testing.T) {
	_, h := newEnv(t)
	hub1 := addNode(t, h, "digest orient hub one", "digest-orient", map[string]any{"why_matters": "central one"})
	hub2 := addNode(t, h, "digest orient hub two", "digest-orient", map[string]any{"why_matters": "central two"})
	for _, pair := range []struct{ linker, hub string }{
		{"digest orient linker a", hub1},
		{"digest orient linker b", hub1},
		{"digest orient linker c", hub2},
		{"digest orient linker d", hub2},
	} {
		linker := addNode(t, h, pair.linker, "digest-orient", nil)
		mustNotError(t, call(t, h, "connect", map[string]any{
			"from_memory": linker, "to_memory": pair.hub,
			"relationship": "connects_to", "narrative": "links",
		}))
	}
	addNode(t, h, "digest orient recent one", "digest-orient", nil)
	addNode(t, h, "digest orient recent two", "digest-orient", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "digest-orient", "digest": true})
	mustNotError(t, tr)

	var resp struct {
		Significant json.RawMessage `json:"significant"`
		Recent      json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	assertDigestStringSection(t, resp.Significant, 2)
	assertDigestStringSection(t, resp.Recent, 2)
}

func TestAudit_DigestMode_Stale(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "digest duplicate label", "digest-audit-stale", nil)
	addNode(t, h, "digest duplicate label", "digest-audit-stale", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "stale", "domain": "digest-audit-stale", "digest": true})
	mustNotError(t, tr)

	lines := unmarshalDigestLines(t, text(t, tr))
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 stale digest lines, got %d", len(lines))
	}
}

func TestAudit_DigestMode_Orphans(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "digest stale orphan one", "digest-audit", nil)
	addNode(t, h, "digest stale orphan two", "digest-audit", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": "digest-audit", "digest": true})
	mustNotError(t, tr)

	lines := unmarshalDigestLines(t, text(t, tr))
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 digest lines for orphans, got %d", len(lines))
	}
}

func TestHistory_DigestMode_OccurredAtSuffixNotPrefix(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "digest history dated", "digest-history-date", map[string]any{
		"occurred_at": "2026-01-01T00:00:00Z", "why_matters": "dated milestone",
	})

	tr := call(t, h, "history", map[string]any{"domain": "digest-history-date", "digest": true})
	mustNotError(t, tr)

	var resp struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse history digest response: %v", err)
	}
	if len(resp.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(resp.Lines))
	}
	line := resp.Lines[0]
	assertDigestLineIDPrefix(t, line, id)
	if strings.HasPrefix(line, "[2026") {
		t.Errorf("occurred_at must not prefix the line (breaks recall id parsing); got: %q", line)
	}
	if !strings.Contains(line, "2026-01-01") {
		t.Errorf("occurred_at should appear as suffix on digest line; got: %q", line)
	}
}

func TestSearch_DigestMode_MultilineFieldsSingleLine(t *testing.T) {
	disableOllama(t)
	_, h := newEnv(t)
	id := addNode(t, h, "zz multiline\nlabel here", "digest-multiline", map[string]any{
		"why_matters": "first line\nsecond line of why_matters",
	})

	tr := call(t, h, "search", map[string]any{
		"query": "multiline", "domain": "digest-multiline", "digest": true,
	})
	mustNotError(t, tr)

	var resp struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse search digest response: %v", err)
	}
	if len(resp.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(resp.Lines))
	}
	line := resp.Lines[0]
	assertDigestLineIDPrefix(t, line, id)
	if strings.Contains(line, "\n") {
		t.Errorf("digest line must not contain embedded newlines; got: %q", line)
	}
}

func TestAudit_DigestMode_Stale_SingleEntryReturnsLinesArray(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "digest duplicate single", "digest-audit-stale-one", nil)
	addNode(t, h, "digest duplicate single", "digest-audit-stale-one", nil)

	tr := call(t, h, "audit", map[string]any{
		"mode": "stale", "domain": "digest-audit-stale-one", "digest": true, "limit": 1,
	})
	mustNotError(t, tr)

	lines := unmarshalDigestLines(t, text(t, tr))
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 digest line, got %d", len(lines))
	}
	if strings.Contains(text(t, tr), `"description"`) {
		t.Error("digest stale line must not include full node description field")
	}
}

func TestOrient_DigestMode_SingleRecentReturnsLinesArray(t *testing.T) {
	_, h := newEnv(t)
	addNode(t, h, "digest orient lone recent", "digest-orient-lone", nil)

	tr := call(t, h, "orient", map[string]any{"domain": "digest-orient-lone", "digest": true})
	mustNotError(t, tr)

	var resp struct {
		Recent json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse orient response: %v", err)
	}
	var lines []string
	if err := json.Unmarshal(resp.Recent, &lines); err != nil {
		t.Fatalf("digest orient recent with 1 entry must be string array, got: %s", string(resp.Recent))
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 recent digest line, got %d", len(lines))
	}
}

func TestAudit_DigestMode_Orphans_SingleEntryReturnsLinesArray(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "digest single orphan", "digest-audit-single", nil)

	tr := call(t, h, "audit", map[string]any{"mode": "orphans", "domain": "digest-audit-single", "digest": true})
	mustNotError(t, tr)

	lines := unmarshalDigestLines(t, text(t, tr))
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 digest line, got %d", len(lines))
	}
	assertDigestLineIDPrefix(t, lines[0], id)
}

func TestAudit_DigestMode_Archived(t *testing.T) {
	_, h := newEnv(t)
	id1 := addNode(t, h, "digest archived one", "digest-archived", nil)
	id2 := addNode(t, h, "digest archived two", "digest-archived", nil)
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id1, "reason": "test"}))
	mustNotError(t, call(t, h, "forget", map[string]any{"id": id2, "reason": "test"}))

	tr := call(t, h, "audit", map[string]any{"mode": "archived", "domain": "digest-archived", "digest": true})
	mustNotError(t, tr)

	var resp struct {
		Nodes []string `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("digest audit archived response: %v", err)
	}
	lines := resp.Nodes
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 archived digest lines, got %d", len(lines))
	}
}
