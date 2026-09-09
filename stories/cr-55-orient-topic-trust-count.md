# CR-55: Fix orient topic path missing trust count

**Status:** READY
**Priority:** Low

`tools/orient.go:240,336-353` — When `topic != ""`, `LoadBearingLowTrust` stays at 0.
The non-topic path computes it. This means topic-oriented orient never surfaces
trust warnings on relevant memories.

---

## Acceptance criteria

- [ ] Compute `LoadBearingLowTrust` in the topic path
- [ ] `TestOrient*Topic*` tests verify trust count
