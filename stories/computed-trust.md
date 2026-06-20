# Computed Trust — epistemic weight derived from node_kind topology

**Status:** COMPLETE — implemented 2026-06-20 as `significance(mode=trust)`

**Shared-surface node:** `trust-as-computed-property-deriv-76098ec7`

---

## Why

memoryweb has `significance` — a structural importance signal computed from inbound
edge count and recency. It answers "what is load-bearing right now?" It does not
answer "how much should I trust this?" Those are different axes.

A node can be highly significant (everything references it) and low-trust (it rests
entirely on `assumption` nodes that have never been corroborated). Significance
ranks it top; a trust signal would flag it as shaky. Neither product currently has
any trust signal.

The answer is not to add hand-asserted confidence scores — that would impose a write
burden on agents. The answer is to **compute trust from the structure already in the
graph**, using `node_kind` as the epistemic-weight carrier. An agent that files with
honest kinds produces a graph whose topology encodes reliability. Trust falls out
without any extra annotation step.

This is a genuine differentiator: memory that reasons about the reliability of its
own contents. It is the concrete answer to the evidence-weighting gap the
`evidence-weighting-significance--deba529f` node identifies.

---

## Mechanism

Two components:

**Intrinsic weight** — a node's own kind has a base trust level:

| Kind | Intrinsic trust |
|------|----------------|
| `finding` | High — empirical, verifiable |
| `decision` | High — settled by authority |
| `standing` | High — durable rule, slow to change |
| `goal` | Medium — intended but not yet achieved |
| `option` | Medium — candidate, not yet chosen |
| `issue` | Medium-low — unresolved tension |
| `assumption` | Low — unverified precondition |
| `reference` | N/A — entities are not epistemic claims |
| `transient` | N/A — lifespan marker, not a claim |

**Structural weight** — the kinds of nodes connected to a node modulate its trust:

- A `decision` resting on three `finding` nodes is more trustworthy than one resting
  on three `assumption` nodes.
- A `standing` rule contradicted by recent `finding` nodes should have its trust
  score reduced.
- The `contradicts` relationship is a direct trust reducer; `is_example_of` and
  `governed_by` are trust amplifiers.

The algorithm (parallel to `GetSignificance`):

```
trust(n) = intrinsic_weight(n.node_kind)
         + Σ edge_weight(e.relationship) × intrinsic_weight(neighbour.node_kind)
             for each edge e incident to n
         (recency-discounted by same window as significance)
```

Normalise to [0, 1] within a result set before returning.

---

## Proposed tool

Extend `significance` with a `mode` parameter: `mode=significance` (default, unchanged
— the existing four-section dual-signal output) or `mode=trust` (the new ranked-list
output below). No new tool, no change to default behaviour.

All of `significance`'s existing parameters carry over unchanged for `mode=trust`:
`domain`, `memory_id`, `limit`, `tags`, `recency_window` (the trust formula is
explicitly recency-discounted by the same window as significance).

Response per node (replaces the four-section shape only when `mode=trust`):

```json
{
  "id": "...",
  "label": "...",
  "node_kind": "decision",
  "trust_score": 0.82,
  "trust_basis": "finding×3, assumption×1",
  "why_matters": "..."
}
```

`trust_basis` is a human-readable summary of which connected kinds drove the score —
this is the audit trail that makes the signal interpretable rather than opaque.

---

## Resolved design questions

1. **Separate tool vs `significance` mode?** Resolved: `mode=trust` on `significance`,
   not a new tool. The live tool count is exactly 21 right now — at the threshold this
   question itself set for preferring a mode parameter. This also matches `audit`'s
   existing precedent: one tool, a `mode` enum, and each mode already returns a
   genuinely different response shape (`stale` → drift candidates, `orphans` → plain
   node list, `archived` → archived node list). `mode=trust` returning the ranked
   trust list instead of the four-section breakdown is the same pattern, not a new one.
2. **`reference` (and `transient`) node handling:** Resolved, and already implied by
   the acceptance criteria below: excluded from the ranked *output* (no `trust_score`
   row of their own — they're not epistemic claims, there's nothing to rank), but
   still counted as a *neighbour* when computing another node's `trust_basis` (their
   intrinsic weight there is simply 0, since N/A contributes nothing to the sum either
   way). This mirrors `audit(mode=orphans)`'s existing exclusion of `reference` nodes
   from orphan detection — the same "not a claim" reasoning already governs that case.
3. **`contradicts` edge weight:** Resolved: `−1 × intrinsic_weight(neighbour.node_kind)`,
   not a fixed penalty. Keeps the formula symmetric (a supporting neighbour contributes
   `+1 × intrinsic_weight(neighbour)`; a contradicting one contributes the same
   magnitude, negated) and keeps every number in the formula derived from `node_kind`
   rather than introducing a standalone magic constant. Concretely: being contradicted
   by a `finding` (high intrinsic weight) costs more trust than being contradicted by
   an `assumption` (low intrinsic weight) — which is the epistemically correct
   direction. `is_example_of` and `governed_by` use the same default `+1` as any other
   supporting relationship; nothing in this story requires them to be weighted higher
   than a generic connection, so they aren't, unless real usage later shows otherwise.
4. **Cross-domain edges:** Resolved: clip to domain by default, **no** `cross_domain`
   opt-out flag. `significance`'s own `memory_id` mode is already domain-clipped with
   no escape hatch today, so adding one for `trust` alone would be new, unrequested
   flexibility inconsistent with its closest sibling.
5. **DB layer vs tools layer:** Resolved, and superseded by the file-split refactor
   (`db/db.go` no longer exists): trust computation goes in a new `db/trust.go` —
   parallel to `db/significance.go` but a distinct file, since it's a genuinely
   different computation (node-kind + edge-kind weighted, not inbound-edge-count
   weighted), consistent with "one file per concern" in CLAUDE.md's package layout.
   The handler-side `mode=trust` branch goes in the existing `tools/significance.go`,
   next to `handleSignificance`, since `mode=trust` is reached through the same
   `significance` tool entry point.

---

## Acceptance criteria

*(Defined at implementation time — these are current expectations)*

- `TestTrust_FindingBacked`: a `decision` node with three `finding` neighbours scores
  higher than the same node with three `assumption` neighbours.
- `TestTrust_ContradictsPenalty`: a `standing` node with a `contradicts` inbound edge
  scores lower than the identical node without it.
- `TestTrust_ReferenceExcluded`: `reference` nodes are not scored (appear in
  `trust_basis` for other nodes but not in ranked output).
- `TestTrust_TransientExcluded`: `transient` nodes are not scored.
- `trust_basis` field is non-empty for every scored node.
- Scores are in [0, 1].
- `go test ./...` green.

---

## Files (expected)

- `db/trust.go` (new) — `GetTrust(domain, memoryID string, limit, recencyWindowDays int, tags []string) (TrustResult, error)`, mirroring `GetSignificance`'s signature in `db/significance.go`
- `tools/significance.go` — add the `mode` dispatch in `handleSignificance`; `significance` tool's `InputSchema` (in `tools/definitions.go`) gains the `mode` enum property
- `tools/significance_test.go` — new tests
- `db/significance_test.go` — new tests for `GetTrust` (mirroring the existing `GetSignificance` test pattern in the same file)

---

## References

- Shared-surface node: `trust-as-computed-property-deriv-76098ec7`
- Blocker: `stories/node-kind.md` (and `build-node-kind-rename-decision--4e2cd629`)
- Distinction from significance: `evidence-weighting-significance--deba529f`
- Parallel to significance tool: `significance-tool-dual-signal-im-84b471ff`
