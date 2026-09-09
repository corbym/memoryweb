# CR-29: Add race detector and test timeout to CI

**Status:** READY
**Priority:** Medium

`ci.yml:44` — `go test ./...` without `-race` flag misses data races. Also no `-timeout`
flag — a hanging test blocks the runner for 6 hours.

---

## Acceptance criteria

- [ ] Add `-race` flag to CI test command
- [ ] Add `-timeout 120s` to CI test command
- [ ] Add `-count=1` to prevent stale cache
- [ ] CI pipeline passes with new flags
