# STORY-016: Misdomain check: exclude batch siblings from KNN check to prevent cross-item false flags

**Type:** bug

## Goal

In addNodesBatch, all nodes are stored before rememberFilingExtras runs per node. Each item's KNN query therefore sees the other items from the same call, so a co-filed node with identical text in a different domain can anchor a false-positive misdomain flag. Fix: pass batch sibling IDs as excludeIDs to FindMisdomainCandidate.

## Acceptance criteria

- [ ] Define acceptance criteria
