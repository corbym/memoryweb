# occurred_at Wording Refinement

> **COMPLETE** — shipped 2026-05-23.
> - `remember` and `revise` descriptions updated to (a)/(b) split.
> - Both `occurred_at` property descriptions updated.
> - Both `items` array descriptions updated to use "in-session: set freely; inferred/back-dated: propose+confirm, never infer silently".
> - `TestOccurredAtWording_TwoCases` added; existing `TestOccurredAt_ToolDescriptions_ContainProposeConfirmGuidance` updated to drop removed phrases.

Splits the `occurred_at` description into two cases with different epistemic standing.
Reduces unnecessary friction for the common case while preserving the guard against agents
silently inventing historical significance. Mirrors recordari STORY-051 (shipped 2026-05-21).

---

## Motivation

The current `occurred_at` description requires propose+confirm for all cases uniformly.
This creates unnecessary friction when an agent is setting a date for something it directly
witnessed happen in the current session (e.g., "we decided X just now"). The friction makes
sense when an agent is inferring or back-dating a historical date it didn't observe.

The split is about epistemic standing:
- **In-session witnessed**: the agent was present when the decision was made. No confirmation
  needed.
- **Inferred or back-dated**: the agent is guessing from context or filling in a past date
  it didn't directly observe. Propose+confirm required.

---

## Change

The `occurred_at` field description appears in the `remember` tool's schema (shared between
single and batch modes) and in `revise`. It is a single shared schema description string
in `tools/tools.go`.

Replace the current uniform wording with the two-case split. The wording to match is from
recordari STORY-051 (in `internal/mcp/tools.go`, the `occurred_at` schema description):

```
occurred_at: ISO8601 date or datetime for when this event actually happened.

Two cases:

(a) In-session witnessed — you directly observed this decision or event happen during
    the current conversation. Set occurred_at freely using today's date. No confirmation
    needed.

(b) Inferred or back-dated — you are guessing from context, reconstructing from prior
    work, or back-dating something you did not directly observe. Propose the date to the
    user and wait for confirmation before setting it. Never guess. Never infer it silently
    from context.

why_matters is required when occurred_at is set — explain why this event is significant
before filing it on the timeline.
```

Adapt the exact wording as needed for consistency with memoryweb's existing description
style, but preserve the (a)/(b) split and the "never guess... never infer it silently"
forbidder.

---

## Acceptance criteria

- `TestOccurredAtWording_TwoCases`: tool description for `remember` (and `revise`) contains
  text distinguishing in-session witnessing from inferred/back-dated dates. Assert that
  both "directly witnessed" (or equivalent) and "never guess" (or "never infer") appear
  in the `occurred_at` property description.
- The existing hard enforcement test (`occurred_at` requires `why_matters`) still passes —
  this story changes wording only, not the validation logic.
- Wording in `remember` and `revise` is consistent (both use the same shared schema
  description string).
- `go test ./...` green.

---

## Files

- `tools/tools.go` — `occurred_at` property description in the `remember` and `revise`
  tool schemas (should be a single shared string)

## References

- shared-surface node: `occurred-at-refinement-in-sessio-d225f645`
- recordari reference: STORY-051 (`internal/mcp/tools.go`)
