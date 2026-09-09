# CR-13: Fix setupStartOllama false success on timeout

**Status:** READY
**Priority:** High

`main.go:805` — When the 30s polling loop exhausts without a successful Ollama connect,
the function falls through to `fmt.Fprintln(out, "started.")` — misleading the user.

---

## Acceptance criteria

- [ ] Track whether a successful connect occurred inside the loop
- [ ] On timeout without connect, print an error message (not "started.")
- [ ] Return a non-nil error or exit code
- [ ] `TestSetup*` tests pass
