# search/audit: document optional domain parameter

**Status:** OPEN

**Shared-surface node:** `issue-search-audit-domain-parameter-undocumented-omitting-it-searches-scans-the-whole-workspace-90a93e57`

**Precedent:** `stories/orient-optional-domain.md` (orient already documents omit-domain bootstrap)

---

## Why

`orient()` explicitly states that omitting `domain` gives a cross-domain bootstrap
snapshot. `search()` and `audit()` accept the same optional `domain` parameter but
their descriptions say nothing about omitting it — agents learn only by trial or by
noticing domain tags in returned compact lines.

Cost observed: three separate domain-scoped search calls in one session that a single
unscoped call would have covered. Same vocabulary-guidance class as
`stories/search-vocabulary-gap.md` (COMPLETE), but specifically about the domain param
rather than query vocabulary.

---

## Changes

### search tool description

Add near the top of the tool description and in the `domain` property description:

> Omitting `domain` searches the entire workspace across all domains. Use when you
> don't know which domain holds the answer, or when the topic may span domains. Scope
> to a single domain when you know it — results are cleaner and faster.

Mirror orient's framing: optional scoping, not required.

### audit tool description

Same addition for all three modes:

> Omitting `domain` scans the entire workspace. Use for cross-domain drift review;
> scope to a domain for focused maintenance passes.

### Fallback cross-reference

In search description fallback strategy (already present from search-vocabulary-gap),
add unscoped search as step 0 before per-domain attempts:

> (0) try search without domain if the target domain is unknown;
> (1) scope to a domain; (2) recall; (3) orient.

---

## Acceptance criteria

- `search` tool description documents omit-domain = workspace-wide search.
- `audit` tool description documents omit-domain = workspace-wide scan (all modes).
- `domain` property descriptions on both tools explain optional scoping.
- No behaviour change — documentation only.
- `TestListTools_*` description tests still pass (no forbidden structural vocabulary).

---

## Files (expected)

- `tools/definitions.go` — search and audit tool + property descriptions
- Optionally `tools/tools_test.go` if description contract tests exist for these strings

---

## References

- Precedent: `stories/orient-optional-domain.md`, `orient-optional-domain-arg-cross-1b54474c`
- Related: `search-tool-gives-no-vocabulary--639d6489` (query vocabulary — already shipped)
- Recordari: mirror same description contract when syncing tool surface
