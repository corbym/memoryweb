# CR-37: Return empty slice instead of nil for Edges

**Status:** DONE
**Priority:** Low

`db/nodes.go:148` — `Edges` field is nil (not `[]Edge{}`) when no edges exist.
JSON serializes this as `null` instead of `[]`.

---

## Acceptance criteria

- [x] Initialize `Edges` to `[]Edge{}` when no edges found
- [x] JSON output uses `[]` not `null`
