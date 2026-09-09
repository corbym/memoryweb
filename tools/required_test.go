package tools_test

import (
	"strings"
	"testing"
)

// TestRequiredFieldsEnforced verifies that tools whose InputSchema declares a
// field Required reject calls that omit or blank that field before the handler
// runs, via the decodeParams framework (CR-26).
func TestRequiredFieldsEnforced(t *testing.T) {
	_, h := newEnv(t)
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"recall blank id", "recall", map[string]any{"id": ""}, "id is required"},
		{"recall missing id", "recall", map[string]any{}, "id is required"},
		{"forget missing id", "forget", map[string]any{}, "id is required"},
		{"audit missing mode", "audit", map[string]any{}, "mode is required"},
		{"suggest_connections missing id", "suggest_connections", map[string]any{}, "id is required"},
		{"disconnect blank id", "disconnect", map[string]any{"id": ""}, "id is required"},
		{"forget_all missing items", "forget_all", map[string]any{}, "items is required"},
		{"restore_all missing items", "restore_all", map[string]any{}, "items is required"},
		{"disconnect_all missing items", "disconnect_all", map[string]any{}, "items is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := call(t, h, tc.tool, tc.args)
			mustError(t, tr)
			if !strings.Contains(text(t, tr), tc.want) {
				t.Errorf("expected %q in error, got: %s", tc.want, text(t, tr))
			}
		})
	}
}

// TestRequiredFieldsNotOverEnforced guards against the framework rejecting
// arguments that are legitimately optional: conditional required fields
// (search needs query OR node_kind) and fully-optional tools must keep working.
func TestRequiredFieldsNotOverEnforced(t *testing.T) {
	_, h := newEnv(t)
	tr := call(t, h, "orient", map[string]any{})
	mustNotError(t, tr)
	tr = call(t, h, "search", map[string]any{"node_kind": "decision standing"})
	mustNotError(t, tr)
	tr = call(t, h, "significance", map[string]any{"domain": "d"})
	mustNotError(t, tr)
}
