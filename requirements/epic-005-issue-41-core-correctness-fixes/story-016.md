# STORY-016: Misdomain check: exclude batch siblings from KNN check to prevent cross-item false flags

**Type:** bug

## Goal

In addNodesBatch, all nodes are stored before rememberFilingExtras runs per node. Each item's KNN query therefore sees the other items from the same call, so a co-filed node with identical text in a different domain can anchor a false-positive misdomain flag. Fix: pass batch sibling IDs as excludeIDs to FindMisdomainCandidate.

## Acceptance criteria

- [x] AC-STORY-016-37b7f077: FindMisdomainCandidate accepts excludeIDs []string; excluded IDs do not appear in KNN results
- [x] AC-STORY-016-eec01144: addNodesBatch passes each node's batch siblings as excludeIDs so a co-filed item in a different domain cannot anchor a false-positive misdomain flag
- [x] AC-STORY-016-81acd823: Single-node path (addNodeSingle) passes nil for excludeIDs — no behaviour change
- [x] AC-STORY-016-c212cee4: New test: TestRemember_NoPossibleMisdomain_BatchSiblingsExcluded
- [x] AC-STORY-016-f480bd09: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-18T08:56:24Z -->
Added excludeIDs []string parameter to FindMisdomainCandidate. addNodesBatch (tools/remember.go) now collects all batch node IDs and passes each node's siblings as excludeIDs. rememberFilingExtras and checkMisdomainCandidate updated to thread excludeIDs through. Single-node path passes nil. Test: TestRemember_NoPossibleMisdomain_BatchSiblingsExcluded. Shipped in v1.67.2.
