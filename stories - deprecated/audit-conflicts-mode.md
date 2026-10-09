# audit(mode=conflicts) — semantic candidate surfacing for contradictions

**Status:** COMPLETE

**Shared-surface node:** `conflict-flagging-is-candidate-s-4a5be655`

**Prerequisite:** `stories/remember-candidate-similarity-floor.md` (similarity floor before
candidate quality is trustworthy)

**Recordari backlog:** STORY-136+ contradiction chain (partially filed)

---

## Why

Conflict-flagging between nodes should be **candidate-surfacing**, not
contradiction-**detection**. Semantic similarity measures aboutness, not agreement:
"cap the pool at 20" and "cap the pool at 35" are near-identical in embedding space
AND genuinely contradict — but "cap the pool at 20" and "raise the pool to 20" are
also near-identical and AGREE.

Naive nearest-neighbour conflict flags produce false positives. Realistic design:

1. **Cheap retrieval:** semantic search finds candidate pairs (sqlite-vec embeddings
   already exist).
2. **Judgement step:** the calling agent decides if they actually conflict.

The server never claims "these conflict" — only "these are worth your attention."

Natural home: **`audit(mode=conflicts)`** — a sixth drift mode alongside
stale/orphans/archived. No new tool. No orient mode-swap.

---

## Design contract

**Input:** optional `domain`, `limit`, `node_kind` (when node_kind filter ships).

**Output:** list of candidate pairs:
```json
{
  "candidates": [
    {
      "a_id": "...",
      "a_label": "...",
      "b_id": "...",
      "b_label": "...",
      "semantic_distance": 0.12,
      "reason": "semantically adjacent — agent adjudicates whether these conflict"
    }
  ],
  "truncated": false
}
```

**Rules:**
- Exclude pairs already linked by `contradicts` edge (those are stale audit's job).
- Exclude pairs below similarity floor (see remember-candidate-similarity-floor story).
- Prefer same-domain pairs; cross-domain only above higher distance threshold.
- Apply digest-line format when digest mode ships for audit.

**Tool description:** imperative framing — "Review each pair; use connect with
relationship contradicts only after confirming conflict. Never auto-file contradicts
edges."

---

## Acceptance criteria

- `audit(mode=conflicts)` returns semantically adjacent pairs within domain.
- Pairs with existing `contradicts` edge excluded.
- Pairs below similarity floor return empty (not noise wall).
- Invalid mode still errors; conflicts coexists with stale/orphans/archived.
- `go test ./...` green.

---

## Files (expected)

- `db/audit.go` — candidate pair query via embeddings
- `db/embeddings.go` — neighbour search helper if not present
- `tools/archive.go` — mode routing, description, digest rendering
- `tools/archive_test.go`, `db/audit_test.go`

---

## References

- Decision: `conflict-flagging-is-candidate-s-4a5be655`
- Prerequisite finding: `finding-remember-filing-time-candidate-suggestions-show-cross-domain-noise-no-similarity-floor-34e0d3f0`
- Resolution lifecycle: `stories/audit-contradiction-resolution.md`
- Recordari: STORY-145/147 (noise gate), STORY-136 (contradiction surfacing)
