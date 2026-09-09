# CR-52: Deduplicate integration test scope

**Status:** DONE
**Priority:** Low

`integration.yml:63` — `go test ./... -run TestSearchSemantic_` runs ALL tests (non-matching
tests still compile and link). Should be `go test ./tools/... -run TestSearchSemantic_`.

---

## Acceptance criteria

- [x] Narrow test scope to `./tools/...` matching the existence check
