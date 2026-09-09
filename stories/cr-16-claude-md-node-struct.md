# CR-16: Update CLAUDE.md stale Node struct

**Status:** READY
**Priority:** High

`CLAUDE.md:160` — Shows `Transient bool` but migration 12 replaced it with `node_kind TEXT`.
The struct in CLAUDE.md is stale and will confuse agents editing the codebase.

---

## Acceptance criteria

- [ ] Update Node struct in CLAUDE.md to show `NodeKind string` (or `node_kind`)
- [ ] Remove `Transient bool` field
- [ ] Verify against actual `db/nodes.go` Node struct
