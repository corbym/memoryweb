# CR-49: Add domainsList truncation signal

**Status:** READY
**Priority:** Low

`tools/domains.go:70-84` — `domainsList` returns unbounded results. No truncation
mechanism for workspaces with hundreds of domains.

---

## Acceptance criteria

- [ ] Fetch `limit+1` domains
- [ ] Return `results_truncated` boolean in response
- [ ] Existing `TestDomains*` tests pass
