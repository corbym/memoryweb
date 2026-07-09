# Sweep tech debt — rename variables to meaningful, human-readable names

**Status:** OPEN

**Shared-surface node:** `story-sweep-tech-debt-rename-variables-to-meaningful-human-readable-names-a99a71cc`

**Depends on:** `standing-all-variables-must-have-meaningful-human-readable-names-3a0f8822` (memoryweb-meta standing rule)

---

## Why

The standing rule requiring meaningful, human-readable variable names governs new
code going forward but does nothing for what's already in the codebase. Without an
explicit sweep, existing short/abbreviated/scaffolding names persist indefinitely —
the rule only ever applies to diffs, never to the baseline.

Filed as a cross-cutting goal in `memoryweb-shared-surface` on 2026-07-08 (applies to
both memoryweb and Recordari) but never sliced into a memoryweb repo story. This is
the memoryweb-side half.

---

## Scope

Audit `db/`, `tools/`, `cmd/`, `main.go`, and `stats/` for:

- Single-letter variables outside trivial loop counters (`i`, `j`, `k` in a `for`
  loop are fine; `n`, `s`, `a`, `b` standing in for `node`, `store`, nodeA, nodeB are
  not — though note `db/audit.go`'s existing `a`/`b` pair-comparison variables and
  `aID`/`bLabel`/... struct fields are an established local convention for
  contradicts-pair code specifically; don't rename those without a separate decision,
  since they're intentional shorthand for "the two nodes in the pair", not scaffolding)
- Unexplained abbreviations (`whyMatters` is fine and self-explanatory; `wm`, `desc`
  used as a full identifier, `tr` for `ToolResult` outside test helpers, are not)
- Leftover scaffolding placeholders (`tmp`, `foo`, `x1`, `data2`)

Exclude:
- Test helper conventions already established as short-and-idiomatic across the
  suite (`h *Handler`, `tr *ToolResult`, `t *testing.T` — Go test convention, not
  tech debt)
- Any rename that would touch a public API surface name exposed to MCP clients
  (tool/parameter names) — those are a different, much higher-blast-radius change
  and out of scope here

---

## Approach

1. Grep each package for short/ambiguous identifiers (`grep -nE '\b[a-z]{1,2}\s*:?='`
   as a starting filter, manually triaged — this pattern is noisy and will need
   judgment, not blind rename).
2. Build a list of candidates with file:line, grouped by package.
3. Rename in small, reviewable commits — one package per commit, not one giant diff.
4. Run `go test ./...` after each package to confirm no behavioural change (renames
   only, zero logic change).

---

## Acceptance criteria

- No single-letter or unexplained-abbreviation variable names remain in `db/`,
  `tools/`, `cmd/`, `main.go`, `stats/` outside the established exclusions above.
- Every rename is variable-name-only — no logic changes bundled in.
- `go test ./...` green after each package's rename commit.
- Test helper and pair-comparison exclusions documented in this file are respected
  (don't rename `a`/`b` in `db/audit.go`'s contradicts-pair code, don't rename `h`/`tr`
  in test files).

---

## Files (expected)

- Touches variable declarations across `db/*.go`, `tools/*.go`, `cmd/**/*.go`,
  `main.go`, `stats/*.go` — no new files.

---

## References

- Shared-surface node: `story-sweep-tech-debt-rename-variables-to-meaningful-human-readable-names-a99a71cc`
- Standing rule (memoryweb-meta): `standing-all-variables-must-have-meaningful-human-readable-names-3a0f8822`
- Standing rule (recordari, cross-cutting parity): `standing-all-variables-must-have-meaningful-human-readable-names-9c761de3`
