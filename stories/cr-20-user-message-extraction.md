# CR-20: Fix user message extraction truncation

**Status:** READY
**Priority:** Medium

`hooks/userpromptsubmit_hook.sh:90-92` — grep regex `"\"message\"..."` matches only
to the first unescaped double quote. User message `use "querySelector"` gets silently
truncated to `use`.

---

## Acceptance criteria

- [ ] Replace grep-based extraction with `jq` if available, falling back to current regex
- [ ] Document the limitation in code comments for the fallback path
- [ ] New test case in `hooks_test.go` with embedded quotes in message
