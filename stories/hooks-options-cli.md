# hooks: options CLI subcommand

Adds `memoryweb options [set <key> <value>]` — a CLI subcommand for viewing and setting
per-user hook options. Options are stored in `~/.memoryweb/config.json` and read by all
hook scripts. Existing env-var overrides (`MEMORYWEB_SAVE_INTERVAL`) remain as the
highest-precedence layer. Mirrors Recordari STORY-326 (`recordari options`).

---

## Motivation

memoryweb's hook behaviours are currently hardcoded or controlled only by env vars set
at setup time. There is no way to toggle a hook off without editing `settings.json`
or unsetting env vars. Recordari gained a user options system (STORY-316/318/326) that
lets users view and set per-hook toggles via `recordari options`. memoryweb needs the
same, both for parity and because the new UserPromptSubmit and PostCompact hooks
(see `stories/hooks-userpromptsubmit.md`, `stories/hooks-postcompact-reinject.md`) need
to be individually disableable.

---

## Options

| Key | Type | Default | Hook |
|-----|------|---------|------|
| `session_orient_enabled` | bool | false | UserPromptSubmit (orient nudge) |
| `auto_recall` | bool | false | UserPromptSubmit (per-prompt memory injection) |
| `pre_compact_enabled` | bool | false | PreCompact (filing prompt) |
| `reinject_on_compact` | bool | false | PostCompact (context reinject) |
| `sweep_interval_turns` | int | 15 | Stop (periodic filing); 0 disables |
| `subagent_orient_enabled` | bool | false | SubagentStart (digest inject) |
| `subagent_audit_enabled` | bool | false | SubagentStop (orphan audit) |

All bool options default to **false** — opt-in only. The Stop hook (`sweep_interval_turns`)
is the sole exception: it mirrors the long-standing hardcoded default of 15 and is the
one hook that has been active since initial deployment. New hooks add tokens the user is
not expecting on a fresh install; the user must explicitly enable each one.

**Note for implementers:** `pre_compact_enabled` defaulting to false changes the behaviour
of existing installs once the option check is added to `memoryweb_precompact_hook.sh`.
Existing users who want to preserve the precompact prompt must run
`memoryweb options set pre_compact_enabled true` after upgrading. Include a migration
note in the release description.

---

## Storage: `~/.memoryweb/config.json`

Options are written as a flat JSON object:

```json
{
  "session_orient_enabled": false,
  "auto_recall": false,
  "pre_compact_enabled": false,
  "reinject_on_compact": false,
  "sweep_interval_turns": 15,
  "subagent_orient_enabled": false,
  "subagent_audit_enabled": false
}
```

The file is created on first `memoryweb options set` call if absent. The options
subcommand reads and patches a single key; it does not rewrite the entire file from
defaults, so keys set by the user are not lost.

---

## Changes

### 1. `main.go` — `memoryweb options` subcommand

Add `case "options":` to the `os.Args[1]` switch:

```go
case "options":
    optionsCmd()
    return
```

Add to help text:
```
  options        View or set hook behaviour options
```

`optionsCmd` and `runOptionsCmd`:

```go
func optionsCmd() {
    flags := flag.NewFlagSet("options", flag.ExitOnError)
    flags.Parse(os.Args[2:])
    if err := runOptionsCmd(os.Stdout, os.Stderr, flags.Args()); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
}

func runOptionsCmd(out, errOut io.Writer, args []string) error {
    home, _ := os.UserHomeDir()
    cfgPath := filepath.Join(home, ".memoryweb", "config.json")

    if len(args) == 0 {
        return optionsPrint(cfgPath, out)
    }
    if args[0] == "set" {
        if len(args) < 3 {
            return fmt.Errorf("'options set' requires a key and value\nUsage: memoryweb options set <key> <value>")
        }
        return optionsSet(cfgPath, args[1], args[2], out, errOut)
    }
    return fmt.Errorf("unknown options subcommand %q\nUsage: memoryweb options [set <key> <value>]", args[0])
}
```

`optionsPrint` reads `config.json` (or uses defaults) and prints each option as:

```
session_orient_enabled   false  orient() nudge when orient not yet called (UserPromptSubmit hook)
auto_recall              false  inject relevant memories on each prompt (UserPromptSubmit hook)
pre_compact_enabled      false  file before compaction (PreCompact hook)
reinject_on_compact      false  reinject orient context after compaction (PostCompact hook)
sweep_interval_turns     15     turns between filing prompts; 0 disables (Stop hook)
subagent_orient_enabled  false  inject digest at sub-agent start (SubagentStart hook)
subagent_audit_enabled   false  orphan audit on sub-agent stop (SubagentStop hook)
```

`optionsSet` reads config, patches the one key, writes back. Validation:
- Bool keys accept `true/false/on/off/1/0` (case-insensitive).
- `sweep_interval_turns` must be a non-negative integer.
- Unknown key → error with the valid key list.

