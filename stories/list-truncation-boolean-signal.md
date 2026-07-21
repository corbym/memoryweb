# List truncation — boolean more-available signal + Category A/B contract

**Status:** COMPLETE

**Shared-surface node:** `list-shaped-responses-that-trunc-941da40a` (standing rule)

**Partial coverage:** `stories/search-vocabulary-gap.md` (search `truncated` only)

---

## Why

Any tool returning an array that can be shorter than underlying data must expose:

1. A derived **`truncated`/`results_truncated` boolean** — `len(results) == limit`, no COUNT(*).
2. A **concrete retrieval path** — what to do next.

The boolean alone is insufficient for **Category B** tools where completeness is the
point (significance declared/potentially_stale, audit stale sweep). A hard cap with no
"raise limit and get more" path is silent data loss, not token efficiency.

**Category A** (curated by design): orient significant/recent/spine, search ranked results.
Truncation is intentional — boolean + "use search instead" is enough.

**Category B** (completeness-is-the-point): significance declared/potentially_stale/uncurated,
audit multi-result modes. Must support raising `limit` to enumerate full set.

Also: do not reuse `truncated` for two meanings — per-node excerpt cut vs list-level
omission. Use distinct names: `excerpt_truncated` vs `results_truncated`.

---

## Gap audit (current memoryweb state)

| Tool / section | List-level boolean | Category | Raise-limit path |
|----------------|-------------------|----------|------------------|
| search | ✅ `truncated` | A | ✅ limit param |
| recent | ❓ verify | A | ✅ limit param |
| orient significant/recent/spine | ❌ silent caps | A | document search fallback |
| significance declared/potentially_stale | ❌ (STORY-123 Recordari) | B | needs uncapped retry |
| audit stale/orphans/archived | ❌ | B | needs limit param + boolean |
| history | ❓ verify | A/B depends on mode | ✅ limit param |

---

## Changes

### Per tool

1. **orient** — add `results_truncated` per section (significant, recent, declared_spine,
   rules) when section hits cap. Description documents Category A recovery ("use search").
2. **significance** — Category B: remove artificial ceiling on declared/potentially_stale
   OR ensure `limit` extends without silent cap; add `results_truncated`.
3. **audit** — add `limit` param if missing; `results_truncated` on all modes.
4. **recent/history** — verify boolean present; align field naming.

### Field naming

- Per-node: keep `truncated` on lean entries (excerpt was cut) — or rename to
  `excerpt_truncated` in a coordinated breaking change.
- List-level: `results_truncated` at section or response root.

---

## Acceptance criteria

- Every capped list section returns `results_truncated: true|false`.
- Category B tools: test raising limit returns strictly more items until exhausted.
- Distinct field names documented in tool descriptions.
- No total_count / cursor pagination (explicitly out of scope per closed decision).
- `go test ./...` green.

---

## Files (expected)

- `tools/orient.go`, `tools/significance.go`, `tools/archive.go`, `tools/recent.go`
- Matching test files — one truncation signal test per Category B tool

---

## References

- Standing rule: `list-shaped-responses-that-trunc-941da40a`
- Boundary: `design-gap-pagination-and-total--bb6445a6` (no total_count)
- Recordari: STORY-123 (significance declared/potentially_stale cap)
