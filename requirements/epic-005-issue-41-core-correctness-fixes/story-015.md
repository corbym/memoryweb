# STORY-015: Misdomain check: implement majority-rule to suppress false positives on correctly filed memories

**Type:** bug

## Goal

FindMisdomainCandidate returns the first cross-domain neighbour within the similarity floor, even if most neighbours are in the requested domain. This causes false-positive misdomain flags on correctly filed memories that happen to have a minority of cross-domain neighbours. Fix: count per-domain amongst the top-20 in-floor neighbours; flag only if another domain has strictly more neighbours than the requested domain.

## Acceptance criteria

- [ ] Define acceptance criteria
