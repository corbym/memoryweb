# suggest_connections keyword matcher treats em-dash and other punctuation as words

**Status:** COMPLETE (memoryweb)

**Shared-surface node:** `bug-suggestkeywords-treats-em-da-ff8dc44b`

---

## Why

`suggested_connections` reasons sometimes read nonsense like:

> "similar label words: —"

Observed live on a `remember` call's `suggested_connections` output.

Root cause is in `db/db.go`, `suggestKeywords()` (backs `SuggestEdges`, which powers
`remember`'s inline `suggested_connections`, the batch `remember` path, and the standalone
`suggest_connections` tool). Tokens come from `strings.Fields(strings.ToLower(text))`, are
trimmed with `strings.Trim(w, ".,!?;:-\"'()")` — an **ASCII-only** punctuation cutset — then
filtered by `len(w) < 3`, which measures **byte length**, not character count.

Multi-byte Unicode punctuation — em-dash `—` (U+2014), en-dash `–` (U+2013), curly quotes,
ellipsis `…`, bullets `•` — each encode to 3 bytes in UTF-8. A label like `"Digest mode —
render multi-node..."` produces `—` as its own token (flanked by spaces): it isn't in the
ASCII trim cutset, and its 3-byte length satisfies the `>= 3` filter despite carrying zero
word content. Two compounding bugs, either of which alone would have caught this:

1. The trim cutset only knows ASCII punctuation.
2. The length filter checks bytes, not runes/word content.

This fires often in practice, not just on edge-case input — em-dashes are used throughout
this codebase's own node-labelling convention (visible in existing shared-surface and
memoryweb-meta labels), so the bug pollutes a meaningful fraction of real
`suggested_connections` output with a meaningless "shared word" justification.

Cross-cutting: Recordari implements its own version of this keyword-similarity matcher and
likely has the same bug if it followed the same approach — worth checking when picked up
there.

---

## Change

### db/db.go — `suggestKeywords` (around line 2538)

Current:

```go
addWords := func(text string) {
	for _, w := range strings.Fields(strings.ToLower(text)) {
		w = strings.Trim(w, ".,!?;:-\"'()")
		if len(w) < 3 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
}
```

Replace the trim and length check:

```go
addWords := func(text string) {
	for _, w := range strings.Fields(strings.ToLower(text)) {
		w = strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if utf8.RuneCountInString(w) < 3 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
}
```

`TrimFunc` with a letter-or-number predicate strips *any* leading/trailing punctuation or
symbol — ASCII or Unicode — instead of a hardcoded ASCII set. A token made entirely of
punctuation (like a standalone em-dash) collapses to the empty string, which the
`RuneCountInString(w) < 3` check then catches for free. Switching to `RuneCountInString`
also fixes the underlying byte-vs-rune mismatch generally, not just for this one symbol.

Requires adding `"unicode"` and `"unicode/utf8"` to the `db` package imports (check `strings`
is already imported — it is, used elsewhere in this function).

No change needed to the ASCII punctuation it already handled correctly (e.g. trailing `:` on
a tag, leading/trailing quotes) — `TrimFunc` with this predicate subsumes the old cutset.

---

## Tests (db/db_test.go)

Mirror the existing `TestSuggestEdges_*` pattern (outside-in via `Store.SuggestEdges`, not
the unexported `suggestKeywords` — `db_test` is an external test package per this repo's
testing convention):

- `TestSuggestEdges_EmDashNotMatchedAsSharedWord`: file two nodes in the same domain whose
  labels both contain `" — "` (e.g. `"Alpha — first node"` and `"Beta — second node"`) but
  share no other vocabulary. Call `SuggestEdges` for one against the other; assert either no
  suggestion is returned, or if one is returned for an unrelated reason, the `Reason` string
  does not contain `"—"`.
- `TestSuggestEdges_EnDashAndEllipsisNotMatchedAsSharedWord`: same shape, using `–` (en-dash)
  and `…` (ellipsis) in place of the em-dash.
- `TestSuggestEdges_RealWordsStillMatch`: regression guard — two nodes sharing an actual
  3+ letter word still produce a `"similar label words"` reason containing that word. Confirms
  the fix didn't over-correct and break the existing matching behaviour covered by
  `TestSuggestEdges_ReturnsOverlappingTagsNode` and friends.

TDD applies per the standing rule — write these failing first, then apply the change above.

---

## Acceptance criteria

- The three tests above pass.
- All existing `TestSuggestEdges_*` tests in `db/db_test.go` still pass unmodified.
- `go test ./...` green.

---

## Files

- `db/db.go` — `suggestKeywords()`
- `db/db_test.go` — three new tests above

---

## References

- Shared-surface node: `bug-suggestkeywords-treats-em-da-ff8dc44b`
- Same feature surface: `story-122-shipped-suggest-connec-6ce7a69e`,
  `suggested-connections-in-remembe-2330807b`
