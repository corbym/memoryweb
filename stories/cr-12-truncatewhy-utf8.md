# CR-12: Fix truncateWhy UTF-8 byte boundary split

**Status:** READY
**Priority:** High

`tools/lean.go:55` — `s[:150]` operates on bytes. For multi-byte UTF-8 characters
(CJK, emoji), this splits in the middle of a character, producing invalid UTF-8.

---

## Acceptance criteria

- [ ] `truncateWhy` uses `[]rune(s)[:limit]` instead of `s[:limit]`
- [ ] New test: `TestTruncateWhy_MultiByte` — CJK/emoji string truncated at rune boundary
- [ ] Existing `TestTruncateWhy*` tests pass
