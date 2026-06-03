# orient: stale_count in response root + audit(mode=stale) prompt in description

**Status:** PENDING

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

### 1. DB — `CountStaleTransients`

New method in `db/db.go`:

```go
func (s *Store) CountStaleTransients(domain string) (int, error)
```

Returns the count of live transient nodes (decision_type = 'transient') that are older
than 7 days. This is the same set surfaced by `audit(mode=stale)`. If `domain` is empty,
counts across all domains. Uses `archived_at IS NULL` guard.

SQL sketch:
```sql
SELECT COUNT(*) FROM nodes
WHERE archived_at IS NULL
  AND decision_type = 'transient'
  AND created_at < datetime('now', '-7 days')
  [AND domain = ? if domain != ""]
```

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

Populate it by calling `h.store.CountStaleTransients(domain)`. If the call errors, log
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
- `stale_count` is 0 when no transient nodes are older than 7 days.
- `stale_count` is > 0 when at least one transient node is older than 7 days.
- orient description contains the phrase `stale_count > 0` and references `audit(mode=stale)`.
- No existing orient tests broken.
- TDD sequence: failing tests first, confirm red, implement, green.

---

## Test sketch

```go
// tools/tools_test.go
func TestOrient_StaleCountZeroWhenNoStaleTransients(t *testing.T) { ... }
func TestOrient_StaleCountNonZeroWhenStaleTransientsExist(t *testing.T) { ... }
func TestOrient_DescriptionContainsStaleCountAdvisory(t *testing.T) { ... }
```

Stale transients can be created by calling `addNode` then directly updating
`created_at` via the raw Store (use `newEnvWithPath` helper).

---

## Related

- `orient-workspace-stats.md` (precedent for adding health fields to orient root)
- `significance-memory-id-mode.md` (neighbourhoodIDs pattern)
