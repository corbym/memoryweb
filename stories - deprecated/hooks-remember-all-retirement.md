# Hooks: retire remember_all references (v1.46.0 retrospective)

> **COMPLETE** — shipped in v1.46.0, commit `065fbc4`.
> - Hook scripts updated: `hooks/memoryweb_save_hook.sh`, `hooks/memoryweb_precompact_hook.sh`
> - Tests corrected: `hooks/hooks_test.go` (4 assertions updated)

Retrospective for the v1.46.0 fix that replaced retired `remember_all` references in hook
scripts and tests. No further code changes needed; filed to close the story gap and record
the incident for future audits.

---

## Motivation

`remember_all` was tombstoned when the tool surface was reduced in v1.43.0 — it returns
an error directing agents to use `remember(items=[...])`. The periodic Stop hook
(`memoryweb_save_hook.sh`, fires every 15 human messages) and the PreCompact hook
(`memoryweb_precompact_hook.sh`) both still told the agent to "Call remember_all" in their
`stopReason` payload.

Four test assertions in `hooks/hooks_test.go` also asserted the wrong string.

**Impact:** Every periodic filing trigger on v1.43.0–v1.45.0 produced a failed tool call.
The agent would see the error, read the redirect message, and retry with the correct tool —
recoverable but wasteful, and it degraded mid-session filing quality for every user on
those releases.

**Why it wasn't caught sooner:** The hook tests (`hooks_test.go`) run the shell scripts via
`exec.Command` and assert on stdout/stderr content. The tests were asserting the old string
without the tests themselves being aware the tool had been retired. No Go-level constant tied
the hook string to the live tool surface, so the rename didn't propagate automatically.

---

## What was fixed (v1.46.0)

| File | Change |
|------|--------|
| `hooks/memoryweb_save_hook.sh` | `stopReason`: `"Call remember_all"` → `"Call remember with an items array"` |
| `hooks/memoryweb_precompact_hook.sh` | Same wording update |
| `hooks/hooks_test.go` | 4 assertions updated to match new wording |

---

## Prevention

The hook scripts embed free-text agent instructions. There is no compile-time link between
the tool name in a hook script and the live tool surface in `tools/definitions.go`. Future
tool removals or renames must include a grep of `hooks/` for the old name as part of the
removal checklist.

Suggested checklist addition for `CLAUDE.md` (or the release process node in memoryweb-meta):
when retiring or renaming a tool, run:

```
grep -r "remember_all\|<old-tool-name>" hooks/
```

and update any matches before tagging the release.

---

## References

- Bug node: `finding-memoryweb-periodic-stop-hook-every-15-turns-referenced-retired-remember-all-hooks-and-tests-were-broken-76dcf8d3`
- Hook script distribution: `hook-script-distribution-repo-hooks-directory-memoryweb-setup-command-42a9df06`
- Recordari portability finding: `memoryweb-periodic-stop-hook-every-15-turns-is-portable-to-recordari-as-a-mid-session-orphan-audit-trigger-ba574484`
