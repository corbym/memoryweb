# Digest mode — render multi-node responses as single-line text, not arrays of JSON objects

**Status:** COMPLETE

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
2. **Edge count.** `(domain, N edges)` implies counting connections per node cheaply at list
   time — confirm this doesn't reintroduce the N+1 query cost the lean-format story avoided by
   leaving `edges` as a separate top-level array (search) or omitting it entirely (recent,
   significance, history).
3. **importance_score / occurred_at placement.** significance and history both need to keep a
   value lean-format already preserves; the line format must make room for it without becoming
   unreadable.
4. **Per-tool or per-server flag (memoryweb)?** Could be a parameter on each of the five tools,
   or a single server-wide response-mode setting. Per-tool matches how `exact` already works
   for search.

---

## Resolved design questions

- **Opt-in vs. default — and it's different per product.** A new parameter (e.g. `digest:
  true`) that defaults to off keeps the existing JSON-array contract for every current caller;
  defaulting it on maximises the saving but silently changes the response shape for anyone not
  already passing the flag. The tell that this is a real breaking change, not a safe default
  flip: if shipping it requires retrofitting every existing test with an explicit "give me the
  old shape" flag just to keep them passing, that's evidence real callers need the same escape
  hatch — and most of them won't know to ask for it.

  This argument is asymmetric across the two products, though:
  - **memoryweb**: opt-in, default **off**. `search`/`recent`/`significance`/`history` already
    have a real, currently-shipped JSON-array contract (this repo's own test suite depends on
    it). Resolved: opt-in via a parameter, default false.
  - **Recordari**: default **on**, no flag needed. STORY-120 (lean format) hasn't shipped
    there yet, so nothing currently depends on any particular shape for these four tools —
    there is no "old behaviour" to protect. Recordari should implement lean-format and digest
    mode together as the only behaviour for these tools, rather than mirroring memoryweb's
    two-phase (lean format now, digest mode later, opt-in) rollout. The staged, cautious
    sequencing memoryweb needed exists specifically *because* memoryweb has an installed base
    on the current shape; Recordari doesn't, so the caution doesn't transfer.
  - If memoryweb's digest mode proves useful in practice, flipping its default later is a
    separate, deliberate decision — made with real usage data, documented as a breaking change
    in release notes per the standing release-process rule, not bundled into the initial ship.

---

## Acceptance criteria

*(Defined at implementation time — these are current expectations)*

### memoryweb

- `TestSearch_DigestMode_SingleLinePerNode` (and equivalents for recent, significance,
  history, and orient's significant/recent sections).
- Digest mode is opt-in, default off: omitting the new parameter preserves the current
  JSON-array response exactly (no regression to existing lean-format tests).
- Each line includes `id` (so a caller can still follow up with `recall(id)`).
- `go test ./...` green.

### Recordari

- Equivalent tests confirming digest-mode line output is the *only* response shape for
  these four tools — no flag, no fallback JSON-array mode to maintain.
- Lean-format field selection (STORY-120) and digest-mode line rendering ship together,
  not as separate sequential stories.

---

## Files (expected)

### memoryweb

- `tools/tools.go` — digest-line rendering helper(s); new parameter handling for `search`,
  `recent`, `significance`, `history`, and orient's `significant`/`recent` sections; tool
  description updates documenting the new parameter.
- `tools/tools_test.go` — new tests above.

### Recordari

- `internal/adapters/repository/supabase/search.go`, `timeline.go`, `significance.go` (and
  whatever backs `recent`) — combined lean-entry mapping + digest-line rendering, no flag.
- Equivalent tool description updates.

---

## References

- Shared-surface node: `digest-mode-render-multi-node-re-8fa64dcf`
- Predecessor / field-set dependency: `stories/lean-format-retrieval-tools.md`,
  shared-surface node `orient-lean-field-format-id-labe-d1cb5704`
- Recordari: STORY-120 (EPIC-001)
