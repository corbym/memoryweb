# STORY-018: audit(mode=orphans, digest=true) returns wrong shape when no orphans

**Type:** bug

## Goal

In findDisconnected (tools/archive.go), the len(nodes)==0 early return runs before the params.Digest branch, so digest=true with no orphans returns {nodes:[], results_truncated:false} instead of the promised {lines:[], results_truncated:false}.

## Acceptance criteria

- [ ] Define acceptance criteria
