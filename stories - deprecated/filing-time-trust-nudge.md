# remember/revise: filing-time low-trust neighbourhood nudge

**Status:** COMPLETE

**Shared-surface node:** `surfacing-vector-filing-time-tru-e9a9d9de`

**Depends on:** `stories/computed-trust.md` (COMPLETE — `significance(mode=trust)`)

**Sibling (different moment):** `stories/trust-inline-annotation.md` (orient `significant` inline annotation at session start)

---

## Why

`significance(mode=trust)` is pull-only — agents rarely invoke it unprompted
(`trust-is-pull-only-risks-going-v-9857f924`). The orient inline-annotation vector
covers load-bearing nodes at **session start**. This vector covers a different
moment: when the agent is **about to file or revise** a memory that depends on a
low-trust neighbourhood — before the shaky dependency becomes load-bearing.

Example nudge in the `remember`/`revise` response:

> You're depending on a memory resting on 3 assumptions, 0 findings.

---

## Design

### Trigger

After a successful `remember` or `revise`, if the filed/updated memory connects to
(or will connect to via `related_to`) nodes in a low-trust neighbourhood, surface a
non-blocking advisory field in the tool response.

Low-trust predicate — reuse the same absolute composition rules as
`stories/trust-inline-annotation.md` / `significance(mode=trust)`:

- **net-contested**: summed inbound contribution ≤ 0
- **unsupported**: zero inbound support from 1.0-tier kinds (finding/decision/standing)

### Scope

- Check dependencies the agent is creating at filing time (`related_to`, or edges
  implied by the filing context if cheap to resolve).
- On `revise`, check when `related_to`-equivalent connection changes are not in scope
  — at minimum, when the revise adds/changes content that references connected ids
  already on the node (via existing edges after update).
- Non-blocking: filing succeeds regardless; nudge is advisory only.
- Reuse existing `trust_basis` string format — no second format.

### Out of scope

- Blocking filing on low trust.
- Replacing orient's `significant` section with trust ranking.
- Header counter (`load_bearing_low_trust`) — blocked on `node-kind-coverage-signal.md`.

---

## Acceptance criteria

- `remember` response includes optional `trust_nudge` (or equivalent) when filing
  onto a low-trust dependency neighbourhood; absent when trust is fine.
- Same for `revise` when applicable per design above.
- Nudge text uses `trust_basis`-style summary, not a hand-rolled format.
- Filing/revising still succeeds when nudge present.
- Tests in `tools/remember_test.go`, `tools/revise_test.go`; `go test ./...` green.

---

## Files (expected)

- `db/trust.go` — neighbourhood trust lookup for a set of dependency ids
- `tools/remember.go`, `tools/revise.go` — attach nudge to success response
- `tools/definitions.go` — document the response field if exposed in schema/examples

---

## References

- Shared-surface node: `surfacing-vector-filing-time-tru-e9a9d9de`
- Trust mechanism: `stories/computed-trust.md`
- Problem framing: `trust-is-pull-only-risks-going-v-9857f924`
- Recordari: STORY-187 (filed from shared-surface slice)
