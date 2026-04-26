package hooks_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

func hooksDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "hooks")
}

// makeTranscript creates a JSONL transcript with numHuman human messages under
// projectsDir/<project>/<sessionID>.jsonl.
func makeTranscript(t *testing.T, projectsDir, sessionID string, numHuman int) {
	t.Helper()
	projectDir := filepath.Join(projectsDir, "test-project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("makeTranscript: mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(projectDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("makeTranscript: create: %v", err)
	}
	defer f.Close()
	for i := 0; i < numHuman; i++ {
		fmt.Fprintf(f, `{"role":"human","content":"message %d"}`+"\n", i+1)
		fmt.Fprintf(f, `{"role":"assistant","content":"reply %d"}`+"\n", i+1)
	}
}

// runHook executes a hook script with JSON payload on stdin and custom env.
func runHook(t *testing.T, script, sessionID, stateDir, projectsDir string) (string, int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	cmd := exec.Command(script)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
		"MEMORYWEB_PROJECTS_DIR="+projectsDir,
		"MEMORYWEB_SAVE_INTERVAL=15",
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec hook %s: %v", script, err)
		}
	}
	return string(out), code
}

// ── save hook tests ───────────────────────────────────────────────────────────

func TestSaveHookAllowsBelowThreshold(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-below"

	makeTranscript(t, projectsDir, sessionID, 5) // 5 < 15

	out, code := runHook(t, saveHook, sessionID, stateDir, projectsDir)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"allow"`) {
		t.Errorf("expected allow decision for 5 messages; got:\n%s", out)
	}
}

func TestSaveHookBlocksAtThreshold(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-block"

	makeTranscript(t, projectsDir, sessionID, 15) // 15 >= 15

	out, _ := runHook(t, saveHook, sessionID, stateDir, projectsDir)
	if !strings.Contains(out, `"block"`) {
		t.Errorf("expected block decision for 15 messages; got:\n%s", out)
	}
	savingFlag := filepath.Join(stateDir, sessionID+".saving")
	if _, err := os.Stat(savingFlag); err != nil {
		t.Errorf(".saving flag not created: %v", err)
	}
}

func TestSaveHookAllowsOnReentry(t *testing.T) {
	saveHook := filepath.Join(hooksDir(t), "memoryweb_save_hook.sh")
	stateDir := t.TempDir()
	projectsDir := t.TempDir()
	sessionID := "test-save-reentry"

	makeTranscript(t, projectsDir, sessionID, 15)

	// Pre-create the saving flag (simulates re-entry after AI filed).
	savingFlag := filepath.Join(stateDir, sessionID+".saving")
	if err := os.WriteFile(savingFlag, []byte{}, 0644); err != nil {
		t.Fatalf("create saving flag: %v", err)
	}

	out, code := runHook(t, saveHook, sessionID, stateDir, projectsDir)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"allow"`) {
		t.Errorf("expected allow on re-entry; got:\n%s", out)
	}
	if _, err := os.Stat(savingFlag); err == nil {
		t.Error(".saving flag should have been deleted on re-entry")
	}
}

// ── precompact hook tests ─────────────────────────────────────────────────────

func runPrecompactHook(t *testing.T, stateDir, sessionID string) (string, int) {
	t.Helper()
	script := filepath.Join(hooksDir(t), "memoryweb_precompact_hook.sh")
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	cmd := exec.Command(script)
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(),
		"MEMORYWEB_HOOK_STATE_DIR="+stateDir,
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("exec precompact hook: %v", err)
		}
	}
	return string(out), code
}

func TestPrecompactHookBlocks(t *testing.T) {
	stateDir := t.TempDir()
	sessionID := "test-precompact-block"

	out, _ := runPrecompactHook(t, stateDir, sessionID)
	if !strings.Contains(out, `"block"`) {
		t.Errorf("expected block on first run; got:\n%s", out)
	}
	compactingFlag := filepath.Join(stateDir, sessionID+".compacting")
	if _, err := os.Stat(compactingFlag); err != nil {
		t.Errorf(".compacting flag not created: %v", err)
	}
}

func TestPrecompactHookAllowsOnReentry(t *testing.T) {
	stateDir := t.TempDir()
	sessionID := "test-precompact-reentry"

	// Pre-create the compacting flag.
	compactingFlag := filepath.Join(stateDir, sessionID+".compacting")
	if err := os.WriteFile(compactingFlag, []byte{}, 0644); err != nil {
		t.Fatalf("create compacting flag: %v", err)
	}

	out, code := runPrecompactHook(t, stateDir, sessionID)
	if code != 0 {
		t.Fatalf("hook exited %d; output:\n%s", code, out)
	}
	if !strings.Contains(out, `"allow"`) {
		t.Errorf("expected allow on re-entry; got:\n%s", out)
	}
	if _, err := os.Stat(compactingFlag); err == nil {
		t.Error(".compacting flag should have been deleted on re-entry")
	}
}
