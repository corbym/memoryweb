# Story: Fix occurred_at — correction path, importance signal, and robust history

**Discovered:** 2026-05-12  
**Domain:** memoryweb-meta  
**Related nodes:** `revise-tool-missing-occurred-at--c4a580c8`, `occurred-at-should-default-to-db-8e4dd320`, `occurred-at-as-importance-signal-cf7ac433`, `history-tool-currently-weak-agre-9dfd4702`, `history-tool-to-support-tag-base-00ad271e`

---

## Background

On 2026-05-12 a code agent filed a batch of nodes and silently guessed the date
for `occurred_at`, writing `2026-05-11` instead of `2026-05-12`. A direct SQL fix
would be required to correct it — there is no MCP path.

This exposed two gaps, and led to a broader redesign of how `occurred_at` and
`history` work together:

1. **No correction path** — `revise` and `revise_all` do not accept `occurred_at`.
2. **history is a weak tool** — it filters `WHERE occurred_at IS NOT NULL`, so most
   nodes never appear. It has no filtering capability. Both the human and the code
   agent independently identified this as a problem.

Note: `history` is the MCP tool name. `timeline` is the internal db method name
for the same operation. They are the same thing. This story uses `history` for
the tool-facing surface and `timeline` for the internal method.

---

## Design decisions

### occurred_at is an importance signal, not just a timestamp

`occurred_at` on a node means two things simultaneously:

1. This event happened at a specific time.
2. This event is important enough to appear as a significant event on the timeline.

Agents should only set `occurred_at` when filing a significant decision or event.
Not for routine technical findings, build notes, or debug observations. The
timestamp and the importance signal are inseparable.

**Agents must never guess or infer `occurred_at` from context.** It should only
be set when the user has explicitly indicated when an event occurred, or that it
is significant enough to place on the timeline. Future dates are valid — for
reminders or planned events.

When not supplied, `occurred_at` remains NULL. NULL means "not a significant
timeline event." It is the correct and honest value for most nodes.

### history has two modes

`history` serves two distinct use cases that must both be supported:

**Mode 1: Complete chronological view (default)**
Every node in a domain, ordered by `COALESCE(occurred_at, created_at) ASC`. No
filter on `occurred_at`. This gives a full picture of how a domain evolved over
time — what was filed, in what order. Complementary to `recent` (which is
`updated_at DESC`) and `orient` (which is structural, not temporal). An agent
picking up a long-running project uses this to understand the arc of the work.

**Mode 2: Important events only (`important_only=true`)**
Only nodes where `occurred_at IS NOT NULL`, ordered by `COALESCE(occurred_at, created_at) ASC`.
Since all rows in this mode have `occurred_at` set, COALESCE degrades to `occurred_at` —
the two are equivalent. There is no separate ORDER BY branch for Mode 2; the same
COALESCE expression is used throughout.

These are the significant decisions and events explicitly curated by the agent.
An agent debugging a decision trail, or reviewing key milestones, uses this mode.

The two modes are genuinely complementary:
- Mode 1 answers: "what happened in this domain, and in what order?"
- Mode 2 answers: "what were the important decisions and events?"

### from/to date range filtering

`from` and `to` are optional parameters that scope results by effective date:
`COALESCE(occurred_at, created_at)`. They apply in both modes.

- Mode 1 + from/to: everything filed in a date range, chronologically.
- Mode 2 + from/to: important events in a date range ("what did we decide last week?").
- Mode 2 + tags + from/to: important events of a specific type in a date range.

`from` and `to` are retained from the current implementation but their filter
condition must be updated to use `COALESCE(occurred_at, created_at)` rather than
`occurred_at` alone.

### Tag filtering applies to both modes

An optional `tags` parameter further scopes results in either mode. The input is
comma-separated (e.g. `"architecture,release"`). Tags are stored in the DB as a
space-separated string (e.g. `"decision architecture release"`).

Per tag, use the following four-part LIKE pattern to match whole words only and
avoid partial-word false positives:

```sql
(
  tags = ?                          -- exact single tag match
  OR tags LIKE ? || ' %'           -- tag at start
  OR tags LIKE '% ' || ?           -- tag at end
  OR tags LIKE '% ' || ? || ' %'  -- tag in middle
)
```

Bind the tag value to all four `?` placeholders. Repeat the expression per tag,
joined with OR. Known limitation: avoid single-character tag names, which may
produce false positives against the space-padded patterns.

### Ordering — single expression throughout

