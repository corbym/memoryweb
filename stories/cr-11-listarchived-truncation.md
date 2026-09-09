# CR-11: Fix listArchived truncation detection

**Status:** DONE — no code change needed; finding was stale. `ListArchived` already fetches `limit+1` internally (`db/nodes.go:641`, shipped v1.40.0), so `len(nodes) > params.Limit` in `listArchived` DOES trigger. Verified by passing `TestAudit_Archived_DefaultLimit25` and `TestAudit_Archived_RaiseLimitReturnsMore`.
**Priority:** High

`tools/archive.go:66` — `listArchived` fetches exactly `Limit` nodes, then checks
`len(nodes) > Limit` for truncation. This never triggers. Other audit modes (drift,
conflicts) fetch `Limit+1` to reliably detect truncation.

---

## Acceptance criteria

- [ ] `listArchived` fetches `Limit+1` nodes
- [ ] Truncation check: `len(nodes) > params.Limit`
- [ ] Existing `TestArchive*` tests pass
