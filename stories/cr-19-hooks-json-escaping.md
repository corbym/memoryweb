# CR-19: Escape JSON in shell hooks domain/topic handling

**Status:** DONE
**Priority:** Medium

`hooks/postcompact_hook.sh:51-53`, `hooks/userpromptsubmit_hook.sh:75,77`,
`hooks/subagent_start_hook.sh:53-60` — Domain and topic values read from context files
are embedded into JSON strings without `memoryweb_json_escape`. A `"` or `\` in
domain/topic breaks the JSON.

---

## Acceptance criteria

- [ ] All domain/topic values passed through `memoryweb_json_escape` before JSON embedding
- [ ] `hooks/hooks_test.go` updated with test for domain containing `"` character
- [ ] All hook tests pass
