# node_kind coverage / migration-readiness signal

**Status:** OPEN

**Shared-surface node:** none yet — this story is the prerequisite named inside
`surfacing-vector-orient-header-l-d7dce0d6`, never itself filed as an issue/goal

**Blocks:** `surfacing-vector-orient-header-l-d7dce0d6` (orient header
`load_bearing_low_trust` counter — explicitly not ready to build until this exists)

---

## Why

`node_kind` shipped in v1.30.0 (rename from `decision_type`, enum expanded to 8
kinds), but the pre-existing graph was never migrated. The back-catalogue is almost
entirely `decision`/`standing` (carried over from the old `decision_type` enum), with
only rare `issue` nodes. That means every `trust_basis` on an old node reads
`self:decision, decision×N` — uniform, all 1.0-intrinsic — and the low-trust
predicate can never fire for it, not because the graph is healthy but because it
cannot yet express low-trust.

The orient header counter vector was explicitly deferred for this reason: "Shipping
an always-zero counter would actively train agents to read trust as 'always 0,
ignore' — the same vestigial fate via empty data instead of no invocation." Its
own resolution names the fix directly: "migrate kinds → stand up a kind-coverage/
migration-readiness signal that produces value on today's graph and drives the
migration → only then switch on load_bearing_low_trust." That middle step — the
signal itself — was never filed as its own story. This is that story.

---

## Design

A read-only diagnostic, not a new MCP tool surface (avoid adding a 22nd tool for a
transitional/migration concern) — most naturally a mode or field on an existing
inspection path.

**Proposed shape:** extend `audit` or `significance` output with a
`node_kind_coverage` block, gated behind an explicit flag or mode so it doesn't
appear in every routine call:

```json
{
  "total_nodes": 1208,
  "by_kind": {"decision": 940, "standing": 210, "issue": 40, "finding": 12, "goal": 6, ...},
  "legacy_dominant_pct": 95.2,
  "migration_candidates": ["<id>", "..."]
}
```

`migration_candidates`: nodes currently filed as `decision` whose label/description
text matches patterns suggesting a different true kind (e.g. "found that", "observed",
"confirmed" → likely `finding`; "should we", "unclear whether" → likely `issue`).
Candidate-surfacing only, same aboutness-not-certainty caveat as `audit(mode=conflicts)`
— never auto-revise node_kind, only flag for a human/agent to review and `revise`.

**Where it lives:** most consistent with `audit` — audit is already the graph-health
tool, and this is a health-of-the-taxonomy signal, not a significance ranking. Add as
a fifth mode (`mode=kind_coverage`) rather than bolting onto `significance`, keeping
the pattern "one tool, an enum, each mode a genuinely different response shape" that
already governs `audit`'s four existing modes.

---

## Open questions (resolve at implementation)

1. Pattern-matching approach for `migration_candidates` — simple keyword heuristics
   (cheap, same class as the existing stale-mode keyword rules in `db/audit.go`) vs.
   something semantic. Recommend starting with keyword heuristics, consistent with
   the rest of `audit`'s existing drift-detection style — no new dependency.
2. Whether `legacy_dominant_pct` (or an equivalent single number) is the right
   "readiness" scalar for a future automated gate, or whether the header-counter
   vector should just check `migration_candidates` count directly when deciding
   whether to switch on. Doesn't need to be resolved here — the header-counter story
   can make that call once this signal exists and produces real numbers to look at.

---

## Acceptance criteria

- `audit(mode=kind_coverage)` (or equivalent) returns per-kind counts and a legacy-
  dominance measure for a domain or the whole workspace.
- `migration_candidates` surfaces nodes whose node_kind looks stale relative to their
  own text, without auto-revising anything.
- Running this against the live memoryweb-meta domain today produces a non-trivial,
  actionable candidate list (manual sanity check, not an automated test).
- `go test ./...` green.

---

## Files (expected)

- `db/audit.go` — new query/mode
- `tools/archive.go`, `tools/definitions.go` — `audit` mode enum + description
- `db/audit_test.go`, `tools/archive_test.go` — new tests

---

## References

- Deferred consumer: `surfacing-vector-orient-header-l-d7dce0d6`
- node_kind migration: `stories/node-kind.md` (COMPLETE), `node-kind-story-implementation-c-7cdd8e4d`
- Style precedent for candidate-surfacing-not-detection: `conflict-flagging-is-candidate-s-4a5be655`
