# STORY-017: remember tool description still says "new domain" for misdomain fields

**Type:** bug

## Goal

tools/definitions.go remember description says "when filing creates a new domain that workspace KNN flags" for possible_misdomain/suggested_domain/suggested_memory_id. Since STORY-011 the check runs on every filing, not only new-domain creation. Update the description to match the actual behaviour.

## Acceptance criteria

- [x] AC-STORY-017-98b6017c: tools/definitions.go remember description no longer says 'when filing creates a new domain that workspace KNN flags'
- [x] AC-STORY-017-7a126af7: Description now says 'on any filing when workspace KNN finds a majority of the nearest neighbours in a different domain'
- [x] AC-STORY-017-06df842b: go test ./... green

## Notes

<!-- backlog-mcp: 2026-11-18T08:56:25Z -->
Updated tools/definitions.go remember tool description: replaced "when filing creates a new domain that workspace KNN flags" with "on any filing when workspace KNN finds a majority of the nearest neighbours in a different domain". Shipped in v1.67.2.
