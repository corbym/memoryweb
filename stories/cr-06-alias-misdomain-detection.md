# CR-06: Fix alias misdomain detection in remember_extras

**Status:** READY
**Priority:** High

`tools/remember_extras.go:48` — `DomainExists(domain)` called with the original unresolved
alias, not the resolved canonical name. If `domain` is an alias, `DomainExists` returns
`false` even though the canonical domain exists, causing false misdomain warnings.

---

## Current code

```go
resolved := hnd.store.ResolveAlias(domain)
// ...
hnd.store.DomainExists(domain)  // BUG: should be DomainExists(resolved)
```

---

## Acceptance criteria

- [ ] `snapshotDomainExistence` passes `resolved` to `DomainExists`
- [ ] New test: alias domain resolves correctly — no false misdomain warning
- [ ] Existing `TestRemember*` tests pass
