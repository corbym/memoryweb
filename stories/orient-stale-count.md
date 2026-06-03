# orient: stale_count in response root + audit(mode=stale) prompt in description

**Status:** COMPLETE — v1.29.0 (commit ae8f81d)

**Shared-surface node:** `orient-stale-count-in-response-r-9971193d`

**Memoryweb-meta node:** `story-rules-section-hard-cap-sec-5864279d`

---

## Why

Agents are currently expected to remember to call `audit(mode=stale)` themselves.
This is unreliable: it requires the agent to exercise independent judgement about when
to curate, and there is no in-band signal from the server prompting the check.

`stale_count` in the orient response root fixes this: the server computes the count on
every session start and the agent reacts to a concrete number rather than relying on
recall. Placement alongside `live_nodes` and `archived_nodes` makes it a first-class
health metric, not an afterthought.

The orient description advisory completes the loop: if `stale_count > 0`, the agent has
a concrete, early instruction to call `audit(mode=stale)` before filing new memories.

### What's already done

`GetStandingNodes` already has `LIMIT 20` + `ORDER BY inbound_count DESC` (capped at 20,
most-connected rules first). The section-level `truncated` flag for rules is out of scope
for this story. The only remaining gaps are `stale_count` and the description advisory.

---

## Changes

### 0. DB — `FindDrift` rule 6 (prerequisite)

Add a 6th rule to `FindDrift` in `db/db.go`. Update the leading comment:

```
// 6. Standing node with low inbound edge count (< 2) older than 30 days.
```

SQL sketch for rule 6:

```sql
SELECT n.id, n.label, ..., COUNT(e.id) AS inbound_count
FROM nodes n
LEFT JOIN edges e ON e.to_node = n.id AND e.archived_at IS NULL
WHERE n.archived_at IS NULL
  AND n.decision_type = 'standing'
  AND n.created_at < datetime('now', '-30 days')
  [AND n.domain = ? if domain != ""]
GROUP BY n.id
HAVING inbound_count < 2
```

Threshold rationale:
- `inbound_count = 0` is orphan territory, already surfaced by `audit(mode=orphans)`. Rule 6
  overlaps intentionally — orphans mode is opt-in; stale mode is the proactive health check.
- `< 2` catches orphans AND nodes with only a single edge (e.g. their own creation link).
- 30-day age guard excludes freshly filed rules that haven't had time to attract connections.

The drift reason string: `"standing rule with low connection count — may not be in use"`.

### 1. DB — `CountStaleDrift`

New method in `db/db.go`:

```go
func (s *Store) CountStaleDrift(domain string) (int, error)
```

Returns the count of live nodes that would be surfaced by `audit(mode=stale)` — i.e. the
union of all 6 `FindDrift` rules. This includes **any** `decision_type`, not just
transients:

1. Connected by a `contradicts` edge  
2. Label contains superseded keywords (old, deprecated, replaced, legacy, previous)  
3. Open-question keywords + older than 30 days  
4. Duplicate label in same domain  
5. `decision_type = 'transient'` and older than 7 days  
6. `decision_type = 'standing'` with low inbound edge count and older than 30 days  

Standing rules CAN appear in rules 1–4 (e.g. a standing rule with a `contradicts` edge,
or a stale open-question standing node). Rule 6 is the specific standing-rules health
check: rules that were filed but nobody has connected `governed_by` or `is_example_of`
edges to them. They silently fall off the LIMIT 20 cap in `GetStandingNodes` and become
invisible in orient — rule 6 surfaces that failure of connection before it happens.

Implementation: the simplest correct approach is to call `FindDrift(domain, 1000, nil, "", 2)`
and return `len(result)`. This avoids duplicating the 6-rule SQL logic. The 1000 limit is
a practical cap; in pathological cases the count will be floored at 1000, which is
acceptable — if there are 1000+ stale nodes, `stale_count > 0` is all the agent needs.

Alternatively, extract a private `countDrift(domain string) int` to avoid the
allocation if performance matters. Start with the `FindDrift` delegation approach first.

### 2. Tools — orient response struct

Add `StaleCount int` to the anonymous orient response struct in `tools/tools.go`:

```go
resp := struct {
    SummaryHint   string      `json:"summary_hint"`
    ServerVersion string      `json:"server_version"`
    LiveNodes     int         `json:"live_nodes"`
    ArchivedNodes int         `json:"archived_nodes"`
    StaleCount    int         `json:"stale_count"`   // NEW
    Rules         interface{} `json:"rules,omitempty"`
    ...
}
```

Populate it by calling `h.store.CountStaleDrift(domain)`. If the call errors, log
and default to 0 (non-fatal).

Apply to **both** orient response structs: the domain-scoped path and the cross-domain
snapshot path (line ~1182 in tools.go).

### 3. Tools — orient description advisory

Add the following sentence early in the orient description (before the parameter docs,
after the section summary):

> "if stale_count > 0, call audit(mode=stale) before filing new memories."

Placement rule: must appear before any parameter documentation, consistent with
the instruction-position convention.

---

## Acceptance criteria

- `stale_count` appears in orient JSON output as an integer.
- `stale_count` is 0 when no nodes match any of the 6 FindDrift rules.
- `stale_count` is > 0 when at least one node matches any drift rule (transient >7d, contradicts edge, superseded label, stale open question, duplicate, or low-connection standing rule).
- A standing node with a `contradicts` edge contributes to `stale_count`.
- A standing node older than 30 days with fewer than 2 inbound edges contributes to `stale_count`.
- orient description contains the phrase `stale_count > 0` and references `audit(mode=stale)`.
- No existing orient tests broken.
- TDD sequence: failing tests first, confirm red, implement, green.

---

## Test sketch

```go
// db/db_test.go — rule 6
func TestFindDrift_LowConnectionStandingNode(t *testing.T) { ... }  // standing node, 0 edges, >30d
func TestFindDrift_StandingNodeNotFlaggedWhenYoung(t *testing.T) { ... }  // same but <30d
func TestFindDrift_StandingNodeNotFlaggedWhenWellConnected(t *testing.T) { ... }  // inbound >= 2

// tools/tools_test.go — orient stale_count
func TestOrient_StaleCountZeroWhenNoDrift(t *testing.T) { ... }
func TestOrient_StaleCountNonZeroWhenTransientIsStale(t *testing.T) { ... }
func TestOrient_StaleCountNonZeroWhenStandingNodeContradicts(t *testing.T) { ... }
func TestOrient_StaleCountNonZeroWhenLowConnectionStanding(t *testing.T) { ... }
func TestOrient_DescriptionContainsStaleCountAdvisory(t *testing.T) { ... }
```

Stale transients / old standing nodes: call `addNode` then update `created_at` to the
required age via raw Store (`newEnvWithPath` helper). Contradicts: add two nodes and
`connect` them with `relationship=contradicts`.

---

## Related

- `orient-workspace-stats.md` (precedent for adding health fields to orient root)
- `significance-memory-id-mode.md` (neighbourhoodIDs pattern)
