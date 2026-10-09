# CR-06: Fix alias misdomain detection in remember_extras

**Status:** DONE — no code change needed; finding was stale. `Store.DomainExists` has resolved aliases internally since it was introduced (`db/domains.go:248-249`, commit `d0eecf8`, v1.41.0), so `snapshotDomainExistence` calling `DomainExists(domain)` already checks the canonical domain. Storage also canonicalizes: `AddNodesBatch`/`AddNode` resolve aliases on write, and `FindMisdomainCandidate` resolves the requested domain and skips same-domain candidates — so alias writes can never false-positive the misdomain warning regardless of the existence snapshot. New regression test `TestRemember_Batch_AliasDomain_NoFalseMisdomain` passes on current code.
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
