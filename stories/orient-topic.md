# orient: query-weighted mode via optional topic parameter

**Status:** COMPLETE — v1.26.0 (commit 8031e6d)

**Shared-surface node:** `orient-query-weighted-mode-via-o-a01c1ece`

---

## Why

Generic orient returns the `significant` section: structurally load-bearing nodes by
recency-weighted inbound degree. This answers "what is load-bearing in this domain right now?"
— useful for cold starts, poor for directed work.

When a session has a known task, the useful question is "what is relevant to what I am doing
today?" That requires the topic, not structural importance. The agent currently has to orient
(general), then search (topic-specific) — two round trips.

An optional `topic` parameter collapses this into one call: when supplied, the server runs a
vector similarity search over the domain and returns the top results as a `relevant` section.
Same token budget as a standard orient — `relevant` replaces `significant`, not adds to it.

---

## Behaviour

### New parameter

`topic` (string, optional) — the user's current question or task description. The agent
should pass this when the session has a known purpose.

### With topic supplied

1. Embed the topic text using the same Ollama embedding path as `search`.
2. Run a vector similarity search scoped to the domain (same `searchNodesSemantic` path).
3. Return the top 5 results as a `relevant` section instead of `significant`.
4. `declared_spine` and `recent` are returned unchanged.
5. Lean format applies to `relevant` identically: id + label + truncated why_matters, max 5 nodes.
6. If the embedding service is unavailable (Ollama not running), fall back to a text `LIKE`
   search on the topic string over label, description, why_matters, tags.

### Without topic (existing behaviour)

`significant` section returned as normal. No change to current behaviour.

### Response shape

```json
// with topic:
{
  "declared_spine": [ ... ],
  "relevant": [ { "id": "...", "label": "...", "why_matters": "..." }, ... ],
  "recent": [ ... ]
}

// without topic (unchanged):
{
  "declared_spine": [ ... ],
  "significant": [ { "id": "...", "label": "...", "why_matters": "...", "importance_score": 0.8 }, ... ],
  "recent": [ ... ]
}
```

Note: `relevant` has no `importance_score` — results are ranked by similarity, not inbound degree.

### Tool description addition

Add after the section semantics block in the orient description:

> "When the session has a known purpose, pass topic — the server returns a relevant section of
> the 5 most similar memories instead of significant. declared_spine and recent are always returned."

---

## Handler changes

In `tools/tools.go`:

1. Add `topic` property to orient `InputSchema`:
   ```go
   "topic": {Type: "string", Description: "Optional — the user's current question or task. When supplied, returns a relevant section of semantically similar memories instead of significant."},
   ```

2. In `summariseDomain()`, check for topic after the domain empty-check:
   ```go
   if a.Topic != "" {
       return h.orientWithTopic(a.Domain, a.Topic)
   }
   // existing significant section logic
   ```

3. Add `orientWithTopic(domain, topic string) (*ToolResult, error)` helper:
   - Call `h.store.SearchNodes(topic, domain, 5)` (reuse existing search path — it already
     falls back from semantic to LIKE when Ollama is unavailable).
   - Build the lean `relevant` section from results.
   - Fetch `declared_spine` and `recent` as normal.
   - Return the combined response with `relevant` instead of `significant`.

No new DB method needed — `SearchNodes` already handles semantic + LIKE fallback.

---

## Tests

In `tools/tools_test.go`:

- `TestOrient_Topic_ReturnsRelevantSection` — call orient with a topic that matches a filed
  node's label; assert response contains `relevant` key and does not contain `significant`.
- `TestOrient_Topic_RelevantIsLean` — assert `relevant` entries have no `description` field
  (lean format applies).
- `TestOrient_Topic_SpineAndRecentUnchanged` — with topic supplied, assert `declared_spine`
  and `recent` are still present.
- `TestOrient_NoTopic_SignificantPresent` — call orient without topic; assert `significant`
  is present and `relevant` is absent (no regression).
- `TestListTools_OrientDescriptionMentionsTopic` — assert orient description contains `"topic"`.

---

## Files

- `tools/tools.go` — orient `InputSchema`, `summariseDomain()`, new `orientWithTopic()` helper, orient description
- `tools/tools_test.go` — five new tests above

## References

- orient-lean-format.md (lean format applies to `relevant` section too)
- shared-surface node: `orient-query-weighted-mode-via-o-a01c1ece`
- existing: `SearchNodes` in `db/db.go` (semantic + LIKE fallback already implemented)
