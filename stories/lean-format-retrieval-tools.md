# Extend orient's lean-format payload to search, recent, significance, history

**Status:** OPEN

**Shared-surface story node:** `story-extend-orient-s-lean-forma-30f10ae9`
**Caused by:** `token-reduction-gap-search-recen-6a034752` (memoryweb-meta finding)
**Precedent:** `v1-23-0-orient-lean-format-secti-53449ff5` (orient lean format, v1.23.0/v1.24.0)

---

## Why

orient was redesigned in two steps — the three-section response (v1.18.1), then lean
fields (v1.23.0/v1.24.0): every entry returns only `id`, `label`, and `why_matters`
truncated at 150 chars on a sentence boundary, with a `truncated` flag. `description`
is omitted entirely. This cut orient's per-node cost from ~200–500 tokens down to
~20–30 tokens, with `recall(id)` available whenever full content is actually needed.

That redesign was never extended past orient. Reading `db/db.go` directly confirms
`SearchNodes` (`searchNodesLike` / `searchNodesSemantic`), `RecentChanges` /
`RecentChangesScoped`, `Timeline` / `GetHistoryForMemoryID`, and `GetSignificance`
all still return full `Node` structs — full `description`, `why_matters`, `tags` —
for every result. A live `search` call against memoryweb-meta confirmed this in
practice: full multi-hundred-character descriptions on every match, plus a full
`edges` array (with narrative text) on top of the node list.

At default limits (10), a single `search`, `recent`, or `significance` call can cost
2000–4000+ tokens that lean formatting would cut to a few hundred. These four tools
are called far more often per session than orient, so this is the largest remaining
token-reduction lever on the tool surface — and it's reuse of an already-proven
pattern, not new design work.

This is cross-cutting: both memoryweb and Recordari implement these four tools
independently and must carry the same lean-entry contract. Filed in
memoryweb-shared-surface per the shared-surface filing rule
(`file-cross-cutting-stories-in-th-c1e7e900`).

---

## Scope

Apply the lean-entry pattern — `id`, `label`, `why_matters` (truncated, with
`truncated` flag), `description` and `tags` omitted — to list-shaped results in:

- **search** — `SearchNodes` (`searchNodesLike` / `searchNodesSemantic` — the
  default fuzzy/semantic path). `SearchNodesExact` (`exact: true`) is explicitly
  **out of scope** — keeps returning full content; see resolved question below.
  Note: `semantic_distance` stays on lean entries (it's a score, not content).
  The `edges` array returned alongside nodes should also drop to lean form (from/to
  IDs, relationship type) — `narrative` text omitted unless the agent recalls the node.
- **recent** — `RecentChanges`, `RecentChangesScoped`.
- **significance** — `GetSignificance`: declared, structural, uncurated, and
  potentially_stale sections all currently return full nodes.
- **history** — `Timeline`, `GetHistoryForMemoryID`.

Each affected tool description should gain the same truncation-disclosure sentence
orient already carries (see `orient-tool-description-contract-7698cf5b`):

> "Returns lean node data only — id, label, and a short excerpt. If you need full
> node content, call recall(id)."

`recall(id)` remains the single place full content is returned — no change needed there.

---

## Resolved design questions

- Should `occurred_at` stay on lean entries for `history`/`significance` (declared
  section), since chronological ordering is the point of those sections? Resolved:
  yes — keep `occurred_at`, drop everything else. orient already keeps it for
  `declared_spine` for the same reason.
- Does `search`'s `exact: true` mode need lean format too, or is exact-match lookup
  rare enough (single identifier lookup) that full content is fine? Resolved: keep
  `exact: true` returning full content. Result sets are typically 1–3 nodes (known
  identifier lookup), so the token-savings case is weak, while an agent calling
  `exact: true` already knows what it wants and is usually about to act on it —
  forcing a `recall(id)` round-trip there costs more than it saves. Lean format
  applies to the default (fuzzy/semantic) path only, where result volume (up to
  `limit`) is the actual cost driver.
- Tag filtering on `recent`/`significance`/`history` currently echoes back full tag
  strings on the node. Resolved: drop `tags` consistently with `description` — lean
  entries carry only `id`, `label`, `why_matters` (and `occurred_at` where
  applicable), even when the request itself filtered by tag.

---

## Tests (memoryweb)

Mirror the orient lean-format test pattern in `tools/tools_test.go`:

- `TestSearch_LeanFormat_NoDescription`
- `TestSearch_LeanFormat_WhyMattersTruncated`
- `TestSearch_LeanFormat_EdgesOmitNarrative`
- `TestRecent_LeanFormat_NoDescription`
- `TestSignificance_LeanFormat_NoDescription` (all four sections)
- `TestHistory_LeanFormat_NoDescription`
- `TestListTools_SearchDescriptionTruncationDisclosure` (and equivalents for recent,
  significance, history)

TDD applies per the standing rule — write each failing test before the
production change that makes it pass.

---

## Files (memoryweb)

- `db/db.go` — `SearchNodes`, `SearchNodesExact`, `searchNodesLike`,
  `searchNodesSemantic`, `RecentChanges`, `RecentChangesScoped`, `Timeline`,
  `GetHistoryForMemoryID`, `GetSignificance` — or a lean-entry mapping layer in
  `tools/tools.go` if leaving the DB layer returning full `Node` and truncating at
  the handler boundary is preferred (matches how orient does it — truncation lives
  in `tools/tools.go`, not `db/db.go`).
- `tools/tools.go` — handler-side lean mapping for `search`, `recent`,
  `significance`, `history`; tool description updates for all four.
- `tools/tools_test.go` — new tests above.

## Files (Recordari)

- `internal/adapters/repository/supabase/search.go`, `timeline.go`,
  `significance.go`, and whatever backs `recent` — equivalent lean-entry mapping.
- Equivalent tool description updates.

---

## References

- `stories/orient-lean-format.md` (predecessor — the pattern being ported)
- shared-surface node: `story-extend-orient-s-lean-forma-30f10ae9`
- shared-surface node: `orient-section-caps-5-max-for-si-fba2c34b`
- memoryweb-meta node: `token-reduction-gap-search-recen-6a034752`
