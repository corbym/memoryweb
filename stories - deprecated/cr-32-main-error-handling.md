# CR-32: Handle ignored errors in main.go

**Status:** DONE
**Priority:** Low

`main.go:1559` — `json.Unmarshal(req.Params, &callReq)` error discarded. Blank tool
name recorded in stats on malformed params.

`main.go:146-152` — Signal goroutine calls `os.Exit(0)` — abrupt termination mid-RPC.

---

## Acceptance criteria

- [x] `json.Unmarshal` error logged or returns parse error to client
- [x] Signal handler closes stdin; main loop exits cleanly and defers run
