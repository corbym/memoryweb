# remember: similarity floor and domain affinity for filing-time candidates

**Status:** COMPLETE

**Shared-surface node:** `finding-remember-filing-time-candidate-suggestions-show-cross-domain-noise-no-similarity-floor-34e0d3f0`

**Recordari:** STORY-145/147 shipped 2026-06-28 (noise gate + domain affinity on Recordari side).

---

## Why

Filing recordari-admin findings about JWT provisioning caused `remember()` to return
five deep-game Z80 assembly nodes as filing-time `suggested_connections` — zero topical
relevance. The standalone `suggest_connections` tool uses tag overlap; remember's path
uses embedding nearest-neighbour across the whole workspace with no domain-affinity
weighting or minimum-similarity floor.

k-NN always returns top-5 regardless of whether anything is genuinely close — in a
heterogeneous corpus it hands back the least-bad options rather than suppressing weak
matches. Generic engineering vocabulary ("fix", "memory", "register", "stack") clusters
unrelated domains.

Same code path is shared between Recordari and memoryweb. Recordari fixed its half;
memoryweb likely exhibits identical noise on cross-domain filing.

Undermines planned contradiction-candidate work — authority weighting on top of noisy
candidates won't deliver epistemic trust.

---

## Changes

Port Recordari STORY-145/147 behaviour:

1. **Similarity floor:** suppress candidates below threshold (configurable constant;
   start with distance above which embedding match is "no signal").
2. **Domain affinity:** prefer same-domain neighbours; cross-domain only when distance
   is significantly better than best same-domain match.
3. **Empty over noise:** return fewer than 5 (even zero) when nothing clears the floor.

Apply to:
- `remember()` filing-time `suggested_connections`
- `suggest_connections` semantic path (if separate)

---

## Acceptance criteria

- Cross-domain filing in heterogeneous workspace → no spurious deep-game/Z80 suggestions
  for unrelated admin bug reports (regression fixture from finding).
- Same-domain genuine duplicate → still suggested above floor.
- Fewer than 5 suggestions when corpus has no close matches.
- `go test ./...` green.

---

## Files (expected)

- `db/embeddings.go` or `db/search.go` — neighbour query with floor + domain weighting
- `tools/remember.go` — candidate generation path
- `tools/remember_test.go` — cross-domain noise regression test

---

## References

- Recordari: STORY-145/147, `story-145-147-filed-candidate-noise-prerequisites-before-contradiction-chain-5ab478e1`
- Downstream: `stories/audit-conflicts-mode.md`
- Shared finding: `finding-remember-filing-time-candidate-suggestions-show-cross-domain-noise-no-similarity-floor-34e0d3f0`
