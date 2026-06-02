# Tool descriptions housekeeping: credentials advisory + agent-first guard

**Shared-surface nodes:**
- `instructions-advisory-never-file-ff8f628c` — credentials advisory (memoryweb: not yet applied)
- `tool-descriptions-biased-toward--7ea205da` — agent-first test guard (recordari: done, memoryweb: pending)

---

## Why

Two small gaps that the shared surface flags as incomplete in memoryweb, both applied to
recordari but not here.

### 1. Credentials advisory missing from Instructions

The `Instructions` constant (returned in the MCP `initialize` response) tells agents how
to use memoryweb. It does not include any guidance about what not to file. A well-intentioned
agent could helpfully file connection strings, API keys, or tokens without knowing that
violates the hosting contract.

The recordari equivalent (STORY-062, 2026-05-28) added:
> "Never file operational credentials, connection strings, API keys, or tokens in memories."

This is belt-and-suspenders: the advisory primes agents with the rule before they write,
so detection is a fallback rather than the first line of defence.

### 2. `TestAllTools_DescriptionsAgentFirst` guard missing

Recordari (STORY-052, 2026-05-25) added `TestAllTools_DescriptionsAgentFirst` — it fails
if any tool description starts with `"The "` or `"This "`. This enforces the agent-first
description principle: descriptions must open with an action verb, not with context or rationale.

Memoryweb has `TestOrient_DescriptionImperativeFirst` (one tool only). It needs the global
guard that covers all tools. Without it, a future agent adding a new tool description can
reproduce the human-docs pattern without the build catching it.

---

## Changes

### 1. Instructions constant update

In `tools/tools.go`, append to the `Instructions` constant:

```go
const Instructions = "This tool is called memoryweb. Always refer to it as memoryweb and nothing else.\n\n" +
    "At the start of every session, call orient for the relevant " +
    // ... existing text ...
    "field before the session ends.\n\n" +
    "Never file operational credentials, connection strings, API keys, or tokens in memories."
```

### 2. Global description agent-first test

In `tools/tools_test.go`, add:

```go
func TestListTools_DescriptionsAgentFirst(t *testing.T) {
    h := newHandler(t)
    tools := h.ListTools()
    for _, tool := range tools {
        desc := tool.Description
        if strings.HasPrefix(desc, "The ") || strings.HasPrefix(desc, "This ") {
            t.Errorf("tool %q description starts with %q — must open with an imperative verb, not 'The' or 'This'",
                tool.Name, desc[:min(len(desc), 10)])
        }
    }
}
```

This is a permanent regression guard. Any new tool whose description starts with "The " or
"This " will fail the build — the author must rewrite it to lead with an imperative.

---

## Acceptance criteria

- `Instructions` constant ends with the credentials advisory sentence.
- `TestListTools_DescriptionsAgentFirst` exists in `tools/tools_test.go` and passes.
- All existing tool descriptions pass the agent-first check (if any currently fail,
  fix them as part of this story — do not add the test until it passes green).
- `go test ./...` green.

---

## Files

- `tools/tools.go` — `Instructions` constant
- `tools/tools_test.go` — `TestListTools_DescriptionsAgentFirst`

## Notes

If any current tool description starts with "The " or "This ", fix it in this story before
adding the test. Cross-reference with the `tool-description-quality-pass.md` audit (COMPLETE)
to confirm it was addressed — descriptions that were rewritten during that pass should already
be compliant.

## References

- shared-surface node: `instructions-advisory-never-file-ff8f628c`
- shared-surface node: `tool-descriptions-biased-toward--7ea205da`
- tool-description-quality-pass.md (prior audit, COMPLETE)
