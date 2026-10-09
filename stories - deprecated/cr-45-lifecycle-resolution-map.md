# CR-45: Use index map for lifecycle resolution check

**Status:** DONE
**Priority:** Low

`db/lifecycle.go:84-94` — `hasResolutionBetween` is O(E) per call, called once per
contradicts edge. Total O(C × E). An index map `pair → bool` would make it O(1).

---

## Acceptance criteria

- [x] Build `resolvedPairs` map once before the loop
- [x] `hasResolutionBetween` looks up in the map
- [x] `TestLifecycle*` tests pass
