# significance: tags filter

**Memoryweb-meta node:** `significance-tags-filter-deferre-6838a8be`

**Prior art:** `history` tool tags filter — same WHERE clause pattern, same property
description shape. See `db.Store.Timeline` for the reference implementation.

---

## Why

Domain-mode significance returns all nodes in a domain. When a domain is large and
consistently tagged, callers may want to narrow the analysis to a named workstream or
category (e.g. only nodes tagged `architecture`, or only nodes tagged `release`).

This complements `memory_id` mode but answers a different question:
- `memory_id` mode: "what is load-bearing in the neighbourhood of this anchor?" — scoped
  by graph topology from a known node.
- tags filter: "what is load-bearing among nodes labelled as this workstream?" — scoped
  by metadata, not topology.

Tags filter is the right choice when:
- The workstream is consistently tagged and the agent knows the tag name.
- No single anchor node is known.
- The agent wants to check domain health for a cross-cutting concern (e.g. all nodes tagged
  `security`).

Tags filter is NOT a substitute for `memory_id` mode — when the agent knows a node ID,
tags are typically not representative of the full connected workstream.

---

## Changes

### 1. DB — `GetSignificance` signature update

Add `tags []string` parameter:

```go
func (s *Store) GetSignificance(domain string, limit int, recencyWindowDays int, tags []string) (SignificanceResult, error)
```

Apply a WHERE clause extension to both the declared and structural queries. Follow the
same whole-word match pattern as `Timeline`:

```go
// For each tag in tags, append:
"(tags = ? OR tags LIKE ? || ' %' OR tags LIKE '% ' || ? OR tags LIKE '% ' || ? || ' %')"
// args: tag, tag, tag, tag  (four bindings per tag)
```

The declared and structural queries must both be built dynamically when `len(tags) > 0`,
appending the tag conditions with AND.

Callers that pass `nil` or `[]string{}` get the existing behaviour unchanged.

Update all existing callsites of `GetSignificance` to pass `nil` or `[]string{}` as the
new last argument.

### 2. Handler — parse `tags` from args

Add `Tags` field to the significance args struct:

```go
var a struct {
    Domain        string `json:"domain"`
    MemoryID      string `json:"memory_id"`   // from memory_id mode story
    Depth         int    `json:"depth"`        // from memory_id mode story
    Limit         int    `json:"limit"`
    RecencyWindow int    `json:"recency_window"`
    Tags          string `json:"tags"`
}
```

Parse comma-separated tags in the handler:

```go
var tags []string
for _, t := range strings.Split(a.Tags, ",") {
    t = strings.TrimSpace(t)
    if t != "" {
        tags = append(tags, t)
    }
}
```

Pass `tags` to `GetSignificance`. Tags filter applies in domain mode only — in
`memory_id` mode the node set is already bounded by neighbourhood; tags can be applied
post-filter if needed but must not be the primary scoping mechanism. **For this story,
implement tags for domain mode only.** `memory_id` + tags interaction is out of scope.

### 3. Schema — add `tags` property to significance

```go
"tags": {Type: "string", Description: "Optional comma-separated list of tags to filter by. Only memories matching at least one tag are included in the analysis. Applies in domain mode. Examples: 'architecture,security' or 'release'."},
```

### 4. Tool description addition

Add after the existing sentence about `limit`:

> Use `tags` (comma-separated) to narrow the analysis to nodes matching at least one tag.
> Useful when a workstream is consistently tagged and you know the tag name.

---

## Tests

- `TestSignificance_TagsFilter_IncludesMatchingNodes`: add two nodes — one with matching
  tag, one without — connect linkers to both; call significance with that tag; verify only
  the matching node appears in structural.
- `TestSignificance_TagsFilter_MultiTag_OR`: add nodes with tags `"foo"` and `"bar"`;
  call significance with `tags="foo,bar"`; verify both appear.
- `TestSignificance_TagsFilter_NoMatch_EmptyStructural`: call significance with a tag that
  matches no live nodes; verify structural section is empty (not an error).
- `TestSignificance_TagsFilter_WholeWordMatch`: add a node with tags `"foobar"`; call with
  `tags="foo"`; verify it does NOT appear (partial-word match must not fire).
- `TestSignificance_TagsFilter_DeclaredRespected`: add a node with `occurred_at` and the
  filter tag; verify it appears in declared.

---

## Out of scope

- `memory_id` + tags interaction. If both are supplied, tags are silently ignored in
  `memory_id` mode for this story. A follow-up story can add tag post-filtering to
  neighbourhood results.
- Adding `tags` to `GetSignificanceByNodeIDs` (the neighbourhood helper from the
  `memory_id` mode story). That is a separate story if ever needed.
