# significance(mode=trust): inline low-trust annotation on orient's significant section

**Status:** COMPLETE

**Shared-surface node:** `surfacing-vector-inline-trust-an-ebe05e3e`

**Depends on:** `stories/computed-trust.md` (COMPLETE — shipped as `significance(mode=trust)`)

**Sibling (not this story — separately blocked):** `surfacing-vector-orient-header-l-d7dce0d6`
(orient header `load_bearing_low_trust` counter) — explicitly blocked on the
node_kind-migration-coverage gap; do not fold that into this story.

---

## Why

`significance(mode=trust)` shipped (v1.32.0) but is pure pull — nothing in the normal
session loop invokes it, so an agent only computes trust when a human explicitly
thinks to ask, which is rare. This is the exact failure pattern that made `visualise`
vestigial: a capability with no ambient invocation point gets exercised so little it
becomes a candidate for removal (`trust-is-pull-only-risks-going-v-9857f924`).

`orient`'s `significant` section is the one place every session already looks at
session start. Annotating it inline — for nodes that are both load-bearing (already
in `significant`) and low-trust — surfaces the signal on a path agents already walk,
with no extra tool call, and no redefinition of what `significant` means (additive
only).

Unlike the sibling header-counter vector, this one is NOT blocked on node_kind
migration coverage: it operates per-node, on whatever nodes already appear in
`significant` today, using whatever `trust_basis` composition each of those specific
nodes already has. It doesn't need the whole graph to carry differentiated kinds —
only the individual nodes it's annotating.

---

## Design

For each node in orient's `significant` section, compute the same low-trust predicate
already resolved for the header-counter vector (reuse the logic, don't reinvent it):

- **net-contested**: summed inbound contribution ≤ 0 (contradiction pressure ≥
  support)
- **unsupported**: zero inbound support from 1.0-tier kinds (finding/decision/
  standing) — everything holding the node up is ≤0.6-tier (goal/option/issue/
  assumption)

A node meeting either predicate gets an inline annotation appended to its lean entry,
e.g.:

```
trust: low — self:decision, assumption×3, 0 findings
```

Reuse `trust_basis`'s existing human-readable format from `significance(mode=trust)` —
don't invent a second format for the same underlying data.

Nodes that are not low-trust get no annotation (don't clutter every line with
`trust: ok`).

---

## Resolved design questions

1. **Absolute vs normalised predicate** — use the same absolute, raw-composition
   predicate the header-counter vector resolved (not the normalised [0,1] score,
   which is unstable across small result sets). The normalised score may still be
   used for a graded low/medium display *within* this annotation if useful, but the
   trigger for showing an annotation at all is the absolute predicate.
2. **Compute cost** — `significant` is already a bounded, small (≤10) set; computing
   trust for those specific nodes on each `orient(domain=X)` call is cheap relative to
   the significance computation orient already does. No caching needed at this scale.
3. **Scope** — `declared_spine` and `recent` sections are NOT annotated. Only
   `significant`, per the shared-surface node's explicit scope ("orient's significant
   section").

---

## Acceptance criteria

- A `significant` node meeting the low-trust predicate carries a `trust` field/line in
  its lean orient output with a non-empty `trust_basis`-style summary.
- A `significant` node not meeting the predicate carries no `trust` annotation.
- Annotation format reuses `significance(mode=trust)`'s existing `trust_basis` string
  shape — no second format invented.
- `declared_spine` and `recent` sections are unaffected.
- `go test ./...` green.

---

## Files (expected)

- `db/trust.go` — expose a per-node (or per-ID-set) trust lookup usable from orient's
  code path, alongside the existing domain/memory_id-scoped `GetTrust`
- `tools/orient.go` — after building the `significant` lean list, annotate low-trust
  entries
- `db/trust_test.go`, `tools/orient_test.go` — new tests

---

## References

- Shared-surface node: `surfacing-vector-inline-trust-an-ebe05e3e`
- Blocked sibling (do not fold in): `surfacing-vector-orient-header-l-d7dce0d6`
- Trust mechanism: `stories/computed-trust.md`, `trust-as-computed-property-deriv-76098ec7`
- Problem framing: `trust-is-pull-only-risks-going-v-9857f924`
