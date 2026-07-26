# occurred_at in embedding text — DEFERRED

**Status:** DEFERRED (2026-07-26)

**Shared-surface nodes:**
- `verify-does-memoryweb-single-tenant-also-treat-occurred-at-as-inert-in-search-not-embedded-not-surfaced-recordari-confirmed-it-does-story-272-58f3b222`
- Recordari STORY-272 (`filed-story-272-epic-001-occurred-at-becomes-a-real-retrieval-signal-embedded-surfaced-in-search-skill-layer-1-prose-rule-held-in-reserve-de2a422b`)

**Supersedes:** re-embed-on-revise folded into `stories/revise-response-envelope-re-embed.md`

---

## Deferral decision (2026-07-26)

Not worth implementing for date retrieval. Recordari STORY-272 post-backfill probe on
Bedrock titan-embed (`finding-bedrock-titan-embed-does-not-associate-occurred-d-month-yyyy-with-date-phrased-queries-date-query-recall-0-41-post-backfill-7fe4c9c7`):

- Date **visibility** in lean/digest lines: ~97% (PASS)
- Date-**query** semantic recall: **0/41** (unchanged after embedding change)

memoryweb already has the half that worked:

| Half | memoryweb state |
|------|-----------------|
| **Surfacing** | ✅ `tools/lean.go` — `occurred_at` in lean entries; `(YYYY-MM-DD)` in digest lines |
| **Embedding date in vector text** | ❌ not implemented — **deferred, not scheduled** |

Date-shaped questions should use **`history(from=…, to=…)`** (explicit `occurred_at`
filter) or **`history(important_only=true)`** (spine) — not semantic `search`.

Re-embed after semantic-field `revise` is a separate concern (stale vectors break
`suggested_connections` ranking) — tracked in `revise-response-envelope-re-embed.md`,
not here.

---

## Reopen only if

1. An arctic-embed probe (10–20 date-phrased queries vs nodes with `occurred_at` set)
   shows **non-zero** date-query recall when date is appended to embed text — titan-embed
   failure does not automatically predict arctic-embed behaviour, but is strong prior
   against investing without evidence.
2. A new retrieval mechanism makes embedding dates load-bearing again.

---

## Original scope (preserved for reopen)

If reopened: centralise `embedTextForNode(label, description, whyMatters, occurredAt)`
in `db/embeddings.go`, append `"Occurred D Month YYYY"` when set, update
`AddNode`/`AddNodesBatch`/`BackfillEmbeddings`, backfill live nodes with
`occurred_at IS NOT NULL`. Do **not** re-implement surfacing.

---

## References

- Recordari STORY-272 done node: `story-272-done-occurred-at-embedded-occurred-d-month-yyyy-surfaced-in-nodesordigest-result-lines-54817be7`
- Revise re-embed (active): `stories/revise-response-envelope-re-embed.md`
