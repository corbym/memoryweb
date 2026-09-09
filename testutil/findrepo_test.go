package testutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corbym/memoryweb/testutil"
)

func TestFindRepoRoot_ReturnsPathContainingGoMod(t *testing.T) {
	root, err := testutil.FindRepoRoot()
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("expected %s to contain go.mod: %v", root, err)
	}
	if info.IsDir() {
		t.Fatalf("go.mod at %s is a directory", root)
	}
}