All queries use `ORDER BY COALESCE(occurred_at, created_at) ASC`. There is no
separate ORDER BY branch for any mode or filter combination. In Mode 2, COALESCE
degrades to `occurred_at` since all returned rows have it set — this is correct
and requires no special handling.

---

## How an agent should use history

| Scenario | Mode | Tags | from/to |
|----------|------|------|---------|
| Picking up a project cold, want full chronological picture | Mode 1 (default) | none | none |
| Understanding how key decisions evolved | Mode 2 (`important_only=true`) | none | none |
| What did we decide last week? | Mode 2 | none | last 7 days |
| Debugging a specific decision trail | Mode 2 | e.g. `architecture` | none |
| Checking what's coming up (future-dated reminders) | Mode 2 | none | from=today |
| Everything filed in a domain this week | Mode 1 | none | this week |
| Significant engineering decisions this sprint | Mode 2 | e.g. `decision` | sprint range |

---

## Requirements

### 1. Add `occurred_at` to `revise`

**Tool schema** (`tools/tools.go`, `ListTools`)

Add `occurred_at` as an optional string property to the `revise` tool's
`InputSchema.Properties`:

```json
"occurred_at": {
  "type": "string",
  "description": "ISO8601 date or datetime to correct when this event occurred (e.g. '2026-04-01' or '2026-04-01T14:30:00Z'). Only supply when the user has explicitly told you when this event occurred. Do not guess. Do not infer from context."
}
```

`id` remains the only required field.

**Handler** (`tools/tools.go`, `updateNode`)

The `updateNode` handler struct must gain an `OccurredAt *string` field.
If non-nil, parse it using the same two-format fallback used in `addNode`
(RFC3339 first, then `2006-01-02`). Pass the result to `UpdateNode` as a new
`*time.Time` parameter.

Return a validation error if the string is non-empty but unparseable.

---

### 2. Add `occurred_at` to `revise_all`

**Tool schema** (`tools/tools.go`, `ListTools`)

Update the `revise_all` items schema to include `occurred_at` as an optional
string property alongside `label`, `description`, `why_matters`, and `tags`.
Use the same description wording as above.

**Handler** (`tools/tools.go`, `updateNodes`)

The anonymous update struct inside `updateNodes` must gain `OccurredAt *string`.
Parse with the same two-format fallback. Pass to `UpdateNodesBatch` via an
updated `NodeUpdateInput`.

---

### 3. Update `occurred_at` tool description on `remember` and `remember_all`

In `ListTools`, update the `occurred_at` property description for both `remember`
and `remember_all`:

```json
"occurred_at": {
  "type": "string",
  "description": "ISO8601 date or datetime for when this event occurred (e.g. '2026-04-01' or '2026-04-01T14:30:00Z'). Only supply when the user has explicitly told you when this event occurred, or when this is a significant decision or event that belongs on the timeline. Do not guess. Do not infer from context. Future dates are valid for planned events or reminders."
}
```

---

### 4. Update `UpdateNode` in db/db.go

Add an `occurredAt *time.Time` parameter to the `UpdateNode` signature:

```go
func (s *Store) UpdateNode(
    id string,
    label, description, whyMatters, tags *string,
    occurredAt *time.Time,
) (*Node, error)
```

When `occurredAt` is non-nil:

- Append `occurred_at = ?` to the SET clause.
- Append `"occurred_at (was <old>)"` to the `changes` slice for the audit log.
  Format the old value as `2006-01-02T15:04:05Z`; if the old value is NULL,
  use the string `"(none)"`.

---

### 5. Update `NodeUpdateInput` and `UpdateNodesBatch` in db/db.go

Add `OccurredAt *time.Time` to `NodeUpdateInput`:

```go
type NodeUpdateInput struct {
    ID          string
    Label       *string
    Description *string
    WhyMatters  *string
    Tags        *string
    OccurredAt  *time.Time
}
```

In `UpdateNodesBatch`, handle `OccurredAt` the same way as the single-node path.

---

### 6. Overhaul the `history` tool and `timeline` db method

**Tool schema** (`tools/tools.go`, `ListTools`)

Replace the current `history` tool description and parameters with the following.
Keep `domain` and `limit` as they are. Replace `from`/`to` semantics (see below).
Add `important_only` and `tags`.

Tool description:

