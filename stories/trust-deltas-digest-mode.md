# trust deltas in digest-mode orient output

**Status:** OPEN

**Shared-surface node:** `surfacing-vector-trust-deltas-fe-a89714a5`

**Depends on:** `stories/trust-inline-annotation.md` (COMPLETE)

---

## Why

Inline trust annotation (Phase 3) surfaces low-trust on orient's `significant` section at
session start. A second vector adds the **time derivative** — trust getting worse — in
digest-mode output at near-zero marginal cost, giving agents a periodic push signal without
a separate `significance(mode=trust)` call.

Not implemented. Verified 2026-07-26: digest lines include `(trust: …)` when set on
scored entries (`digestLineFromScored`) but no delta/trend signal.

---

## Design (from shared-surface goal)

- Compare current trust score/basis to last logged value in `significance_log` (or lightweight
  snapshot table if needed)
- In digest orient output, append delta hint when trust dropped since last session, e.g.
  `(trust: low — …; ↓ since last orient)`
- Non-blocking advisory only

---

## Open questions

1. Is `significance_log` sufficient or do we need per-node trust history?
2. Scope: significant section only, or all digest sections?
3. Token budget — cap delta annotations per orient call

---

## Acceptance criteria

- Digest orient surfaces delta when trust worsened on a significant node
- No extra tool call required
- Tests with mocked prior trust state
- `go test ./...` green

---

## References

- Trust computation: `db/trust.go`, `db/significance.go`
- Sibling: `stories/orient-load-bearing-low-trust-counter.md`
