package integration_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/tools"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newEnv(t *testing.T) (*db.Store, *tools.Handler) {
	t.Helper()
	dir := t.TempDir()
	store, err := db.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, tools.New(store, "dev", nil)
}

func call(t *testing.T, h *tools.Handler, toolName string, arguments any) *tools.ToolResult {
	t.Helper()
	argBytes, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	params, _ := json.Marshal(map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(argBytes),
	})
	raw, err := h.CallTool(params)
	if err != nil {
		t.Fatalf("CallTool(%q): %v", toolName, err)
	}
	return raw.(*tools.ToolResult)
}

// isOllamaAvailable probes localhost:11434 to detect a running Ollama instance.
func isOllamaAvailable() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestSemanticSearchFindsRelatedConcept verifies that a query using different
// wording than the stored nodes still surfaces the semantically relevant node
// when Ollama is running.
func TestSemanticSearchFindsRelatedConcept(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama not running; skipping semantic search test")
	}

	_, h := newEnv(t)

	call(t, h, "remember", map[string]any{
		"label":       "boot crash interrupts ROM",
		"description": "System fails to complete POST sequence; ROM checksum error on startup",
		"why_matters": "Critical startup failure pathway blocks all other operations",
		"domain":      "test",
	})

	call(t, h, "remember", map[string]any{
		"label":       "straitjacket tutorial movement",
		"description": "Guide on practising escape from physical restraints",
		"why_matters": "Physical training technique for stage performance",
		"domain":      "test",
	})

	tr := call(t, h, "search", map[string]any{
		"query": "startup failure",
	})
	if tr.IsError {
		t.Fatalf("search_nodes returned error: %s", tr.Content[0].Text)
	}

	result := tr.Content[0].Text
	if !strings.Contains(result, "boot crash") {
		t.Errorf("expected boot crash node in results, got: %s", result)
	}
	if strings.Contains(result, "straitjacket") {
		t.Errorf("unexpected tutorial node in results; got: %s", result)
	}
}

// TestSemanticSearchFallsBackToLiteralIfVecUnavailable verifies that
// search_nodes returns literal LIKE results (no panic, no error) when
// semantic search is unavailable (Ollama not running, so embed returns nil).
func TestSemanticSearchFallsBackToLiteralIfVecUnavailable(t *testing.T) {
	_, h := newEnv(t)

	call(t, h, "remember", map[string]any{
		"label":       "hardware boot literal",
		"description": "ROM chip literal test description for fallback search",
		"why_matters": "startup process verification",
		"domain":      "test",
	})

	tr := call(t, h, "search", map[string]any{
		"query": "ROM chip literal test",
	})
	if tr.IsError {
		t.Fatalf("search_nodes returned error: %s", tr.Content[0].Text)
	}

	result := tr.Content[0].Text
	if !strings.Contains(result, "hardware boot literal") {
		t.Errorf("expected literal match in results, got: %s", result)
	}
}

// TestSemanticSearchScopedByDomain verifies that a domain-scoped semantic
// search only returns nodes from the requested domain.
func TestSemanticSearchScopedByDomain(t *testing.T) {
	if !isOllamaAvailable() {
		t.Skip("Ollama not running; skipping semantic search test")
	}

	_, h := newEnv(t)

	call(t, h, "remember", map[string]any{
		"label":       "boot failure in domain A",
		"description": "System startup crash sequence in project A",
		"why_matters": "Critical boot failure must be resolved in project A",
		"domain":      "domain-a",
	})

	call(t, h, "remember", map[string]any{
		"label":       "boot failure in domain B",
		"description": "System startup crash sequence in project B",
		"why_matters": "Critical boot failure must be resolved in project B",
		"domain":      "domain-b",
	})

	tr := call(t, h, "search", map[string]any{
		"query":  "startup crash",
		"domain": "domain-a",
	})
	if tr.IsError {
		t.Fatalf("search_nodes returned error: %s", tr.Content[0].Text)
	}

	result := tr.Content[0].Text
	if !strings.Contains(result, "domain-a") {
		t.Errorf("expected domain-a node in results, got: %s", result)
	}
	if strings.Contains(result, "domain-b") {
		t.Errorf("unexpected domain-b node in results; got: %s", result)
	}
}
