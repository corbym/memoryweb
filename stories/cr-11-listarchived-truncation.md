# CR-11: Fix listArchived truncation detection

**Status:** READY
**Priority:** High

`tools/archive.go:66` — `listArchived` fetches exactly `Limit` nodes, then checks
`len(nodes) > Limit` for truncation. This never triggers. Other audit modes (drift,
conflicts) fetch `Limit+1` to reliably detect truncation.

---

## Acceptance criteria

- [ ] `listArchived` fetches `Limit+1` nodes
- [ ] Truncation check: `len(nodes) > params.Limit`
- [ ] Existing `TestArchive*` tests pass