### 2. `hooks/memoryweb_lib.sh` — hook config reading

`memoryweb_read_option` and `memoryweb_option_enabled` are added in the
UserPromptSubmit story (`stories/hooks-userpromptsubmit.md`). This story ensures the
*existing* hooks also use them:

**`memoryweb_save_hook.sh`** — replace the hardcoded `MEMORYWEB_SAVE_INTERVAL` line:

Before:
```bash
SAVE_INTERVAL="${MEMORYWEB_SAVE_INTERVAL:-15}"
```

After:
```bash
memoryweb_read_option "sweep_interval_turns" "15"
SAVE_INTERVAL="${MEMORYWEB_SAVE_INTERVAL:-${_opt}}"
```

And short-circuit when `sweep_interval_turns=0`:
```bash
if [ "${SAVE_INTERVAL}" -eq 0 ]; then
  printf '{"continue":true}\n'
  exit 0
fi
```

**`memoryweb_precompact_hook.sh`** — add option check after sourcing lib:
```bash
if ! memoryweb_option_enabled "pre_compact_enabled" "true"; then
  printf '{"continue":true}\n'
  exit 0
fi
```

### 3. `main_test.go` — tests for `runOptionsCmd`

- `TestOptionsCmd_PrintDefaults` — config absent → prints all seven keys with default values.
- `TestOptionsCmd_SetBool` — `options set session_orient_enabled false` → `config.json` contains `"session_orient_enabled": false`; subsequent print shows `false`.
- `TestOptionsCmd_SetInt` — `options set sweep_interval_turns 30` → value stored as `30`.
- `TestOptionsCmd_SetInt_Zero` — `sweep_interval_turns 0` → stored as `0` (disables hook).
- `TestOptionsCmd_UnknownKey` — unknown key → non-zero exit, error message names valid keys.
- `TestOptionsCmd_BadBool` — `options set session_orient_enabled maybe` → error.
- `TestOptionsCmd_BadInt` — `options set sweep_interval_turns -5` → error (negative).
- `TestOptionsCmd_Idempotent` — two sequential set calls → only the patched key changes; others retain prior values.

### 4. `hooks/hooks_test.go` — option-read tests

- `TestReadOption_FileAbsent` — no config file → helper returns default.
- `TestReadOption_FilePresent` — config file with `"session_orient_enabled": false` → helper returns `false`.
- `TestSaveHook_SweepZeroDisabled` — `sweep_interval_turns=0` in config → hook outputs `{"continue":true}` without blocking.
- `TestPreCompactHook_OptionDisabled` — `pre_compact_enabled=false` in config → hook outputs `{"continue":true}`.

---

## Acceptance criteria

- `memoryweb options` prints all seven keys with defaults when config absent (all bools show `false`; `sweep_interval_turns` shows `15`).
- `memoryweb options set <key> <value>` writes the key; subsequent `options` reflects it.
- `memoryweb options set unknown_key true` exits non-zero with an error naming valid keys.
- `memoryweb options set sweep_interval_turns 0`: save hook passes through on next invocation.
- `memoryweb options set pre_compact_enabled true`: precompact hook blocks on next invocation.
- All `TestOptionsCmd_*` and `TestReadOption_*` pass.
- Before merging: update `docs/memoryweb-skill.md` to document the `memoryweb options` subcommand and the full option table (name, default, which hook it controls). Update `AGENTS.md` deploy section if the setup flow changes. Release description must include the migration note for `pre_compact_enabled`.
- `go test ./...` green.

---

## Files

| File | Change |
|------|--------|
| `main.go` | `optionsCmd`, `runOptionsCmd`, `optionsPrint`, `optionsSet`; dispatch + help |
| `main_test.go` | `TestOptionsCmd_*` tests |
| `hooks/memoryweb_lib.sh` | `memoryweb_read_option`, `memoryweb_option_enabled` (if not already added by hooks-userpromptsubmit) |
| `hooks/memoryweb_save_hook.sh` | Read `sweep_interval_turns` from config; short-circuit on 0 |
| `hooks/memoryweb_precompact_hook.sh` | Check `pre_compact_enabled` option |
| `hooks/hooks_test.go` | `TestReadOption_*`, `TestSaveHook_SweepZeroDisabled`, `TestPreCompactHook_OptionDisabled` |

---

## References

- Recordari STORY-326: `story-326-done-recordari-options-view-and-set-server-side-user-options-via-cli`
- Recordari STORY-316/318: user options schema and hook fields
- Related: `stories/hooks-userpromptsubmit.md` (adds `session_orient_enabled` and `auto_recall`)
- Related: `stories/hooks-postcompact-reinject.md` (adds `reinject_on_compact`)
- Related: `stories/hooks-subagent-start-stop.md` (adds `subagent_orient_enabled`, `subagent_audit_enabled`)
