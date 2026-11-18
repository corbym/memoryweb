# STORY-018: audit(mode=orphans, digest=true) returns wrong shape when no orphans

**Type:** bug

## Goal

In findDisconnected (tools/archive.go), the len(nodes)==0 early return runs before the params.Digest branch, so digest=true with no orphans returns {nodes:[], results_truncated:false} instead of the promised {lines:[], results_truncated:false}.

## Acceptance criteria

- [x] AC-STORY-018-fa9e2139: audit(mode=orphans, digest=true) with no orphans returns {lines:[], results_truncated:false} not {nodes:[], results_truncated:false}
- [x] AC-STORY-018-7963b8a2: auditStaleDigestResult placeholder_lines and placeholders_truncated fields carry omitempty so orphans digest never emits spurious placeholder fields
- [x] AC-STORY-018-b1fed400: New test: TestAudit_OrphansDigestEmptyReturnsLinesShape
- [x] AC-STORY-018-03146477: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-18T08:56:27Z -->
Fixed findDisconnected (tools/archive.go): the len(nodes)==0 early return now checks params.Digest first and returns auditStaleDigestResult{Lines:[]string{}} when true. Also added omitempty to PlaceholderLines and PlaceholdersTruncated on auditStaleDigestResult so orphans digest (both empty and non-empty paths) never emits spurious placeholder fields. Test: TestAudit_OrphansDigestEmptyReturnsLinesShape. Shipped in v1.67.2.
