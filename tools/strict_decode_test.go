package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/corbym/memoryweb/tools"
)

// minimalArgs returns the smallest valid argument object for each tool.
// Used only to attach an unknown field for strict-decode rejection tests.
var minimalArgs = map[string]map[string]any{
	"remember":            {"label": "x", "domain": "d", "why_matters": "y"},
	"connect":             {"from_memory": "a", "to_memory": "b"},
	"recall":              {"id": "test-id"},
	"search":              {"query": "x"},
	"recent":              {},
	"why_connected":       {"from_label": "a", "to_label": "b"},
	"history":             {"domain": "d"},
	"significance":        {"domain": "d"},
	"alias":               {"action": "list"},
	"forget":              {"id": "x", "reason": "test"},
	"restore":             {"id": "x"},
	"audit":               {"mode": "orphans"},
	"forget_all":          {"items": []map[string]string{{"id": "x", "reason": "test"}}},
	"orient":              {},
	"revise":              {"id": "x"},
	"suggest_connections": {"id": "x"},
	"domains":             {},
	"disconnect":          {"id": "x"},
	"trace":               {"from_id": "a", "to_id": "b"},
	"visualise":           {"domain": "d"},
	"rename_domain":       {"old_domain": "a", "new_domain": "b"},
}

func toolNames(t *testing.T, h *tools.Handler) []string {
	t.Helper()
	raw, err := h.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var resp struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("parse ListTools: %v", err)
	}
	names := make([]string, len(resp.Tools))
	for i, td := range resp.Tools {
		names[i] = td.Name
	}
	return names
}

func TestStrictDecode_AllToolsRejectUnknownField(t *testing.T) {
	_, h := newEnv(t)
	for _, name := range toolNames(t, h) {
		t.Run(name, func(t *testing.T) {
			base, ok := minimalArgs[name]
			if !ok {
				t.Fatalf("minimalArgs missing entry for tool %q — add one", name)
			}
			args := make(map[string]any, len(base)+1)
			for k, v := range base {
				args[k] = v
			}
			args["__bogus_unknown_field__"] = true

			tr := call(t, h, name, args)
			mustError(t, tr)
			msg := text(t, tr)
			if !strings.Contains(msg, "unknown field") && !strings.Contains(msg, "__bogus_unknown_field__") {
				t.Errorf("error should name unknown field, got: %s", msg)
			}
			if !strings.Contains(msg, "tools/list") {
				t.Errorf("error should mention tools/list, got: %s", msg)
			}
		})
	}
}

func TestStrictDecode_OrientAcceptsDomainsArray(t *testing.T) {
	// domains is now a valid parameter (multi-domain orient). It must not be
	// rejected as an unknown field, and must not fall through to bootstrap.
	_, h := newEnv(t)
	addNode(t, h, "test node", "memoryweb-meta", nil)
	tr := call(t, h, "orient", map[string]any{"domains": []string{"memoryweb-meta"}})
	mustNotError(t, tr)
	msg := text(t, tr)
	if strings.Contains(msg, "cross_domain_snapshot") {
		t.Error("orient must not fall through to bootstrap when domains array is passed")
	}
}

func TestStrictDecode_RememberRejectsUnknownField(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "test",
		"domain":      "d",
		"why_matters": "because",
		"extra_field": "nope",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "extra_field") && !strings.Contains(text(t, tr), "unknown field") {
		t.Errorf("expected unknown field error, got: %s", text(t, tr))
	}
}

func TestStrictDecode_BatchRememberRejectsUnknownTopLevelField(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "a", "domain": "d", "why_matters": "y"},
		},
		"extra_field": "nope",
	})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "extra_field") && !strings.Contains(text(t, tr), "unknown field") {
		t.Errorf("expected unknown field error, got: %s", text(t, tr))
	}
}

func TestStrictDecode_BatchRememberRejectsUnknownItemField(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"items": []map[string]any{
			{"label": "a", "domain": "d", "why_matters": "y", "extra_field": "nope"},
		},
	})
	mustError(t, tr)
	msg := text(t, tr)
	if !strings.Contains(msg, "extra_field") && !strings.Contains(msg, "unknown field") {
		t.Errorf("expected unknown field error on batch item, got: %s", msg)
	}
}

func TestStrictDecode_SingleRememberAcceptsNullItems(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "remember", map[string]any{
		"label":       "test",
		"domain":      "d",
		"why_matters": "because",
		"items":       nil,
	})
	mustNotError(t, tr)
}

func callNoArgs(t *testing.T, h *tools.Handler, toolName string) *tools.ToolResult {
	t.Helper()
	params, err := json.Marshal(map[string]string{"name": toolName})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
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

func TestStrictDecode_MissingArgsRejectsWriteTools(t *testing.T) {
	_, h := newEnv(t)
	for _, tool := range []string{"remember", "recall", "connect", "search", "why_connected"} {
		t.Run(tool, func(t *testing.T) {
			tr := callNoArgs(t, h, tool)
			mustError(t, tr)
			msg := text(t, tr)
			if !strings.Contains(msg, "unexpected end of JSON input") &&
				!strings.Contains(msg, "is required") {
				t.Errorf("expected empty-args error, got: %s", msg)
			}
		})
	}
}

func TestStrictDecode_MissingArgsAcceptsOptionalTools(t *testing.T) {
	_, h := newEnv(t)
	for _, tool := range []string{"orient", "domains", "recent"} {
		t.Run(tool, func(t *testing.T) {
			tr := callNoArgs(t, h, tool)
			mustNotError(t, tr)
		})
	}
}

func TestStrictDecode_SearchRejectsEmptyQuery(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "search", map[string]any{"query": ""})
	mustError(t, tr)
	if !strings.Contains(text(t, tr), "query is required") {
		t.Errorf("expected query required error, got: %s", text(t, tr))
	}
}
