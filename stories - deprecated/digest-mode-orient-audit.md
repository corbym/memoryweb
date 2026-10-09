# Digest mode — orient and audit (remainder after STORY-120)

**Status:** COMPLETE

**Shared-surface nodes:**
- `cross-cutting-gap-digest-mode-not-shipped-on-orient-or-audit-04aa0ae4`
- `digest-mode-render-multi-node-re-8fa64dcf` (standing rule)

**Predecessor:** `stories/digest-mode.md` (OPEN — search/recent/significance/history),
`stories/lean-format-retrieval-tools.md` (COMPLETE for memoryweb)

**Recordari backlog:** STORY-156 (orient), STORY-157 (audit)

---

## Why

Lean format + digest mode shipped for search, recent, history, and significance
(STORY-120 / lean-format-retrieval-tools). Two list-returning tools remain outstanding
on both products — the largest remaining JSON key-repetition tax:

**orient:** sections (`rules`, `declared_spine`, `significant`/`relevant`, `recent`)
still serialize as JSON object arrays with per-entry key repetition. Field-pruning
exists (id/label/why_matters) but uses ad-hoc truncation (200/120 rune cuts in handler
code), not `tools/lean.go`'s `truncateWhyMatters` (150, sentence boundary). No digest-line
collapse at 2+ entries.

**audit (all modes):** returns full `Node` or `DriftCandidate` objects — description,
tags, full why_matters untouched. No lean pruning, no digest lines. Highest token cost
on multi-result calls (e.g. 14 stale candidates in a smoke test).

Misreading STORY-120/lean-format as complete leaves the worst offenders unfixed.

---

## Scope

### orient

Apply lean.go helpers + digest-line rendering to all list sections:
- `rules`, `declared_spine`, `significant`/`relevant`, `recent`
- Replace ad-hoc 200/120 rune truncation with `truncateWhyMatters` (150, sentence boundary)
- At 2+ entries per section: collapse to single-line text per node (same format as
  `stories/digest-mode.md` working draft: `"[id] label — excerpt (domain, node_kind)"`)

### audit

All three modes (stale, orphans, archived):
- Lean field selection: id, label, why_matters (truncated), node_kind, reason/drift metadata
- Digest-line collapse for multi-result responses
- Preserve mode-specific fields (stale reason, orphan signal) inline in the line format

**memoryweb:** opt-in via parameter, default off (same as digest-mode.md for other tools).
**Recordari:** default on, no flag (same product asymmetry as digest-mode.md).

---

## Open questions (resolve at implementation)

1. Audit line format for drift candidates — how to embed stale reason without full JSON object?
2. orient `rules` section — standing nodes may need node_kind prominent in line format.
3. Per-section vs whole-response digest flag — match search's parameter shape.

---

## Acceptance criteria

### memoryweb

- `TestOrient_DigestMode_SingleLinePerSection` — significant/recent/rules at 2+ entries.
- orient uses `truncateWhyMatters` everywhere (no ad-hoc rune cuts remain).
- `TestAudit_DigestMode_Stale`, `_Orphans`, `_Archived` — multi-result digest lines.
- Digest opt-in, default off: omitting parameter preserves current JSON-array shape.
- Each line includes `id` for `recall(id)` follow-up.
- `go test ./...` green.

### Recordari

- STORY-156/157 ship lean + digest together as only behaviour (no flag).

---

## Files (expected)

### memoryweb

- `tools/orient.go`, `tools/archive.go` — digest rendering, parameter handling
- `tools/lean.go` — shared digest-line helper if not already present
- `tools/orient_test.go`, `tools/archive_test.go` — new tests

---

## References

- `stories/digest-mode.md` — field-pruning + serialisation-shape distinction
- `stories/lean-format-retrieval-tools.md` — lean entry precedent
- Live evidence: `story-gap-confirmed-by-live-smoke-test-digest-mode-lean-format-never-shipped-on-orient-or-audit-cab29c8b`
