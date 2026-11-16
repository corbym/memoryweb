# STORY-011: Misdomain check: run KNN check on all filings, not only new-domain creation

**Type:** bug

## Goal

tools/remember_extras.go:25-27: `if domainExisted { return out }` causes rememberFilingExtras to return early without calling checkNewDomainMisdomain. The KNN misdomain check (FindMisdomainCandidate) is dead code for every filing into an existing domain — the overwhelming majority of real filings.

Fix:
- Remove the early-return guard (or replace it with logic that runs the KNN check regardless of domainExisted).
- Rename checkNewDomainMisdomain → checkMisdomainCandidate and generalise LogDomainCreationFlagged → LogMisdomainFlagged (or add a reason field) so the audit log entry is accurate for both cases.
- The KNN check already has a similarity threshold in FindMisdomainCandidate; no threshold change needed.
- Batch path (addNodesBatch) calls rememberFilingExtras per node via domainSnap — same fix applies automatically once the guard is removed.
- Add a test: file a node into an existing-but-wrong domain whose nearest neighbours are overwhelmingly in a different domain; assert possible_misdomain=true.

Refs: tools/remember_extras.go:17 (rememberFilingExtras), tools/remember_extras.go:338 (checkNewDomainMisdomain), db/search.go (FindMisdomainCandidate)

## Acceptance criteria

- [ ] AC-STORY-011-0d2b73be: remember returns possible_misdomain=true when a node is filed into an existing domain whose nearest neighbours are overwhelmingly in a different domain
- [ ] AC-STORY-011-3c81b0fa: remember does not return possible_misdomain=true when filed into the correct domain
- [ ] AC-STORY-011-e7f31b8b: Batch remember (items array) surfaces possible_misdomain per node using the same check
- [ ] AC-STORY-011-003c5ea3: The audit log records a misdomain flag event for both new-domain and existing-domain filings
- [ ] AC-STORY-011-3ca50722: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-16T18:18:26Z -->
Dev notes:
- Bug location: tools/remember_extras.go:25-27 — `if domainExisted { return out }` causes the KNN check to be skipped for every filing into an existing domain.
- Fix: remove the early-return guard so checkNewDomainMisdomain (which calls db.FindMisdomainCandidate) runs on all filings.
- Rename checkNewDomainMisdomain → checkMisdomainCandidate and LogDomainCreationFlagged → LogMisdomainFlagged (or add a reason parameter) so the audit log entry is accurate for both cases.
- Batch path: addNodesBatch calls rememberFilingExtras per node via domainSnap — fix applies automatically once the guard is removed.
- KNN threshold already lives in FindMisdomainCandidate (db/search.go) — no threshold change needed in this story.
