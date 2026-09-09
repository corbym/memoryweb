# CR-45: Use index map for lifecycle resolution check

**Status:** READY
**Priority:** Low

`db/lifecycle.go:84-94` — `hasResolutionBetween` is O(E) per call, called once per
contradicts edge. Total O(C × E). An index map `pair → bool` would make it O(1).

---

## Acceptance criteria

- [ ] Build `resolvedPairs` map once before the loop
- [ ] `hasResolutionBetween` looks up in the map
- [ ] `TestLifecycle*` tests pass
