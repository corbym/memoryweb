# STORY-010: orient(topic=...) without domain silently drops topic — return error or run workspace-wide search

**Type:** bug

## Goal

tools/orient.go summariseDomain (~line 441): when params.Domain == "" && len(params.Domains) == 0, the function immediately returns orientCrossDomain(params.Limit, params.Digest) before params.Topic is ever read. orientCrossDomain takes (limit int, digest bool) and has no topic parameter. The topic is silently dropped; callers receive a plain domain-list snapshot with no indication the topic was ignored.

Fix (option A — simpler, ship first):
- When params.Topic != "" and params.Domain == "" and len(params.Domains) == 0, return a tool error: "topic requires a domain — pass domain=X or omit topic for the cross-domain bootstrap snapshot".
- Error message should suggest calling domains() first if the caller doesn't know the domain.

Fix (option B — more useful, can be a follow-up story):
- Run a workspace-wide semantic search for the topic and return a relevant section across domains, analogous to orientWithTopic but unscoped.

Option A is the minimum viable fix: silent wrong behaviour is worse than a clear error. Implement option A in this story; file option B as a separate feature story if desired.

Refs: tools/orient.go:420 (summariseDomain), tools/orient.go:62 (orientCrossDomain), tools/orient.go:167 (orientWithTopic)

## Acceptance criteria

- [x] AC-STORY-010-26c90750: orient(topic=X) with no domain and no domains array returns a tool error — the topic is not silently ignored
- [x] AC-STORY-010-5042b1e9: The error message names the dropped topic and suggests passing domain=X or calling domains() first
- [x] AC-STORY-010-941aa1da: orient() with no topic and no domain continues to return the cross-domain bootstrap snapshot unchanged
- [x] AC-STORY-010-6af91d0f: orient(topic=X, domain=Y) continues to work as before
- [x] AC-STORY-010-a2794e64: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-16T18:18:23Z -->
Dev notes:
- Bug location: tools/orient.go summariseDomain (~line 441). When params.Domain == "" && len(params.Domains) == 0, it immediately returns orientCrossDomain(params.Limit, params.Digest) before params.Topic is ever read. orientCrossDomain takes (limit int, digest bool) — no topic parameter exists.
- Fix: add a guard before the early return — if params.Topic != "" { return errorResult("orient: topic requires a domain — pass domain=X or omit topic for the cross-domain snapshot; call domains() first if unknown") }.
- Option B (workspace-wide topic search with no domain) is a separate feature story; do not implement here.
- Skill update: document the error in the orient tool reference section.

<!-- backlog-mcp: 2026-11-16T20:01:33Z -->
Added guard in tools/orient.go before the cross-domain early return: when topic is non-empty and no domain is given, returns a tool error naming the dropped topic and suggesting domain=X or calling domains() first. Two tests added (error case + no-topic cross-domain unchanged). Skill quick-reference table updated. STORY-014 filed for Option B (workspace-wide semantic search when topic given without domain).
