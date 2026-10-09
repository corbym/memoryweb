# setup idempotency — no duplicate hook entries on second run

**Status:** COMPLETE — v1.53.0 (commit 3678c39)

Fixes `memoryweb setup` so running it twice does not write duplicate hook entries to
`~/.claude/settings.json`. Adds unit tests for `runSetup` and `setupUpsertCommand`,
which currently have no test coverage. Mirrors the same idempotency requirement as
recordari STORY-313 (`recordari-client init --hooks`).

---

## Motivation

Running `memoryweb setup` twice produces duplicate hook entries in
`~/.claude/settings.json`. Duplicate entries cause every lifecycle event to fire the
hook twice — double orient() calls, double audits, double context injections.

The `setupUpsertCommand` helper already uses `filepath.Base` to match by script filename
rather than by exact path, which is the correct strategy. The gap is that there are **no
unit tests** for `runSetup` or `setupUpsertCommand`, so the idempotency contract is
unverified and any regression will be invisible.

Filed: 2026-08-30. Recordari counterpart: STORY-313.

---

## Changes

### 1. Unit tests for `setupUpsertCommand`

Add `TestSetupUpsertCommand_*` in `main_test.go`:

- `TestSetupUpsertCommand_AppendsWhenEmpty` — empty slice → one entry appended.
- `TestSetupUpsertCommand_IdempotentSamePath` — entry already present with same command path
  → returns slice unchanged (same length, same content).
- `TestSetupUpsertCommand_ReplacesOnPathChange` — entry present with different path but same
  basename → replaces in-place (same length, new path).
- `TestSetupUpsertCommand_DoesNotTouchOtherEntries` — two entries, one matching → only the
  matching entry is affected.

### 2. Integration test for `runSetup` idempotency

Add `TestRunSetup_IdempotentHookEntries` in `main_test.go`:

```go
func TestRunSetup_IdempotentHookEntries(t *testing.T) {
    // arrange: temp home dir, fake hook scripts, real runSetup
    // act: call runSetup twice with the same paths and a scripted "n" reader
    // assert: settings.json Stop and PreCompact arrays each have exactly
    //         one entry after the second call
}
```

`runSetup` already accepts `io.Writer` and `io.Reader` for testability; use a
`strings.NewReader("n\n")` (or equivalent) to answer all interactive prompts with "no" so
the test can run without Ollama or other external deps.

### 3. Fix any edge cases surfaced by tests

If the tests reveal a case where the current `setupUpsertCommand` logic fails — e.g., the
outer entry structure written by `makeEntry` is read back in a shape that doesn't match
the traversal in `setupUpsertCommand` — fix the traversal. The contract is:

> A second `memoryweb setup` call with the same binary path must produce **no change** to
> `settings.json` if the hook entries are already present.

---

## Acceptance criteria

- `TestSetupUpsertCommand_IdempotentSamePath`: slice length unchanged after second call with
  same command path.
- `TestSetupUpsertCommand_ReplacesOnPathChange`: slice length unchanged but entry updated
  when basename matches and path differs.
- `TestRunSetup_IdempotentHookEntries`: Stop and PreCompact entries are exactly length 1
  after two sequential `runSetup` calls.
- `go test ./...` green.

---

## Files

- `main_test.go` — new `TestSetupUpsertCommand_*` and `TestRunSetup_Idempotent*` tests
- `main.go` — `setupUpsertCommand` fix if tests surface a bug; no change if already correct

---

## References

- Bug node: `bug-memoryweb-setup-run-twice-writes-duplicate-hook-entries-in-settings-json-84e7032a`
- Recordari STORY-313: `story-313-filed-recordari-client-init-hooks-installs-lifecycle-hooks-into-agent-platform-config-b6f8960d`
- Hook distribution: `hook-script-distribution-repo-hooks-directory-memoryweb-setup-command-42a9df06`
