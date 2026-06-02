# orient: sentence-boundary truncation + truncated flag

**Shared-surface node:** `orient-sentence-boundary-truncat-5bd5717b`
**Depends on:** orient-lean-format.md (COMPLETE — v1.23.0)

---

## Why

The current 150-char hard truncation cuts mid-sentence. A fragment that ends mid-thought reads
as a complete thought — agents infer and complete the reasoning, introducing hallucination risk
in overview responses. This is worse than no content at all.

Two additive fixes:

1. **Sentence-boundary truncation** — cut at the last complete sentence within the 150-char
   budget. If no sentence boundary exists, fall back to the hard cut. A complete sentence is
   a closed unit; a hard-cut fragment is explicit about incompleteness via `"..."`.

2. **`truncated` flag** — each node entry gains `truncated: true` when why_matters was
   shortened. Agents check the flag; they do not infer truncation from `"..."` or content length.

Priority by section: `declared_spine` highest (causal decisions — mid-sentence truncation here
is the highest hallucination risk), `significant` medium, `recent` lowest.

---

## Behaviour

### Sentence-boundary logic

Character budget: unchanged at 150.

Within the budget, find the last occurrence of `.`, `!`, or `?` followed by whitespace or
end-of-string. Cut after the punctuation mark (include it, strip trailing whitespace).
No `"..."` suffix on a sentence-boundary cut — it is a complete sentence.

If no sentence boundary exists within the budget: hard-cut at 150 chars and append `"..."`.

### `truncated` field

Added to `leanEntry` as `bool` with `json:"truncated,omitempty"`.

| Condition | `truncated` in JSON |
|-----------|---------------------|
| why_matters fits within 150 chars unchanged | omitted (false) |
| cut at a sentence boundary | `true` |
| hard-cut at 150 chars + `"..."` | `true` |

### No change to character budget or other fields

The 150-char limit, the omission of `description`, and the `omitempty` on `why_matters` are
all unchanged from the lean format contract.

---

## Implementation

### `truncateWhy` signature change

Current:
```go
func truncateWhy(s string) string
```

New — returns text and a truncation flag:
```go
func truncateWhy(s string) (string, bool)
```

Implementation:
```go
func truncateWhy(s string) (string, bool) {
    const limit = 150
    if len(s) <= limit {
        return s, false
    }
    sub := s[:limit]
    lastBoundary := -1
    for i, ch := range sub {
        if ch == '.' || ch == '!' || ch == '?' {
            next := i + 1
            if next >= len(sub) || sub[next] == ' ' || sub[next] == '\n' || sub[next] == '\t' {
                lastBoundary = next
            }
        }
    }
    if lastBoundary > 0 {
        return strings.TrimRight(s[:lastBoundary], " \t\n"), true
    }
    return sub + "...", true
}
```

### `leanEntry` struct change

Add `Truncated` field:
```go
type leanEntry struct {
    ID         string `json:"id"`
    Label      string `json:"label"`
    WhyMatters string `json:"why_matters,omitempty"`
    Truncated  bool   `json:"truncated,omitempty"`
    OccurredAt *string `json:"occurred_at,omitempty"`
}
```

### `toLean` function change

```go
toLean := func(n db.Node) leanEntry {
    why, truncated := truncateWhy(n.WhyMatters)
    e := leanEntry{
        ID:         n.ID,
        Label:      n.Label,
        WhyMatters: why,
        Truncated:  truncated,
    }
    if n.OccurredAt != nil {
        s := n.OccurredAt.Format("2006-01-02")
        e.OccurredAt = &s
    }
    return e
}
```

Note: `strings` import is already present.

---

## Tests

In `tools/tools_test.go`:

- `TestOrient_LeanFormat_SentenceBoundary` — file a node with why_matters containing a
  sentence ending well within 150 chars followed by more text; assert orient returns
  why_matters ending at the sentence boundary (no `"..."`), and `truncated: true`.
- `TestOrient_LeanFormat_HardCutFallback` — file a node with why_matters of 200 all-`x`
  chars (no sentence boundary); assert orient returns why_matters ending with `"..."`,
  length ≤ 153, and `truncated: true`.
- `TestOrient_LeanFormat_TruncatedFalseWhenFits` — file a node with short why_matters;
  assert `truncated` is absent from the JSON entry.

In `tools/tools_test.go` (update existing):

- `TestOrient_LeanFormat_WhyMattersTruncated` — update to also assert `truncated: true`
  in the entry.

---

## Files

- `tools/tools.go` — `truncateWhy()`, `leanEntry` struct, `toLean` closure in `summariseDomain()`
- `tools/tools_test.go` — three new tests, one updated

## References

- shared-surface node: `orient-sentence-boundary-truncat-5bd5717b`
- orient-lean-format.md (predecessor, COMPLETE)
