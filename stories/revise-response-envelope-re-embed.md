# revise: response envelope + re-embed on semantic change

**Status:** OPEN

**Shared-surface spec:** `spec-revise-response-envelope-re-embed-shared-contract-memoryweb-mirrors-story-250-no-ownership-supersede-27342d56`

**memoryweb-meta goal:** `story-needed-memoryweb-revise-response-envelope-re-embed-mirrors-story-250-no-ownership-supersede-5c5b1c42`

**Recordari precedent:** STORY-250 (shipped); memoryweb mirrors minus ownership/supersede/override/claim

**Related (deferred):** `stories/occurred-at-embedding.md` — do **not** add
`occurred_at` to embed text; date-in-embedding deferred after Recordari 0/41 probe.

---

## Why

### Envelope gap

`remember` returns `{memory, suggested_connections, possible_contradicts?}` but `revise`
returns only `{node, updated}` — even though revise already loads edges via
`GetNodeWithEdges` and discards them. After a revise the agent may need to disconnect
stale edges or connect new ones; without an inline envelope it must `recall` +
`suggest_connections` separately (often skipped).

### Re-embed gap

`remember` embeds at write time (`label + " " + description + " " + whyMatters` in
`db/nodes.go:77`); `UpdateNode` updates semantic fields but **never** calls
`storeEmbedding` — `suggested_connections` / contradiction ranking after revise uses a
stale vector.

Verified 2026-07-26: `db/nodes.go UpdateNode` ends at re-fetch with no embedding path.

Re-embed motivation is **suggestion accuracy after content edits**, not date retrieval
(`occurred-at-embedding.md` deferred — embedding dates did not improve date-query recall
on Recordari).

---

## Contract (memoryweb scope — from shared-surface spec)

Every successful single-memory `revise` returns:

- `connections`: lean inbound/outbound list `{direction, relationship, peer_id, peer_label}`
- `suggested_connections`: same shape/pipeline as `remember` (`SuggestConnections`)
- `possible_contradicts` + `possible_contradicts_candidates` when filing-time threshold crossed

Batch `revise` (`items[]`): per-item envelope mirroring remember batch — not aggregate
`{updated:N}` only.

No-op revise (`updated:false`): still return connections + suggestion/contradiction envelope.

**Re-embed:** when **label, description, or why_matters** change, recompute embedding
from post-update text (`label + " " + description + " " + whyMatters` — same as
`remember`) and persist **before** ranking suggestions/contradictions.

Tags-only, domain-only, node_kind-only, or occurred_at-only revise: use stored embedding
(occurred_at is not in embed text today and is deferred from embed text per
`occurred-at-embedding.md`).

Tool description + skill: imperative same-turn review of connections,
suggested_connections, possible_contradicts (warning-as-instruction pattern).

### NOT in memoryweb scope

- override / override_confirm (STORY-206)
- claim path (STORY-209)
- supersede flag + archived predecessor (STORY-249)
- Role gates on foreign-node writes
- Appending `occurred_at` to embedding text (deferred — see `occurred-at-embedding.md`)

---

## Changes

### `db/embeddings.go` — shared helper

```go
func embedTextForNode(label, description, whyMatters string) string {
    return label + " " + description + " " + whyMatters
}
```

Single source of truth for embed input — used by `AddNode`, `AddNodesBatch`,
`BackfillEmbeddings`, and `UpdateNode` re-embed path. No `occurred_at` suffix until/unless
`occurred-at-embedding.md` reopens.

### `db/nodes.go` — `UpdateNode` / `UpdateNodesBatch`

After successful commit, if label/description/why_matters changed:

```go
text := embedTextForNode(n.Label, n.Description, n.WhyMatters)
if embedding, err := embed(text); err == nil {
    s.storeEmbedding(id, embedding)
}
```

Refactor existing `AddNode` / `AddNodesBatch` embed calls to use `embedTextForNode`
(deduplication only — behaviour unchanged).

### `tools/revise.go`

After successful update, build envelope from `GetNodeWithEdges` + run
`SuggestConnections` / contradiction check (reuse remember helpers). Re-embed must
complete before suggestion ranking.

### `tools/definitions.go`

Update `revise` description with envelope fields + same-turn review imperative.

### `docs/memoryweb-skill.md`

Mirror description changes per standing rule.

---

## Acceptance criteria

- Single revise returns connections + suggested_connections (+ possible_contradicts when triggered)
- Batch revise returns per-item envelopes
- Semantic-field revise (label/description/why_matters) updates embedding before suggestion ranking
- Tags-only / occurred_at-only / domain-only revise does not re-embed
- Tests in `tools/revise_test.go`; `go test ./...` green
- Skill + tool description reviewed in both directions

### Re-embed tests (TDD)

- `TestEmbedTextForNode_MatchesRememberConcatenation`
- `TestUpdateNode_LabelChangeTriggersReEmbed`
- `TestUpdateNode_TagsOnlyChangeDoesNotReEmbed`
- `TestRevise_SuggestedConnectionsUsePostUpdateEmbedding` (integration — revise label,
  then verify suggestions reflect new semantics not old vector)

---

## References

- Recordari STORY-250 acceptance criteria minus ownership/supersede ACs
- Deferred: `stories/occurred-at-embedding.md`
