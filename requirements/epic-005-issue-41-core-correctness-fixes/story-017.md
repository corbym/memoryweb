# STORY-017: remember tool description still says "new domain" for misdomain fields

**Type:** bug

## Goal

tools/definitions.go remember description says "when filing creates a new domain that workspace KNN flags" for possible_misdomain/suggested_domain/suggested_memory_id. Since STORY-011 the check runs on every filing, not only new-domain creation. Update the description to match the actual behaviour.

## Acceptance criteria

- [ ] Define acceptance criteria