```
Returns nodes in a domain in chronological order by effective date
(COALESCE(occurred_at, created_at)).

By default returns ALL nodes — the complete chronological view of everything
filed in the domain. Use this to understand how a domain evolved over time.

Set important_only=true to return only nodes where occurred_at is explicitly set.
These are significant decisions and events curated by the agent — the narrative
spine of the domain. Use this to review key milestones or debug a decision trail.

Use from/to to scope by effective date. Examples:
  - important_only=true, from=last Monday: "what did we decide last week?"
  - important_only=true, from=today: upcoming reminders and planned events.
  - from/to with no important_only: everything filed in a date window.

Use tags to further filter results in either mode (comma-separated).

The two modes are complementary:
  - Default: "what happened in this domain, and in what order?"
  - important_only=true: "what were the important decisions and events?"
```

Parameters:

```json
"important_only": {
  "type": "boolean",
  "description": "When true, return only nodes with occurred_at explicitly set (significant decisions and events). When false or absent, return all nodes ordered by effective date."
},
"tags": {
  "type": "string",
  "description": "Optional comma-separated list of tags to filter by. Only nodes matching at least one tag are returned. Applies in both modes."
},
"from": {
  "type": "string",
  "description": "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or after this value."
},
"to": {
  "type": "string",
  "description": "ISO8601 date or datetime. Filter to nodes whose effective date (COALESCE(occurred_at, created_at)) is on or before this value."
}
```

**Handler** (`tools/tools.go`, `timeline` handler)

Extract all four parameters and pass to `Timeline`:
- `important_only` bool, default false
- `tags` string, split on comma, trim whitespace, nil if absent
- `from` *time.Time, parsed with two-format fallback, nil if absent
- `to` *time.Time, parsed with two-format fallback, nil if absent

**`Timeline` db method** (`db/db.go`)

Update signature:

```go
func (s *Store) Timeline(
    domain string,
    importantOnly bool,
    tags []string,
    from, to *time.Time,
    limit int,
) ([]Node, error)
```

Query logic:

```
SELECT ... FROM nodes
WHERE domain = ?
  [AND occurred_at IS NOT NULL]                          -- if importantOnly=true
  [AND COALESCE(occurred_at, created_at) >= ?]           -- if from non-nil
  [AND COALESCE(occurred_at, created_at) <= ?]           -- if to non-nil
  [AND (tag LIKE patterns...)]                           -- if tags non-empty, per tag:
    (tags = ? OR tags LIKE ? || ' %' OR tags LIKE '% ' || ? OR tags LIKE '% ' || ? || ' %')
ORDER BY COALESCE(occurred_at, created_at) ASC
LIMIT ?
```

Single ORDER BY expression throughout. No separate branch for `importantOnly`.

---

## Acceptance criteria

| # | Criterion |
|---|-----------|
| AC-1 | `revise` with a valid `occurred_at` string updates the node's timestamp and returns the updated node with the new value. |
| AC-2 | `revise` with an invalid `occurred_at` string returns `IsError: true` and does not modify the node. |
| AC-3 | `revise` without `occurred_at` behaves identically to today — no change to `occurred_at`. |
| AC-4 | `revise_all` applies the same behaviour across multiple nodes in a single transaction (all succeed or all roll back). |
| AC-5 | When `occurred_at` changes via `revise` or `revise_all`, the audit log records the old value in the reason field. |
| AC-6 | `remember` and `remember_all` tool descriptions include the importance signal wording and "do not guess" instruction for `occurred_at`. |
| AC-7 | `history` with no parameters returns all nodes in the domain ordered by `COALESCE(occurred_at, created_at) ASC`, including nodes with NULL `occurred_at`. |
| AC-8 | `history` with `important_only=true` returns only nodes where `occurred_at IS NOT NULL`, ordered by `COALESCE(occurred_at, created_at) ASC`. |
| AC-9 | `history` with `from` and/or `to` filters correctly by effective date in both modes. |
| AC-10 | `history` with `tags` returns only nodes matching at least one supplied tag, using whole-word matching. |
| AC-11 | `history` with all four optional parameters (`important_only`, `tags`, `from`, `to`) applies all filters correctly and returns the correct subset. |
| AC-12 | All existing tests pass (`go test ./...`). |
| AC-13 | New tests cover: revise sets occurred_at, revise rejects invalid occurred_at, revise omitting occurred_at leaves it unchanged, history default mode returns all nodes, history important_only returns only explicitly dated nodes, history from/to filters by effective date, history tag filter uses whole-word matching, history with all filters combined. |

---

## Out of scope

- Defaulting `occurred_at` to `created_at` in `AddNode` or `AddNodesBatch`.
- Exposing `occurred_at` on `connect`, `forget`, or any edge tool.
- Backfilling existing nodes that have NULL `occurred_at`.
- AGENTS.md guidance on date guessing — tracked as a separate node.