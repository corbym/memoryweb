# CR-28: Remove dead code paths

**Status:** READY
**Priority:** Medium

Three dead code locations:
- `tools/revise.go:328-347` — `updateNodes` (old `revise_all` format) never called
- `tools/graph.go:42-62` — `tracePath` unreachable (switch case returns error)
- `tools/tools.go:145-146` — `check_for_updates` case returns "unknown tool"

---

## Acceptance criteria

- [ ] Remove `updateNodes` function
- [ ] Remove `tracePath` function
- [ ] Remove `check_for_updates` switch case (or wire it properly)
- [ ] Remove `checkForUpdates` method if unused
- [ ] `go vet ./...` passes
- [ ] All tests pass
