# CR-12: Fix truncateWhy UTF-8 byte boundary split

**Status:** DONE — implemented this session. `truncateWhy` now operates on `[]rune(s)` with a rune-count budget and rune-safe boundary scan; no more byte-level splits of multi-byte UTF-8. New tests: `TestSearch_LeanFormat_WhyMattersMultiByteTruncated` and `TestSearch_LeanFormat_WhyMattersMultiByteUnderLimit` (both failed on the old byte-slice code). All tests pass.
**Priority:** High

`tools/lean.go:55` — `s[:150]` operates on bytes. For multi-byte UTF-8 characters
(CJK, emoji), this splits in the middle of a character, producing invalid UTF-8.

---

## Acceptance criteria

- [ ] `truncateWhy` uses `[]rune(s)[:limit]` instead of `s[:limit]`
- [ ] New test: `TestTruncateWhy_MultiByte` — CJK/emoji string truncated at rune boundary
- [ ] Existing `TestTruncateWhy*` tests pass
