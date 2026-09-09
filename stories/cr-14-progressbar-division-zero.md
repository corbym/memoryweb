# CR-14: Fix drawProgressBar division by zero

**Status:** READY
**Priority:** High

`main.go:387` — `drawProgressBar` computes `float64(done) / float64(total)` with no
guard for `total == 0`. Returns `NaN`, printing a garbage percentage.

---

## Acceptance criteria

- [ ] Guard: if `total == 0`, render 0% or skip percentage display
- [ ] New test: `TestProgressBar_ZeroTotal` — no NaN in output
- [ ] Existing progress bar tests pass
