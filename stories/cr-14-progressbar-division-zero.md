# CR-14: Fix drawProgressBar division by zero

**Status:** DONE — implemented this session. `drawProgressBar` guards `total > 0` before dividing; `total == 0` renders `[>   ...] 0/0 (0%)` with zero percentage (no NaN). New `TestDrawProgressBar_ZeroTotal` (fails on old NaN output, passes now); `TestDrawProgressBar_Format/Complete/First` unchanged and passing.
**Priority:** High

`main.go:387` — `drawProgressBar` computes `float64(done) / float64(total)` with no
guard for `total == 0`. Returns `NaN`, printing a garbage percentage.

---

## Acceptance criteria

- [ ] Guard: if `total == 0`, render 0% or skip percentage display
- [ ] New test: `TestProgressBar_ZeroTotal` — no NaN in output
- [ ] Existing progress bar tests pass
