package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dbFlag := flag.String("db", "", "memoryweb database path (default ~/.memoryweb.db)")
	dryRun := flag.Bool("dry-run", false, "print resulting config without writing")
	hooksDirFlag := flag.String("hooks-dir", "", "directory containing hook scripts (default: hooks/ next to binary)")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fatalf("cannot determine home directory: %v", err)
	}

	// Locate hooks directory.
	hooksDir := *hooksDirFlag
	if hooksDir == "" {
		exe, err := os.Executable()
		if err != nil {
			fatalf("cannot determine binary path: %v", err)
		}
		hooksDir = filepath.Join(filepath.Dir(exe), "hooks")
	}

	saveHook := filepath.Join(hooksDir, "memoryweb_save_hook.sh")
	precompactHook := filepath.Join(hooksDir, "memoryweb_precompact_hook.sh")

	for _, script := range []string{saveHook, precompactHook} {
		info, err := os.Stat(script)
		if err != nil {
			fatalf("hook script not found: %s (%v)", script, err)
		}
		if info.Mode()&0o111 == 0 {
			fatalf("hook script is not executable: %s", script)
		}
	}

	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = filepath.Join(home, ".memoryweb.db")
	}

	// Read existing settings.local.json or start with an empty object.
	settingsPath := filepath.Join(home, ".claude", "settings.local.json")
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			fatalf("cannot parse %s: %v", settingsPath, err)
		}
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Get or create top-level "hooks" map.
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// Build the two hook entries.
	makeEntry := func(command string) map[string]interface{} {
		return map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": command,
					"env": map[string]interface{}{
						"MEMORYWEB_DB": dbPath,
					},
				},
			},
		}
	}

	// Merge Stop entries (append if not already present).
	stop := toSlice(hooks["Stop"])
	if !containsCommand(stop, saveHook) {
		stop = append(stop, makeEntry(saveHook))
	}
	hooks["Stop"] = stop

	// Merge PreCompact entries.
	precompact := toSlice(hooks["PreCompact"])
	if !containsCommand(precompact, precompactHook) {
		precompact = append(precompact, makeEntry(precompactHook))
	}
	hooks["PreCompact"] = precompact

	settings["hooks"] = hooks

	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fatalf("marshal settings: %v", err)
	}

	if *dryRun {
		fmt.Println(string(output))
		return
	}

	// Create state directory.
	stateDir := filepath.Join(home, ".memoryweb", "hook_state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		fatalf("create state dir: %v", err)
	}

	// Ensure .claude directory exists.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		fatalf("create .claude dir: %v", err)
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		fatalf("write settings: %v", err)
	}

	fmt.Println("memoryweb hooks installed. Restart Claude Code to activate.")
}

// toSlice safely converts an interface{} to []interface{}.
func toSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	s, _ := v.([]interface{})
	return s
}

// containsCommand reports whether any entry in the slice contains the given
// command path in its nested "hooks" array.
func containsCommand(entries []interface{}, cmd string) bool {
	for _, e := range entries {
		entry, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		hooks, _ := entry["hooks"].([]interface{})
		for _, h := range hooks {
			hm, ok := h.(map[string]interface{})
			if ok && hm["command"] == cmd {
				return true
			}
		}
	}
	return false
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
