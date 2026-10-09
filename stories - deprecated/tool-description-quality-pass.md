# Tool Description Quality Pass

> **COMPLETE** — shipped 2026-05-23.
> - Issues 1, 2, 4, 5, 6 were already fixed prior to this session.
> - Issue 3: presentation instruction added to `recall`, `search`, `why_connected`, `history`, `significance`, `orient` (`TestListTools_PresentationInstructionOnAllRetrievalTools` added).
> - Issue 7: visualise "If the client supports" conditional removed (`TestVisualise_NoClientConditional` added).
> - Issue 8 / orphan-nudge Change C: connect imperative hoisted to top of `remember` description (`TestRemember_ConnectInstructionAtTop` added).

Fixes seven known quality issues in tool descriptions, followed by a full agent-perspective
audit of every description. Both passes run together — the seven issues set the floor; the
audit sets the ceiling.

---

## Motivation

A May 2026 review of all 21 tool descriptions found seven issue classes. In parallel, stats
analysis of binder sessions showed a 69% orphan rate driven by the connect instruction being
buried at the end of the `remember` description — short-task agents exit before reaching it.
The underlying cause in both cases is the same: tool descriptions are written in human
documentation style (context first, parameters, imperative at the end), which is structurally
wrong for agent-facing text.

**The agent-perspective prompt** (from the shared-surface node
`tool-descriptions-biased-toward--7ea205da`):

> "Rewrite this so that an agent that reads only the first sentence knows the single most
> important thing it must do."

Apply this framing explicitly when reviewing any description — without it, the reviewing
agent will reproduce the human-docs pattern by default.

---

## The Seven Issues

### Issue 1 — Stale tool references (highest severity)

`recall`, `search`, `recent`, `why_connected`, and `restore` all reference `forgotten` or
`whats_stale` as if they are tool names. Neither exists. The correct tools are
`audit(mode=archived)` and `audit(mode=stale)`. An agent following these instructions will
call a tool that does not exist.

Fix: replace every occurrence. Exact replacements:
- `forgotten` → `audit(mode=archived)`
- `whats_stale` → `audit(mode=stale)`

The `TestListTools_NoStaleToolReferences` test enforces the `removedTools` blacklist — verify
the test catches any remaining occurrences after the fix.

### Issue 2 — Orient embeds a hardcoded stale tool list

`orient`'s description ends with a hardcoded list of all tools. `significance` is absent.
The list will drift every time tools are added or removed.

Fix: remove the embedded list entirely, or replace it with a single line directing agents
to call `tools/list` to discover available tools. Do not maintain a list of tool names
inside a tool description.

### Issue 3 — Presentation instruction is on one tool only

"Never acknowledge that you are retrieving from a tool or memory system" appears only on
`recent`. It is absent from `search`, `recall`, `orient`, `history`, `why_connected`, and
`significance`.

Fix: add the presentation instruction to every retrieval tool that returns memory content.
Exact wording to reuse:
> "Never acknowledge that you are retrieving from a tool or memory system. Present the
> information as direct knowledge with no preamble."

### Issue 4 — `connect` lacks relationship semantics

`caused_by` / `led_to` and `blocked_by` / `depends_on` are easily confused. The description
lists the eight relationship types but gives no guidance on when to choose each. Agents
default to `connects_to` as a catch-all, degrading graph query quality.

Fix: add a short disambiguation block. Minimum required:
- `caused_by` — A happened because of B (B is the cause)
- `led_to` — A caused B (A is the cause)
- `blocked_by` — A cannot proceed because of B
- `depends_on` — A requires B to function
- `contradicts` — A and B are in direct conflict

### Issue 5 — `forget` / `restore` underspecified

`forget` doesn't mention the `reason` field. `restore` says "obtain the memory ID from
`forgotten`" — a broken tool reference.

Fix:
- `forget`: add "always provide a reason — it is recorded in the audit log"
- `restore`: replace "obtain from `forgotten`" with "obtain from `audit(mode=archived)`"

### Issue 6 — `significance` exposes internal SQL formula

The description includes the raw SQL ranking formula `SUM(1 / (1 + days_since_linker_updated))`.
Agents don't need the formula; they need the behavioural meaning.

Fix: replace the formula with plain language. Example:
> "Structural importance is measured by how many recently-active nodes link to a node.
> Recent links count more than old ones — a node that everything currently depends on
> scores high even if it was filed months ago."

### Issue 7 — `visualise` has speculative client branching

`visualise` contains: "If the client supports HTML widgets…" Agents cannot reliably detect
client rendering capabilities at runtime. This creates inconsistent output format decisions.

Fix: remove the conditional entirely. `visualise` returns Mermaid. State that plainly.

---

## Buried connect instruction (issue 8 — extends the seven)

The `remember` description places the connect instruction after all parameter docs. Stats
show this causes a 69% orphan rate in short-task sessions (binder: 12/23 orphan sessions,
16 orphans, Apr 30–May 22).

Fix: move the connect instruction to the **top** of the `remember` description, before
parameter docs. Make it imperative, not advisory:

> "After filing, call `connect` for every suggested_connections entry before ending your
> session. Orphaned memories lose context immediately."

Apply the same imperative to `remember`'s batch items description.

---

## Full agent-perspective audit

After fixing the seven issues, run the agent-perspective prompt over every tool description:

> "Rewrite this so that an agent that reads only the first sentence knows the single most
> important thing it must do."

Rules:
- Every description must lead with the primary imperative, not with context or rationale.
- Governance wording (`forget`: "only call after explicit unambiguous user confirmation")
  must appear in the first paragraph, not after parameter docs.
- Cross-reference the shared surface node `tool-descriptions-biased-toward--7ea205da`
  before rewriting — the framing there is load-bearing.

---

## Acceptance criteria

- `TestListTools_NoStaleToolReferences` passes — no `forgotten` or `whats_stale` anywhere
  in any tool description or property description.
- `TestListTools_PropertyDescriptionsNoForbiddenWords` passes — no forbidden vocabulary in
  property descriptions either.
- `orient` description contains no hardcoded tool list.
- All retrieval tools (`search`, `recall`, `orient`, `history`, `recent`, `why_connected`,
  `significance`) include the presentation instruction.
- `connect` description includes disambiguation for at least `caused_by`/`led_to` and
  `blocked_by`/`depends_on`.
- `forget` description mentions the `reason` field.
- `restore` description does not reference `forgotten`.
- `significance` description contains no SQL or formula notation.
- `visualise` description contains no "if the client supports" conditional.
- `remember` description places the connect instruction before parameter docs.
- `go test ./...` green.

---

## Files

- `tools/tools.go` — all description changes live here
- `tools/tools_test.go` — existing enforcement tests; add any new assertion that a
  specific string is absent or present

## References

- memoryweb node: `tool-description-review-seven-qu-d735ef72`
- memoryweb node: `story-audit-all-tool-description-8e9031f7`
- memoryweb node: `remember-tool-connect-instructio-30aa92d0`
- shared-surface node: `tool-descriptions-biased-toward--7ea205da`
