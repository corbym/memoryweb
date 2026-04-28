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

// setupBin is the compiled memoryweb binary, built once by TestMain.
var setupBin string

// TestMain builds the memoryweb binary once for all setup integration tests.
// The internal package main tests (main_test.go) share this binary via the
// single test binary Go produces for a directory.
func TestMain(m *testing.M) {
	root := setupFindRepoRoot()
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("memoryweb-setup-test-%d", os.Getpid()))
	buildCmd := exec.Command("go", "build", "-o", bin, ".")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: cannot build memoryweb: %v\n%s\n", err, out)
		os.Exit(1)
	}
	setupBin = bin
	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

func setupFindRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

// runSetupCmd invokes "memoryweb setup [args]" with HOME overridden to homeDir
// and "n\n" piped to stdin so Ollama prompts are answered automatically.
func runSetupCmd(t *testing.T, homeDir string, args ...string) (string, int) {
	t.Helper()
	return runSetupCmdWithStdin(t, homeDir, "n\n", args...)
}

// runSetupCmdWithStdin invokes "memoryweb setup [args]" with a custom stdin payload.
func runSetupCmdWithStdin(t *testing.T, homeDir, stdinContent string, args ...string) (string, int) {
	t.Helper()
	allArgs := append([]string{"setup"}, args...)
	cmd := exec.Command(setupBin, allArgs...)
	cmd.Env = append(os.Environ(), "HOME="+homeDir)
	if stdinContent != "" {
		cmd.Stdin = strings.NewReader(stdinContent)
	}
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

// hooksDir returns the absolute path to the repo's hooks directory.
func hooksDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(setupFindRepoRoot(), "hooks")
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestSetupInstallsHooks: dry-run prints the resulting config including hook paths.
func TestSetupInstallsHooks(t *testing.T) {
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0755)

	out, code := runSetupCmd(t, tmpHome, "--hooks-dir", hooksDir(t), "--dry-run")
	if code != 0 {
		t.Fatalf("setup --dry-run exited %d; output:\n%s", code, out)
	}

	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")

	if !strings.Contains(out, saveHook) {
		t.Errorf("output should contain save hook path %q; got:\n%s", saveHook, out)
	}
	if !strings.Contains(out, precompactHook) {
		t.Errorf("output should contain precompact hook path %q; got:\n%s", precompactHook, out)
	}

	// Dry-run must not write the settings file.
	settingsPath := filepath.Join(tmpHome, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); err == nil {
		t.Error("--dry-run should not write settings.local.json")
	}
}

// TestSetupMergesExistingConfig: existing Stop hooks are preserved after merge.
func TestSetupMergesExistingConfig(t *testing.T) {
	tmpHome := t.TempDir()
	claudeDir := filepath.Join(tmpHome, ".claude")
	os.MkdirAll(claudeDir, 0755)

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

	// "n\n" answers the Ollama install prompt (not installed on CI).
	out, code := runSetupCmd(t, tmpHome, "--hooks-dir", hooksDir(t))
	if code != 0 {
		t.Fatalf("setup exited %d; output:\n%s", code, out)
	}

	written, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	body := string(written)

	if !strings.Contains(body, "/existing/other-hook.sh") {
		t.Error("existing hook should be preserved after merge")
	}
	if !strings.Contains(body, "memoryweb_save_hook.sh") {
		t.Errorf("save hook should appear in merged config; got:\n%s", body)
	}
	if !strings.Contains(body, "memoryweb_precompact_hook.sh") {
		t.Errorf("precompact hook should appear in merged config; got:\n%s", body)
	}
}

// TestSetupDryRunSkipsOllamaPrompts: --dry-run reports Ollama status without
// showing an interactive [y/N] prompt.
func TestSetupDryRunSkipsOllamaPrompts(t *testing.T) {
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0755)

	// No stdin provided — if a prompt appeared, bufio.Scanner would return EOF
	// and the binary would fail or behave unexpectedly.
	out, code := runSetupCmdWithStdin(t, tmpHome, "", "--hooks-dir", hooksDir(t), "--dry-run")
	if code != 0 {
		t.Fatalf("setup --dry-run exited %d; output:\n%s", code, out)
	}
	if strings.Contains(out, "[y/N]") {
		t.Errorf("--dry-run should not show interactive prompts; got:\n%s", out)
	}
	// Dry-run should report Ollama state.
	if !strings.Contains(out, "dry-run") && !strings.Contains(out, "Ollama") {
		t.Errorf("--dry-run should report Ollama status; got:\n%s", out)
	}
}

// TestSetupDeclinesOllamaInstall: answering "n" to the install prompt prints
// an advisory and exits 0. The model pull is not prompted — if Ollama were
// installed, setup would pull automatically.
// This test only runs when Ollama is NOT installed; it is skipped on machines
// where Ollama is already present because the install prompt never fires.
func TestSetupDeclinesOllamaInstall(t *testing.T) {
	if _, err := exec.LookPath("ollama"); err == nil {
		t.Skip("Ollama is installed; the install prompt does not appear — skip")
	}
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0755)

	// Answer "n" to the Ollama install prompt.
	out, code := runSetupCmdWithStdin(t, tmpHome, "n\n", "--hooks-dir", hooksDir(t))
	if code != 0 {
		t.Fatalf("setup exited %d; output:\n%s", code, out)
	}
	// Should print an advisory about downloading Ollama manually.
	if !strings.Contains(out, "Advisory") {
		t.Errorf("expected advisory after declining install; got:\n%s", out)
	}
}

// TestSetupIdempotent: running setup twice does not duplicate hook entries.
func TestSetupIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	os.MkdirAll(filepath.Join(tmpHome, ".claude"), 0755)

	args := []string{"--hooks-dir", hooksDir(t)}

	if _, code := runSetupCmd(t, tmpHome, args...); code != 0 {
		t.Fatal("first setup run failed")
	}
	if _, code := runSetupCmd(t, tmpHome, args...); code != 0 {
		t.Fatal("second setup run failed")
	}

	settingsPath := filepath.Join(tmpHome, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	// Count occurrences of the save hook — should appear exactly once.
	count := strings.Count(string(data), "memoryweb_save_hook.sh")
	if count != 1 {
		t.Errorf("save hook should appear exactly once after two runs; found %d; settings:\n%s", count, data)
	}
}

