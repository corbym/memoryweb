# CR-54: Document security review staleness

**Status:** READY
**Priority:** Low

`security-review.md:5` — References commit `0fe3391` but findings may be stale.
Key findings like F-1 (curl|sh) and F-4 (no HTTP timeout) should be verified.

---

## Acceptance criteria

- [ ] Update security-review.md with current commit hash
- [ ] Mark findings as open/closed based on current code
- [ ] Add `govulncheck` and `gosec` to CI (per F-12 recommendation)
