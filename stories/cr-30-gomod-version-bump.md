# CR-30: Bump go.mod minimum version

**Status:** READY
**Priority:** Medium

`go.mod:3` — `go 1.22.5` with `toolchain go1.24.13`. The minimum directive is 14 minor
versions behind the toolchain. Anyone building with Go 1.22.x gets an auto-download or error.

---

## Acceptance criteria

- [ ] Bump `go` directive to match the minimum version actually tested against (likely 1.24.x)
- [ ] Verify `go build ./...` and `go test ./...` pass with the new minimum
- [ ] Update README build instructions if they reference a Go version
