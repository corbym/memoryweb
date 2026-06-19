# Computed Trust — epistemic weight derived from node_kind topology

**Status:** OPEN — unblocked; `stories/node-kind.md` is implemented (commit 5b451e6), pending release

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

Add a `trust` tool (or extend `significance` with a `mode=trust` parameter — decide
at implementation time which is less noisy in the tool list).

Parameters mirror `significance`:

| Parameter | Description |
|-----------|-------------|
| `domain` | Scope to a domain |
| `memory_id` | Scope to a memory's neighbourhood |
| `limit` | Top-N (default 10) |
| `tags` | Tag filter (OR semantics) |

Response per node:

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

## Open questions (resolve at implementation)

1. **Separate tool vs `significance` mode?** A separate `trust` tool is cleaner for
   description clarity; a mode parameter keeps the tool count down. Check tool count
   before deciding — if at or near 21, use mode.
2. **`reference` node handling:** `reference` nodes have no intrinsic epistemic
   weight. Exclude them from scoring? Or score as N/A and omit from ranked output?
3. **`contradicts` edge weight:** Should be negative. Quantify: −1× the intrinsic
   weight of the contradicting node? Or a fixed penalty?
4. **Cross-domain edges:** Trust topology can span domains. Clip to domain or follow
   edges? Recommendation: clip by default (same as significance), allow opt-out via
   a `cross_domain` flag.
5. **DB layer vs tools layer:** Significance computation is in `db/db.go`. Trust
   should follow the same pattern.

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

- `db/db.go` — `GetTrust(domain, memoryID, limit, tags string) (*TrustResult, error)`
- `tools/tools.go` — `trust` tool definition + `handleTrust`
- `tools/tools_test.go` — new tests

---

## References

- Shared-surface node: `trust-as-computed-property-deriv-76098ec7`
- Blocker: `stories/node-kind.md` (and `build-node-kind-rename-decision--4e2cd629`)
- Distinction from significance: `evidence-weighting-significance--deba529f`
- Parallel to significance tool: `significance-tool-dual-signal-im-84b471ff`
