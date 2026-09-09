# CR-26: Enforce required fields in decodeParams framework

**Status:** DONE
**Priority:** Medium

`tools/util.go:44-49` — `decodeParams` only checks for unknown fields. It never inspects
the `Required` field from `InputSchema`. Every handler must manually call `requireNonEmpty`.
One missed handler accepts empty required fields.

---

## Acceptance criteria

- [ ] `decodeParams` accepts an optional list of required field names
- [ ] Required fields validated automatically before handler runs
- [ ] Remove redundant `requireNonEmpty` calls from handlers (keep for fields not in schema)
- [ ] Existing `TestDecodeParams*` tests pass
