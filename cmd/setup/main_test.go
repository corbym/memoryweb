package main_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var setupBin string

func TestMain(m *testing.M) {
	root := findRepoRoot()
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-setup-%d", os.Getpid()))
	buildCmd := exec.Command("go", "build", "-o", bin, "./cmd/setup")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot build cmd/setup: %v\n%s\n", err, out)
		os.Exit(1)
	}
	setupBin = bin
	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root")
		}
		dir = parent
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return findRepoRoot()
}

func runSetup(t *testing.T, homeDir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(setupBin, args...)
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec setup: %v", err)
		}
	}
	return string(out), code
}

func TestSetupInstallsHooks(t *testing.T) {
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0755)

	hooksDir := filepath.Join(repoRoot(t), "hooks")

	out, code := runSetup(t, tmpHome,
		"--hooks-dir", hooksDir,
		"--dry-run",
	)
	if code != 0 {
		t.Fatalf("setup exited %d; output:\n%s", code, out)
	}

	saveHook := filepath.Join(hooksDir, "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir, "memoryweb_precompact_hook.sh")

	if !strings.Contains(out, saveHook) {
		t.Errorf("output should contain save hook path %q; got:\n%s", saveHook, out)
	}
	if !strings.Contains(out, precompactHook) {
		t.Errorf("output should contain precompact hook path %q; got:\n%s", precompactHook, out)
	}

	// Assert no files were written.
	settingsPath := filepath.Join(tmpHome, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); err == nil {
		t.Error("--dry-run should not write settings.local.json")
	}
}

func TestSetupMergesExistingConfig(t *testing.T) {
	tmpHome := t.TempDir()
	claudeDir := filepath.Join(tmpHome, ".claude")
	os.MkdirAll(claudeDir, 0755)

	// Write existing settings with an unrelated Stop hook.
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/existing/other-hook.sh",
						},
					},
				},
			},
		},
	}
	existingBytes, _ := json.MarshalIndent(existing, "", "  ")
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, existingBytes, 0644); err != nil {
		t.Fatalf("write existing settings: %v", err)
	}

	hooksDir := filepath.Join(repoRoot(t), "hooks")
	out, code := runSetup(t, tmpHome, "--hooks-dir", hooksDir)
	if code != 0 {
		t.Fatalf("setup exited %d; output:\n%s", code, out)
	}

	// Read resulting settings.
	written, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	body := string(written)

	saveHook := filepath.Join(hooksDir, "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir, "memoryweb_precompact_hook.sh")

	if !strings.Contains(body, "/existing/other-hook.sh") {
		t.Error("existing hook should be preserved after merge")
	}
	if !strings.Contains(body, saveHook) {
		t.Errorf("save hook %q should appear in merged config; got:\n%s", saveHook, body)
	}
	if !strings.Contains(body, precompactHook) {
		t.Errorf("precompact hook %q should appear in merged config; got:\n%s", precompactHook, body)
	}
}
