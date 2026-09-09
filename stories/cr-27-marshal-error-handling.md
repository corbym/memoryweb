# CR-27: Discard json.MarshalIndent errors properly

**Status:** READY
**Priority:** Medium

All tools files — `json.MarshalIndent` errors silently discarded. If marshalling fails
(e.g., non-UTF8 label), the response contains an empty text block with no error signal.

---

## Acceptance criteria

- [ ] All `json.MarshalIndent` calls check the error
- [ ] On marshal failure, return `errorResult` with descriptive message
- [ ] All `Test*` tests pass
