package tools

import (
	"strings"
	"testing"
)

func TestMarshalResponseIndent_ErrorSurfaces(t *testing.T) {
	v := map[string]any{"fn": func() {}}
	_, err := marshalResponseIndent(v)
	if err == nil {
		t.Fatal("expected error marshalling unsupported type")
	}
	if !strings.Contains(err.Error(), "tool response") {
		t.Errorf("error should carry tool-response context, got: %v", err)
	}
}
