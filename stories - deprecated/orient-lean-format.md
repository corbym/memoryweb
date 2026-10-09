# orient: lean field format + section cap update

**Status:** COMPLETE

**Shared-surface nodes:**
- `orient-lean-field-format-id-labe-d1cb5704` — lean field contract
- `orient-section-caps-5-max-for-si-fba2c34b` — updated section caps
- `orient-tool-description-contract-7698cf5b` — description extension (truncation disclosure)

---

## Why

The orient redesign (shipped v1.18.1) replaced `all_nodes` with three purposeful sections. But
each node still returns full content — id, label, description, why_matters, occurred_at. On a
mature domain the declared_spine alone has ~70 entries each with multi-paragraph descriptions.
The result is a response that overflows agent context limits for the same reason `all_nodes` did.

The fix is a **lean field format**: orient returns only id, label, and why_matters truncated at
150 characters. Description is stripped entirely. An agent needing full content calls `recall(id)`.

The section caps are also tightened now that lean format makes every node cheap: significant
and recent drop from 10–15 / 10 to **5 / 5**. The declared_spine stays at 20 — it is the curated
decision history and should be as complete as the cap allows.

---

## Behaviour

### Field changes in orient response (all sections)

Every node entry across `declared_spine`, `significant`, and `recent` returns exactly three fields:

| Field | Format |
|-------|--------|
| `id` | unchanged |
| `label` | unchanged |
| `why_matters` | truncated at 150 chars; `"..."` appended if longer; omitted if null/empty |

`description` is **omitted entirely** from orient responses. No placeholder. No empty string.

An agent that needs the full description for a node must call `recall(id)`.

### Section cap changes

| Section | Old cap | New cap |
|---------|---------|---------|
| `declared_spine` | 20 | 20 (unchanged) |
| `significant` | 15 | **10** |
| `recent` | 10 | **5** |

Rationale: `significant` serves continuity across sessions and compacts — it answers "what is
currently load-bearing?" rather than "what was I just touching?" A mid-flight project may have
8–10 genuinely critical nodes (blocking issue, open design question, current approach, key
dependency). Cutting to 5 risks silently dropping the 6th most important node before the agent
has enough context to know what to search for. At lean format, 10 nodes is ~300 tokens — no
budget argument for cutting further.

`recent` stays at 5: it only needs to anchor "where was work happening last?" A handful of
recent touchpoints is sufficient before the agent searches for specifics.

### Description extension

Append the following to the orient tool description (after the existing post-orient guidance):

> "orient returns lean node data only — id, label, and a short excerpt. If you need full
> node content, call recall(id). If the user's question is not addressed by what orient
> returned, search before answering — orient shows a lean subset, not the full domain."

---

## Handler changes

In `tools/tools.go`, `summariseDomain()`:

1. Update `toEntry` to truncate `why_matters` at 150 chars and strip `description`:

```go
func truncateWhy(s string) string {
    const limit = 150
    if len(s) <= limit {
        return s
    }
    return s[:limit] + "..."
}

type leanEntry struct {
    ID         string  `json:"id"`
    Label      string  `json:"label"`
    WhyMatters string  `json:"why_matters,omitempty"`
    OccurredAt *string `json:"occurred_at,omitempty"`
}
```

2. Change the `GetSignificance` call limit from `15` to `10`.
3. Change the `RecentChanges` call limit from `10` to `5`.
4. The `Timeline` call for `declared_spine` stays at `20`.
5. Update the `significant` scored entry struct to use `leanEntry` (drop `description`).
6. Append the truncation disclosure sentence to the orient tool description string.

---

## Tests

In `tools/tools_test.go`:

- `TestOrient_LeanFormat_NoDescription` — call orient on a domain with nodes that have
  description set; assert no `description` field appears in any section of the response.
- `TestOrient_LeanFormat_WhyMattersTruncated` — file a node with why_matters longer than
  150 chars; call orient; assert the returned why_matters ends with `"..."` and is ≤ 153 chars.
- `TestOrient_LeanFormat_WhyMattersNullOmitted` — file a node with no why_matters; call
  orient; assert the node entry does not include a `why_matters` key.
- `TestOrient_SignificantCappedAtTen` — populate a domain with 15+ interconnected nodes;
  call orient; assert `len(significant) <= 10`.
- `TestOrient_RecentCappedAtFive` — file 10+ nodes in a domain; call orient; assert
  `len(recent) <= 5`.
- `TestListTools_OrientDescriptionTruncationDisclosure` — assert orient description contains
  the string `"call recall(id)"`.

---

## Files

- `tools/tools.go` — `summariseDomain()`, `leanEntry` struct, `truncateWhy()` helper, orient description
- `tools/tools_test.go` — six new tests above

## References

- orient-redesign.md (predecessor story, marked COMPLETE)
- shared-surface node: `orient-lean-field-format-id-labe-d1cb5704`
- shared-surface node: `orient-section-caps-5-max-for-si-fba2c34b`
- shared-surface node: `orient-tool-description-contract-7698cf5b`
