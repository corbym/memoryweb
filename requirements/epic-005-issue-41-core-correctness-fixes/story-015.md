# STORY-015: Misdomain check: implement majority-rule to suppress false positives on correctly filed memories

**Type:** bug

## Goal

FindMisdomainCandidate returns the first cross-domain neighbour within the similarity floor, even if most neighbours are in the requested domain. This causes false-positive misdomain flags on correctly filed memories that happen to have a minority of cross-domain neighbours. Fix: count per-domain amongst the top-20 in-floor neighbours; flag only if another domain has strictly more neighbours than the requested domain.

## Acceptance criteria

- [x] AC-STORY-015-291ad7f5: FindMisdomainCandidate returns nil when the requested domain has a majority of KNN neighbours, even when a minority cross-domain neighbour exists within the similarity floor
- [x] AC-STORY-015-7a3c9123: FindMisdomainCandidate still flags when the requested domain has zero in-floor neighbours and another domain has at least one (new-domain case preserved)
- [x] AC-STORY-015-caf25713: Tie between two non-requested domains resolved deterministically by nearest distance, not map iteration order
- [x] AC-STORY-015-dddc5c81: New tests: TestFindMisdomainCandidate_NilReturnedWhenMinority, TestFindMisdomainCandidate_ExcludeIDs_SuppressesFlag, TestRemember_NoPossibleMisdomain_MinorityCrossDomain
- [x] AC-STORY-015-fe5b5447: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-18T08:56:22Z -->
Implemented majority rule in FindMisdomainCandidate (db/graph.go). Counts KNN neighbours per domain within CandidateSimilarityFloor across the top-20 results; flags only when another domain has strictly more neighbours than the requested domain. Ties between non-requested domains are broken by nearest distance (deterministic). Tests: TestFindMisdomainCandidate_NilReturnedWhenMinority, TestFindMisdomainCandidate_ExcludeIDs_SuppressesFlag, TestRemember_NoPossibleMisdomain_MinorityCrossDomain. Shipped in v1.67.2.
