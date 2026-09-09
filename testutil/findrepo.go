package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FindRepoRoot walks up from the working directory until it finds the go.mod
// that marks the repository root, and returns that directory. Shared by test
// files across packages (hooks, cmd/*) so the discovery logic lives in exactly
// one place. FindRepoRoot works from TestMain (no *testing.T available);
// FindRepoRootOrFatal wraps it for ordinary tests.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

// FindRepoRootOrFatal returns the repo root, failing the test on error.
func FindRepoRootOrFatal(t *testing.T) string {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
