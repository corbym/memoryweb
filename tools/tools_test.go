package tools_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/corbym/memoryweb/db"
	"github.com/corbym/memoryweb/tools"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// ollamaRunning returns true when the Ollama server is reachable and the
// snowflake-arctic-embed model is available. The base URL is taken from the
// OLLAMA_HOST environment variable when set, so it works in both local
// development and CI environments. Tests that exercise LIKE search should call
// disableOllama(t) instead.
func ollamaRunning(t *testing.T) bool {
	t.Helper()
	base := os.Getenv("OLLAMA_HOST")
	if base == "" {
		base = "http://localhost:11434"
	}
	resp, err := http.Get(base + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Models []struct{ Name string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, m := range body.Models {
		if strings.HasPrefix(m.Name, "snowflake-arctic-embed") {
			return true
		}
	}
	return false
}

// disableOllama sets MEMORYWEB_OLLAMA_ENDPOINT=disabled for the duration of
// the test. Call this at the top of any test that exercises LIKE search in
// isolation so that a running Ollama instance does not interfere with
// embedding-based results.
func disableOllama(t *testing.T) {
	t.Helper()
	t.Setenv("MEMORYWEB_OLLAMA_ENDPOINT", "disabled")
}

// newEnvWithPath creates an isolated Store+Handler backed by a temp-file SQLite DB
// and returns the DB file path for tests that need a second raw SQL connection
// (e.g. to inspect internal tables like audit_log).
func newEnvWithPath(t *testing.T) (string, *db.Store, *tools.Handler) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return dbPath, store, tools.New(store, "dev", nil)
}

// newEnv creates an isolated Store+Handler. All existing tests use this.
// For tests that also need the DB file path, use newEnvWithPath.
func newEnv(t *testing.T) (*db.Store, *tools.Handler) {
	_, s, h := newEnvWithPath(t)
	return s, h
}

// call invokes a tool by name with arbitrary arguments and returns the ToolResult.
// It fails the test if the RPC layer itself errors (not tool-level errors).
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
	tr, ok := raw.(*tools.ToolResult)
	if !ok {
		t.Fatalf("CallTool(%q): result type %T, want *ToolResult", toolName, raw)
	}
	return tr
}

// text returns the first content block's text from a ToolResult.
func text(t *testing.T, tr *tools.ToolResult) string {
	t.Helper()
	if len(tr.Content) == 0 {
		t.Fatal("ToolResult has no content blocks")
	}
	return tr.Content[0].Text
}

// mustNotError fails the test if the ToolResult is an error.
func mustNotError(t *testing.T, tr *tools.ToolResult) {
	t.Helper()
	if tr.IsError {
		t.Fatalf("unexpected tool error: %s", text(t, tr))
	}
}

// mustError fails the test if the ToolResult is NOT an error.
func mustError(t *testing.T, tr *tools.ToolResult) {
	t.Helper()
	if !tr.IsError {
		t.Fatalf("expected tool error, got success: %s", text(t, tr))
	}
}

// addNode is a typed wrapper that returns the created node's ID.
func addNode(t *testing.T, h *tools.Handler, label, domain string, extras map[string]any) string {
	t.Helper()
	args := map[string]any{"label": label, "domain": domain}
	for k, v := range extras {
		args[k] = v
	}
	tr := call(t, h, "remember", args)
	mustNotError(t, tr)
	var resp struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &resp); err != nil {
		t.Fatalf("parse add_node response: %v", err)
	}
	if resp.Node.ID == "" {
		t.Fatal("add_node returned empty ID")
	}
	return resp.Node.ID
}

// searchIDs returns the node IDs from a search_nodes response.
func searchIDs(t *testing.T, tr *tools.ToolResult) []string {
	t.Helper()
	var result struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &result); err != nil {
		t.Fatalf("parse search_nodes response: %v", err)
	}
	ids := make([]string, len(result.Nodes))
	for i, n := range result.Nodes {
		ids[i] = n.ID
	}
	return ids
}

// contains returns true if needle is in haystack.
func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}

func TestGetNode_HappyPath(t *testing.T) {
	_, h := newEnv(t)
	id := addNode(t, h, "Boot crash", "deep-game", nil)

	tr := call(t, h, "recall", map[string]any{"id": id})
	mustNotError(t, tr)

	var nwe struct {
		Node struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"node"`
		Edges []any `json:"edges"`
	}
	if err := json.Unmarshal([]byte(text(t, tr)), &nwe); err != nil {
		t.Fatalf("parse get_node: %v", err)
	}
	if nwe.Node.ID != id {
		t.Errorf("got ID %q, want %q", nwe.Node.ID, id)
	}
	if nwe.Node.Label != "Boot crash" {
		t.Errorf("got label %q, want %q", nwe.Node.Label, "Boot crash")
	}
}

func TestGetNode_NotFound(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "recall", map[string]any{"id": "does-not-exist"})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("error should say 'not found', got: %s", text(t, tr))
	}
}

func TestGetNode_ArchivedNodeIsHidden(t *testing.T) {
	store, h := newEnv(t)
	id := addNode(t, h, "Stale node", "deep-game", nil)

	if err := store.ArchiveNode(id, "no longer relevant"); err != nil {
		t.Fatalf("ArchiveNode: %v", err)
	}

	tr := call(t, h, "recall", map[string]any{"id": id})
	mustError(t, tr) // archived → treated as not found
	if !strings.Contains(text(t, tr), "not found") {
		t.Errorf("archived node should report not found, got: %s", text(t, tr))
	}
}

func TestUnknownTool_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "totally_unknown_tool", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool' message, got: %s", text(t, tr))
	}
}

func TestCallTool_MalformedParams_ReturnsError(t *testing.T) {
	_, h := newEnv(t)
	_, err := h.CallTool(json.RawMessage(`{not valid json`))
	if err == nil {
		t.Error("malformed JSON params should return an error")
	}
}

const errOccurredAtRequiresWhyMatters = "occurred_at requires why_matters — explain why this decision is significant before filing it on the timeline."

func newHandlerWithVersion(t *testing.T, version string, checker func() (string, error)) *tools.Handler {
	t.Helper()
	_, store, _ := newEnvWithPath(t)
	return tools.New(store, version, checker)
}

func TestCheckForUpdates_IsUnknownTool(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "check_for_updates", map[string]any{})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "unknown tool") {
		t.Errorf("expected 'unknown tool'; got: %s", text(t, tr))
	}
}
