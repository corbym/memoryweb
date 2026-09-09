# CR-13: Fix setupStartOllama false success on timeout

**Status:** DONE — implemented this session. `setupStartOllama` now returns an error and the "started." fallthrough is gone: the poll loop was extracted to `waitForOllamaReady(client, url, deadline)` which returns a real readiness bool, and on timeout it prints `failed: Ollama server did not become ready within 30s` and returns a non-nil error. Chain updated: `setupOllama` returns the error, `runSetup` propagates it, `setupCmd` prints `error:` + exit 1. New tests: `TestWaitForOllamaReady_Reachable` (httptest server → true) and `TestWaitForOllamaReady_UnreachableExpiredDeadline` (false, returns immediately). All `TestSetup*` tests pass.
**Priority:** High

`main.go:805` — When the 30s polling loop exhausts without a successful Ollama connect,
the function falls through to `fmt.Fprintln(out, "started.")` — misleading the user.

---

## Acceptance criteria

- [ ] Track whether a successful connect occurred inside the loop
- [ ] On timeout without connect, print an error message (not "started.")
- [ ] Return a non-nil error or exit code
- [ ] `TestSetup*` tests pass
