# install-skill subcommand — embed and install memoryweb-skill.md

**Status:** DONE

Adds a `memoryweb install-skill` subcommand that writes the memoryweb agent
skill to `~/.claude/skills/memoryweb/SKILL.md`. Embeds `docs/memoryweb-skill.md`
in the binary at build time so the skill version is always pinned to the binary
version with no separate artifact or network dependency.

Closes the gap found in this session: `memoryweb setup` wires hooks and MCP
config but installs no skill, leaving agent guidance absent by default for every
user.

---

## Design

### Embedding

Use `//go:embed docs/memoryweb-skill.md` to bake the skill content into the
binary at build time — same principle as `tools.Instructions`. No separate file
to ship in the Homebrew formula. A user on v1.56.0 always gets the v1.56.0 skill;
no drift possible.

```go
//go:embed docs/memoryweb-skill.md
var skillContent string
```

Declare in a new file `skill.go` at the package root alongside `main.go`.

### Install path

`~/.claude/skills/memoryweb/SKILL.md`

Claude Code loads skills from `~/.claude/skills/<name>/SKILL.md` at the user
level, making the skill available in every project without per-project config.
Create the directory if it does not exist (`os.MkdirAll`, mode 0755).

### Idempotency

If the file already exists and its content matches the embedded version, print
`memoryweb skill is already up to date` and exit 0. If the content differs,
overwrite and print `memoryweb skill updated`.

### Subcommand interface

```
memoryweb install-skill [--dry-run]
```

`--dry-run`: print the target path and whether an update would occur, without
writing. Consistent with the `setup --dry-run` flag.

### setup integration

Add an `install-skill` step to `runSetup` after the hooks block and before the
Ollama block. Print a single confirmation line on success:

```
✓ Agent skill installed at ~/.claude/skills/memoryweb/SKILL.md
```

Idempotency means re-running `setup` after an `install-skill` call is safe.

### doctor integration

Add a check in `doctorCheckHooks` (or a new `doctorCheckSkill`) that verifies:
1. `~/.claude/skills/memoryweb/SKILL.md` exists
2. Its content matches the embedded version (stale check)

Report:
- `ok` — present and current
- `warn` — present but stale (content differs from embedded)
- `fail` — missing — `run: memoryweb install-skill`

---

## Changes

| File | Change |
|------|--------|
| `skill.go` | New — `//go:embed docs/memoryweb-skill.md` + `var skillContent string` |
| `main.go` | Add `install-skill` case to the `os.Args` switch; add to help text and subcommand list |
| `main.go` | `runSetup` — call `installSkill` after hooks block |
| `main.go` | `doctorCheckSkill` — skill presence + staleness check |
| `main_test.go` | `TestInstallSkill_*` — write, idempotent, stale-overwrite, dry-run |
| `setup_test.go` | Extend setup integration test to assert skill file written |

---

## Acceptance criteria

- `memoryweb install-skill` writes `~/.claude/skills/memoryweb/SKILL.md`
  containing the embedded skill content; creates directory if absent
- Second run with identical content exits 0 with "already up to date" message
- Second run after content change overwrites and reports "updated"
- `--dry-run` prints target path and update status without writing
- `memoryweb setup` installs the skill as part of its onboarding flow
- `memoryweb doctor` reports `ok` / `warn` (stale) / `fail` (missing) for
  skill presence
- Embedded content matches `docs/memoryweb-skill.md` at build time — verified
  by a test that compares `skillContent` to the file read at test time
- `go test ./...` green

---

## References

- Finding: `finding-memoryweb-setup-does-not-install-the-skill-file-docs-memoryweb-skill-md-has-no-automated-installation-path-c6a53afd` (memoryweb-meta)
- Option: `option-embed-memoryweb-skill-content-in-binary-memoryweb-install-skill-writes-to-claude-skills-memoryweb-skill-md-setup-calls-it-automatically-91eaf12f` (memoryweb-meta)
- `stories/setup-idempotency.md` — idempotency pattern to follow
- `docs/memoryweb-skill.md` — file to embed
