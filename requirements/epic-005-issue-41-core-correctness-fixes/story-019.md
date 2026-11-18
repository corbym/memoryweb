# STORY-019: Fix stale digest dropping placeholder fields; add tie-breaking and digest shape tests

**Type:** bug

## Goal

Commit 8bf8431 added omitempty to auditStaleDigestResult.PlaceholderLines and .PlaceholdersTruncated to fix the orphans digest shape. That change also silently dropped both fields from audit(mode=stale, digest=true) responses whenever PlaceholdersTruncated is false (always omitted) or PlaceholderLines is empty (omitted when len=0). Callers cannot distinguish "not truncated" from "field absent". Fix by using a separate struct for orphans digest and removing omitempty from the stale struct. Also add the missing test for deterministic tie-breaking in FindMisdomainCandidate (noted in v1.67.3 release notes but not shipped).

## Acceptance criteria

- [ ] AC-STORY-019-b11419f5: audit(mode=stale, digest=true) always returns all four keys: lines, results_truncated, placeholder_lines, placeholders_truncated — even when placeholder_lines is [] and placeholders_truncated is false
- [ ] AC-STORY-019-26aec2c3: audit(mode=orphans, digest=true) returns exactly two keys: lines and results_truncated — no placeholder fields
- [ ] AC-STORY-019-0702c2ff: TestAudit_StaleDigest_AllFourKeysPresent_Empty passes (four keys on empty domain)
- [ ] AC-STORY-019-b33820fa: TestAudit_StaleDigest_AllFourKeysPresent_CandidatesNoPlaceholders passes (four keys with candidates, no placeholders)
- [ ] AC-STORY-019-1f2cd227: TestAudit_OrphansDigest_NonEmpty_ExactlyTwoKeys passes (exactly two keys with orphans present)
- [ ] AC-STORY-019-05815b9d: TestFindMisdomainCandidate_TieBreaking_Deterministic passes (same domain across 20 iterations)
- [ ] AC-STORY-019-c3a76b29: go test ./... is green
- [ ] AC-STORY-019-d8078978: docs/memoryweb-skill.md Last verified line matches the release tag
