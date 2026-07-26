# orient: header load_bearing_low_trust counter

**Status:** OPEN

**Shared-surface node:** `surfacing-vector-orient-header-l-d7dce0d6`

**Depends on:** `stories/node-kind-coverage-signal.md` (COMPLETE — `audit(mode=kind_coverage)`)

**Sibling (shipped):** `stories/trust-inline-annotation.md` (per-node trust on significant section)

---

## Why

Trust surfacing shipped two vectors (orient significant inline annotation, filing-time
nudge) but the **always-run header counter** was explicitly deferred: shipping an
always-zero counter would train agents to ignore trust (`surfacing-vector-orient-header-l-d7dce0d6`).

Prerequisite `audit(mode=kind_coverage)` now ships — operators can see legacy-dominant
graphs before the counter goes live. The counter itself is still unimplemented.

Verified 2026-07-26: no `load_bearing_low_trust` field in `tools/orient.go` response structs.

---

## Contract (from shared-surface goal)

Add to orient response root (domain-scoped and multi-domain paths):

```json
{
  "load_bearing_low_trust": 3,
  ...
}
```

Count = live nodes in orient's `significant` section (or equivalent load-bearing set)
that fail the low-trust predicate (same rules as `significance(mode=trust)` /
`trust-inline-annotation`).

When `load_bearing_low_trust > 0`, orient description should instruct agents to inspect
significant entries with `"trust": "low — …"` inline annotations (already shipped).

### Bootstrap / cross-domain path

Cross-domain bootstrap (`orient()` with no domain) may omit or scope differently —
decide explicitly; Recordari finding noted stale_count missing on bootstrap.

---

## Acceptance criteria

- Domain orient returns `load_bearing_low_trust` integer ≥ 0
- Count matches manual inspection of significant + trust annotation set
- Zero on healthy fixture domain; >0 on fixture with assumption-only load-bearing node
- Tool description updated with reactive guidance when > 0
- Tests in `tools/orient_test.go`

---

## References

- Trust predicate: `db/trust.go`, `tools/trust_helpers.go`
- Kind coverage gate: `stories/node-kind-coverage-signal.md`
