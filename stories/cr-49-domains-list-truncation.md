# CR-49: Add domainsList truncation signal

**Status:** DONE
**Priority:** Low

`tools/domains.go:70-84` — `domainsList` returns unbounded results. No truncation
mechanism for workspaces with hundreds of domains.

---

## Acceptance criteria

- [x] `ListDomainsLimited(limit)` fetches `limit+1` domains from DB
- [x] Return `results_truncated` boolean in `domainsList` response (default limit 200)
- [x] Existing `TestDomains*` tests pass; `TestDomainsList_TruncationSignal` added
