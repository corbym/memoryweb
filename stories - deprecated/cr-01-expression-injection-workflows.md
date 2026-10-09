# CR-01: Fix expression injection in GitHub Actions workflows

**Status:** DONE — implemented this session. Both workflows now pass `inputs.*` through `env:` blocks. `go vet`/`go test` green.

`workflow_dispatch` inputs are interpolated directly into shell via `${{ inputs.* }}`.
A malicious input value executes arbitrary commands on the runner.

---

## Affected files

- `.github/workflows/release.yml` line 40: `FILTER="${{ inputs.os_filter }}"`
- `.github/workflows/backfill-release-hooks.yml` line 29: `"${{ inputs.tag }}"`

---

## Acceptance criteria

- [ ] `release.yml`: `inputs.os_filter` passed through `env:` block, referenced as `$FILTER` in shell
- [ ] `backfill-release-hooks.yml`: `inputs.tag` passed through `env:` block, referenced as `$TAG` in shell
- [ ] No other workflow uses `${{ inputs.* }}` directly in shell (grep check)
- [ ] `go vet ./...` and `go test ./...` pass (no code change, but CI should be green)

---

## Notes

This is the #1 priority finding. GitHub expression injection is actively exploitable by anyone with `workflow_dispatch` permission.
