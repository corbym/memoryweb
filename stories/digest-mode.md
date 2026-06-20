# Digest mode — render multi-node responses as single-line text, not arrays of JSON objects

**Status:** OPEN

**Shared-surface node:** `digest-mode-render-multi-node-re-8fa64dcf`
**Depends on:** `stories/lean-format-retrieval-tools.md` (COMPLETE for memoryweb) — digest mode
collapses the lean field set that story defines into a single text line per node; the field
set has to be decided first.

---

## Why

The lean-format story only prunes which **fields** are returned per node — `id`, `label`,
`why_matters` truncated at 150 chars, a `truncated` flag — but still serialises the result as
a JSON array of objects. Every entry repeats the key names (`"id":`, `"label":`,
`"why_matters":`, `"truncated":`) plus JSON's structural punctuation (brackets, quotes, commas)
on top of whatever fields survive the pruning.

Digest mode is the next, separate compression layer: collapse each lean-format node into a
single compact text line instead of a JSON object — e.g. `"[id] label — truncated excerpt
(domain, N edges)"` — for any tool returning a **list** of nodes. Candidates: orient's
`significant`/`recent` sections, `search`, `recent`, `significance`, `history` (the same tool
set named in the lean-format story, for both memoryweb and Recordari/STORY-120).

Field-pruning (lean format) and serialisation-shape (digest mode) are independent, stackable
levers — you can prune fields and still pay the full JSON key-repetition tax per entry, or
keep more fields and pay less tax by changing the shape. Full JSON node objects remain the
right shape only for single-target lookups (`recall(id)` / the Recordari equivalent) or any
caller that needs programmatically structured fields rather than an LLM agent reading prose.

JSON's per-field key repetition is itself a token cost independent of which fields are
selected. If the lean-format story is treated as "fewer JSON keys" without this distinction,
most of the achievable saving on any tool returning more than a couple of nodes is left on
the table.

---

## Scope

Apply a single-line text rendering to list-shaped results in the same tool set the lean-format
story covers:

- **orient** — `significant` and `recent` sections (and `declared_spine`, `rules` if the
  format generalises cleanly).
- **search** — the default (non-exact) path. `exact: true` stays full-content JSON, consistent
  with the lean-format story's carve-out.
- **recent** — all branches (memory_id scope, tags-only, group_by_domain, normal).
- **significance** — all four sections; `importance_score` needs a place in the line format.
- **history** — `occurred_at` needs a place in the line format (chronological ordering is the
  point of this tool).

`recall(id)` is unaffected — single-node lookups stay full JSON.

---

## Open questions (resolve at implementation)

1. **Exact line format.** Working draft: `"[id] label — truncated excerpt (domain, N edges)"`.
   Confirm field order and punctuation; decide whether `domain` is worth a token when the tool
   call was already domain-scoped.
2. **Opt-in or default?** A new parameter (e.g. `digest: true`) keeps the existing JSON-array
   contract as the default and avoids a breaking change to every caller; a new default
   behaviour maximises the saving but breaks any caller (including memoryweb's own tests)
   that parses the current JSON shape. Recommend opt-in via a parameter, default false,
   given how much of `tools_test.go` already asserts on JSON shape for these tools.
3. **Edge count.** `(domain, N edges)` implies counting connections per node cheaply at list
   time — confirm this doesn't reintroduce the N+1 query cost the lean-format story avoided by
   leaving `edges` as a separate top-level array (search) or omitting it entirely (recent,
   significance, history).
4. **importance_score / occurred_at placement.** significance and history both need to keep a
   value lean-format already preserves; the line format must make room for it without becoming
   unreadable.
5. **Per-tool or per-server flag?** Could be a parameter on each of the five tools, or a single
   server-wide response-mode setting. Per-tool matches how `exact` already works for search.
6. **Recordari (STORY-120).** Same tool set, same two-lever distinction — confirm Recordari
   picks up digest mode as a follow-on to its own lean-format work, not bundled into it.

---

## Acceptance criteria

*(Defined at implementation time — these are current expectations)*

- `TestSearch_DigestMode_SingleLinePerNode` (and equivalents for recent, significance,
  history, and orient's significant/recent sections).
- Digest mode is opt-in: omitting the new parameter preserves the current JSON-array
  response exactly (no regression to existing lean-format tests).
- Each line includes `id` (so a caller can still follow up with `recall(id)`).
- `go test ./...` green.

---

## Files (expected)

- `tools/tools.go` — digest-line rendering helper(s); new parameter handling for `search`,
  `recent`, `significance`, `history`, and orient's `significant`/`recent` sections; tool
  description updates documenting the new parameter.
- `tools/tools_test.go` — new tests above.

---

## References

- Shared-surface node: `digest-mode-render-multi-node-re-8fa64dcf`
- Predecessor / field-set dependency: `stories/lean-format-retrieval-tools.md`,
  shared-surface node `orient-lean-field-format-id-labe-d1cb5704`
- Recordari: STORY-120 (EPIC-001)
